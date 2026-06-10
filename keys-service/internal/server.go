package internal

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"e2ee/pkg/jwtutils"
	pb "e2ee/pkg/pb/keyspb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type KeysServer struct {
	pb.UnimplementedKeysServiceServer
	store     *Store
	jwtSecret string
}

func NewKeysServer(store *Store, jwtSecret string) *KeysServer {
	return &KeysServer{store: store, jwtSecret: jwtSecret}
}

func (s *KeysServer) RegisterUser(ctx context.Context, req *pb.RegisterUserRequest) (*pb.RegisterUserResponse, error) {
	if req.Id == "" || req.IdentityPublicKey == "" || req.DhPublicKey == "" {
		return nil, status.Error(codes.InvalidArgument, "id and public keys are required")
	}

	err := s.store.RegisterUser(ctx, req.Id, req.IdentityPublicKey, req.DhPublicKey)
	if err != nil && !errors.Is(err, ErrAlreadyExists) {
		slog.Error("failed to register user", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	token, err := jwtutils.GenerateToken(req.Id, s.jwtSecret, 24*time.Hour)
	if err != nil {
		slog.Error("failed to generate jwt", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.RegisterUserResponse{Token: token}, nil
}

func (s *KeysServer) GetPublicKey(ctx context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	identityPubKey, dhPubKey, err := s.store.GetPublicKey(ctx, req.Id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		slog.Error("failed to get public key", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.GetPublicKeyResponse{
		Id:                req.Id,
		IdentityPublicKey: identityPubKey,
		DhPublicKey:       dhPubKey,
	}, nil
}
