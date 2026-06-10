package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"e2ee/keys-service/internal"
	pb "e2ee/pkg/pb/keyspb"
	"e2ee/pkg/tracing"

	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	_ = godotenv.Load(".env", "../.env")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tp, err := tracing.InitTracer("keys-service")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
	} else {
		defer func() { _ = tp.Shutdown(context.Background()) }()
	}

	port := os.Getenv("KEYS_SERVICE_PORT")
	if port == "" {
		port = "50051"
	}

	dsn := os.Getenv("KEYS_DB_DSN")
	if dsn == "" {
		slog.Error("KEYS_DB_DSN is required")
		os.Exit(1)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("JWT_SECRET is required")
		os.Exit(1)
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
	keysServer := internal.NewKeysServer(store, jwtSecret)
	pb.RegisterKeysServiceServer(grpcServer, keysServer)
	reflection.Register(grpcServer)

	go func() {
		slog.Info("keys-service listening", "port", port)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("failed to serve", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down keys-service")
	grpcServer.GracefulStop()
}
