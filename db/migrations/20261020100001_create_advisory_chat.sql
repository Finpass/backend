-- +goose Up
CREATE TABLE chat_sessions (
    id         UUID PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_sessions_user_idx ON chat_sessions (user_id, updated_at DESC);

CREATE TABLE chat_messages (
    id         BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES chat_sessions (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content    TEXT NOT NULL,
    citations  JSONB,
    trace      JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_messages_session_idx ON chat_messages (session_id, id);

-- +goose Down
DROP TABLE chat_messages;
DROP TABLE chat_sessions;
