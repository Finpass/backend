-- +goose Up
CREATE TABLE whatsapp_contacts (
    user_id          TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    phone            TEXT,                                   -- E.164, personal data (UU PDP): masked in logs
    verified         BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at      TIMESTAMPTZ,
    opt_in           BOOLEAN NOT NULL DEFAULT FALSE,
    paused_at        TIMESTAMPTZ,                            -- set by STOP
    send_time_local  TEXT NOT NULL DEFAULT '07:00',
    timezone         TEXT NOT NULL DEFAULT 'Asia/Jakarta',
    frequency        TEXT NOT NULL DEFAULT 'daily' CHECK (frequency IN ('daily', 'weekdays', 'weekly')),
    locale           TEXT NOT NULL DEFAULT 'id' CHECK (locale IN ('id', 'en')),
    last_sent_at     TIMESTAMPTZ,
    last_inbound_at  TIMESTAMPTZ,
    fail_streak      INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- one verified owner per number
CREATE UNIQUE INDEX whatsapp_contacts_phone_verified_uq ON whatsapp_contacts (phone) WHERE verified;
CREATE INDEX whatsapp_contacts_phone_idx ON whatsapp_contacts (phone);

CREATE TABLE whatsapp_verifications (
    user_id    TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    phone      TEXT NOT NULL,
    code_hash  TEXT NOT NULL,
    attempts   INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE whatsapp_messages (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    direction   TEXT NOT NULL CHECK (direction IN ('in', 'out')),
    kind        TEXT NOT NULL,                               -- briefing|test|verification|reply|command
    body        TEXT NOT NULL DEFAULT '',                    -- never holds verification codes
    status      TEXT NOT NULL,                               -- sent|failed|received
    provider_id TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX whatsapp_messages_user_idx ON whatsapp_messages (user_id, created_at DESC);

CREATE TABLE briefing_runs (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    run_date        DATE NOT NULL,                           -- user-local date
    status          TEXT NOT NULL CHECK (status IN ('sending', 'sent', 'failed', 'retry')),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ,
    fallback        TEXT NOT NULL DEFAULT '',
    content         JSONB NOT NULL DEFAULT '{}',             -- selection keys used for suppression/variety
    char_count      INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, run_date)
);

CREATE INDEX briefing_runs_retry_idx ON briefing_runs (status, next_attempt_at);

CREATE TABLE whatsapp_events (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,                                -- briefing_sent|briefing_failed|slot_included|reply|command|opt_out|opt_in|delivery_issue
    slot       TEXT NOT NULL DEFAULT '',
    meta       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX whatsapp_events_user_idx ON whatsapp_events (user_id, created_at DESC);
CREATE INDEX whatsapp_events_kind_idx ON whatsapp_events (kind, created_at DESC);

-- +goose Down
DROP TABLE whatsapp_events;
DROP TABLE briefing_runs;
DROP TABLE whatsapp_messages;
DROP TABLE whatsapp_verifications;
DROP TABLE whatsapp_contacts;
