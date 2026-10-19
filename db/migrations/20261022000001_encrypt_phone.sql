-- +goose Up
-- Phone numbers are now stored encrypted in the application (see internal/platform/crypto). Equality lookups and the
-- "one verified owner per number" rule move to a keyed blind index; existing rows are encrypted at the next start.
ALTER TABLE whatsapp_contacts ADD COLUMN phone_hash TEXT;
CREATE UNIQUE INDEX whatsapp_contacts_phone_hash_verified_uq ON whatsapp_contacts (phone_hash) WHERE verified;
CREATE INDEX whatsapp_contacts_phone_hash_idx ON whatsapp_contacts (phone_hash);
DROP INDEX whatsapp_contacts_phone_verified_uq;
DROP INDEX whatsapp_contacts_phone_idx;

-- +goose Down
-- Only valid while the phone column still holds plaintext (before encryption is enabled).
CREATE INDEX whatsapp_contacts_phone_idx ON whatsapp_contacts (phone);
CREATE UNIQUE INDEX whatsapp_contacts_phone_verified_uq ON whatsapp_contacts (phone) WHERE verified;
DROP INDEX whatsapp_contacts_phone_hash_idx;
DROP INDEX whatsapp_contacts_phone_hash_verified_uq;
ALTER TABLE whatsapp_contacts DROP COLUMN phone_hash;
