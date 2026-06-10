package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"e2ee/messages-service/internal"
	pb "e2ee/pkg/pb/messagespb"
	"e2ee/pkg/tracing"

	"github.com/joho/godotenv"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	_ = godotenv.Load(".env", "../.env")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tp, err := tracing.InitTracer("messages-service")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
	} else {
		defer func() { _ = tp.Shutdown(context.Background()) }()
	}

	port := os.Getenv("MESSAGES_SERVICE_PORT")
	if port == "" {
		port = "50052"
	}

	dsn := os.Getenv("MESSAGES_DB_DSN")
	if dsn == "" {
		slog.Error("MESSAGES_DB_DSN is required")
		os.Exit(1)
	}

	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	if kafkaBrokers == "" {
		kafkaBrokers = "localhost:9092"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := internal.NewStore(ctx, dsn)
	if err != nil {
		slog.Error("failed to connect to db", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	messagesServer := internal.NewMessagesServer(store)
	pb.RegisterMessagesServiceServer(grpcServer, messagesServer)
	reflection.Register(grpcServer)

	go func() {
		slog.Info("messages-service listening", "port", port)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("failed to serve", "error", err)
		}
	}()

	// Start Kafka Consumer
	go startKafkaConsumer(kafkaBrokers, store)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down messages-service")
	grpcServer.GracefulStop()
}

type KafkaMessage struct {
	From               string `json:"from"`
	To                 string `json:"to"`
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	EncryptedPayload   string `json:"encrypted_payload"`
	Signature          string `json:"signature"`
}

func startKafkaConsumer(brokers string, store *internal.Store) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{brokers},
		GroupID: "messages-worker",
		Topic:   "chat.messages",
	})
	defer reader.Close()

	slog.Info("Kafka consumer started", "topic", "chat.messages")

	for {
		m, err := reader.ReadMessage(context.Background())
		if err != nil {
			slog.Error("kafka read error", "error", err)
			continue
		}

		var payload KafkaMessage
		if err := json.Unmarshal(m.Value, &payload); err != nil {
			slog.Error("kafka unmarshal error", "error", err)
			continue
		}

		req := &pb.SendMessageRequest{
			From:               payload.From,
			To:                 payload.To,
			EphemeralPublicKey: payload.EphemeralPublicKey,
			EncryptedPayload:   payload.EncryptedPayload,
			Signature:          payload.Signature,
		}

		if err := store.SaveMessage(context.Background(), req); err != nil {
			slog.Error("failed to save message from kafka", "error", err)
		} else {
			slog.Info("message saved from kafka", "from", req.From, "to", req.To)
		}
	}
}
