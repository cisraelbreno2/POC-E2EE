package internal

import (
	"context"
	"log/slog"

	pb "e2ee/pkg/pb/messagespb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MessagesServer struct {
	pb.UnimplementedMessagesServiceServer
	store *Store
}

func NewMessagesServer(store *Store) *MessagesServer {
	return &MessagesServer{store: store}
}

func (s *MessagesServer) SendMessage(ctx context.Context, req *pb.SendMessageRequest) (*pb.SendMessageResponse, error) {
	if req.From == "" || req.To == "" || req.EphemeralPublicKey == "" || req.EncryptedPayload == "" || req.Signature == "" {
		return nil, status.Error(codes.InvalidArgument, "all fields are required")
	}

	err := s.store.SaveMessage(ctx, req)
	if err != nil {
		slog.Error("failed to save message", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.SendMessageResponse{Success: true}, nil
}

func (s *MessagesServer) ListMessages(ctx context.Context, req *pb.ListMessagesRequest) (*pb.ListMessagesResponse, error) {
	if req.To == "" {
		return nil, status.Error(codes.InvalidArgument, "to parameter is required")
	}

	messages, err := s.store.ListMessagesForUser(ctx, req.To)
	if err != nil {
		slog.Error("failed to list messages", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.ListMessagesResponse{Messages: messages}, nil
}
