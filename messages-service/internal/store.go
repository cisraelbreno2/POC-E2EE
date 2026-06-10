package internal

import (
	"context"
	"database/sql"
	"time"

	pb "e2ee/pkg/pb/messagespb"
	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewStore(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(10)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveMessage(ctx context.Context, msg *pb.SendMessageRequest) error {
	const query = `
		INSERT INTO messages (from_user_id, to_user_id, ephemeral_public_key, encrypted_payload, signature)
		VALUES ($1, $2, $3, $4, $5);
	`
	_, err := s.db.ExecContext(
		ctx,
		query,
		msg.From,
		msg.To,
		msg.EphemeralPublicKey,
		msg.EncryptedPayload,
		msg.Signature,
	)
	return err
}

func (s *Store) ListMessagesForUser(ctx context.Context, userID string) ([]*pb.EncryptedMessage, error) {
	const query = `
		SELECT id, from_user_id, to_user_id, ephemeral_public_key, encrypted_payload, signature, created_at
		FROM messages
		WHERE to_user_id = $1
		ORDER BY created_at ASC, id ASC;
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*pb.EncryptedMessage
	for rows.Next() {
		var msg pb.EncryptedMessage
		var createdAt time.Time
		if err := rows.Scan(
			&msg.Id,
			&msg.From,
			&msg.To,
			&msg.EphemeralPublicKey,
			&msg.EncryptedPayload,
			&msg.Signature,
			&createdAt,
		); err != nil {
			return nil, err
		}
		msg.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		messages = append(messages, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}
