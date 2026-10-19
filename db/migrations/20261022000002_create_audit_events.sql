-- +goose Up
-- Security audit trail. Append-only and hash-chained: every row commits to the one before it, so edits or removals are
-- detectable. actor_id is a pseudonymous user id with no FK on purpose: erasing a user must not rewrite the trail.
CREATE TABLE audit_events (
    id         BIGSERIAL PRIMARY KEY,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id   TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    object     TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    meta       JSONB NOT NULL DEFAULT '{}',
    prev_hash  TEXT NOT NULL DEFAULT '',
    row_hash   TEXT NOT NULL
);

CREATE INDEX audit_events_actor_idx ON audit_events (actor_id, id DESC);
CREATE INDEX audit_events_action_idx ON audit_events (action, id DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events rows are append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_events_no_change BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();

-- +goose Down
DROP TRIGGER audit_events_no_change ON audit_events;
DROP FUNCTION audit_events_immutable();
DROP TABLE audit_events;
