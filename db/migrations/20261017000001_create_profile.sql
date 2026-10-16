-- +goose Up
CREATE TABLE profile_consents (
    user_id         TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    accepted        BOOLEAN NOT NULL,
    whatsapp_opt_in BOOLEAN NOT NULL DEFAULT FALSE,
    accepted_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE profiles (
    user_id         TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    status          TEXT NOT NULL CHECK (status IN ('draft', 'completed')),
    current_section TEXT NOT NULL DEFAULT 'section_1',
    answers         JSONB NOT NULL DEFAULT '{}',
    opt_out         TEXT[] NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE profile_assessments (
    id                    TEXT PRIMARY KEY,
    user_id               TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    questionnaire_version TEXT NOT NULL,
    rule_version          TEXT NOT NULL,
    input                 JSONB NOT NULL,
    trace                 JSONB NOT NULL,
    output                JSONB NOT NULL,
    explanation           JSONB,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX profile_assessments_user_idx ON profile_assessments (user_id, created_at DESC);

-- Assessments are an audit trail: rows may be erased with the user but never edited.
-- +goose StatementBegin
CREATE FUNCTION profile_assessments_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'profile_assessments rows are immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER profile_assessments_no_update
    BEFORE UPDATE ON profile_assessments
    FOR EACH ROW EXECUTE FUNCTION profile_assessments_immutable();

-- +goose Down
DROP TABLE profile_assessments;
DROP FUNCTION profile_assessments_immutable();
DROP TABLE profiles;
DROP TABLE profile_consents;
