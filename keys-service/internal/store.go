package internal

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/lib/pq"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
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

func (s *Store) RegisterUser(ctx context.Context, id, identityPubKey, dhPubKey string) error {
	const query = `
		INSERT INTO users (id, identity_public_key, dh_public_key)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING;
	`
	res, err := s.db.ExecContext(ctx, query, id, identityPubKey, dhPubKey)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAlreadyExists
	}
	return nil
}

func (s *Store) GetPublicKey(ctx context.Context, id string) (string, string, error) {
	const query = `SELECT identity_public_key, dh_public_key FROM users WHERE id = $1`
	var identityPubKey, dhPubKey string
	err := s.db.QueryRowContext(ctx, query, id).Scan(&identityPubKey, &dhPubKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return identityPubKey, dhPubKey, nil
}
