CREATE TABLE IF NOT EXISTS messages (
	id BIGSERIAL PRIMARY KEY,
	from_user_id TEXT NOT NULL,
	to_user_id TEXT NOT NULL,
	ephemeral_public_key TEXT NOT NULL,
	encrypted_payload TEXT NOT NULL,
    signature TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_messages_to_user_created_at
	ON messages (to_user_id, created_at, id);
