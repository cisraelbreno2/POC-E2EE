CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	identity_public_key TEXT NOT NULL,
	dh_public_key TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
