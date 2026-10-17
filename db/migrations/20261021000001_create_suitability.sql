-- +goose Up
-- Audit trail of suitability decisions (portfolio suggestions and product comparisons): the profile facts and the
-- rule outcomes at the time. No name, email or phone; removed with the user (PDP right to delete).
CREATE TABLE suitability_records (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('comparison', 'portfolio')),
    digest     TEXT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX suitability_user_idx ON suitability_records (user_id, id DESC);

-- +goose Down
DROP TABLE suitability_records;
