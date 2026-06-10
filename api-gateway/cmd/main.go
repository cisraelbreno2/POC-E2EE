package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"e2ee/api-gateway/internal"
	"e2ee/pkg/tracing"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	_ = godotenv.Load(".env", "../.env")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tp, err := tracing.InitTracer("api-gateway")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
	} else {
		defer func() { _ = tp.Shutdown(context.Background()) }()
	}

	port := os.Getenv("API_GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}

	keysServiceAddr := os.Getenv("KEYS_SERVICE_ADDR")
	redisAddr := os.Getenv("REDIS_ADDR")
	jwtSecret := os.Getenv("JWT_SECRET")
	kafkaBrokers := os.Getenv("KAFKA_BROKERS")

	if keysServiceAddr == "" || redisAddr == "" || jwtSecret == "" {
		slog.Error("missing required environment variables")
		os.Exit(1)
	}
	if kafkaBrokers == "" {
		kafkaBrokers = "kafka:9092"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	keysConn, err := grpc.NewClient(
		keysServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		slog.Error("failed to connect to keys-service", "error", err)
		os.Exit(1)
	}
	defer keysConn.Close()

	kafkaWriter := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBrokers),
		Topic:                  "chat.messages",
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
	defer kafkaWriter.Close()

	gateway := internal.NewGateway(keysConn, rdb, kafkaWriter, jwtSecret)

	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: gateway.Handler(),
	}

	slog.Info("api-gateway listening", "port", port)
	if err := httpServer.ListenAndServe(); err != nil {
		slog.Error("server error", "error", err)
	}
}
