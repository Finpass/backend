# backend (Go)

Go 1.27, chi, pgx/v5, goose (migrations), sqlc (queries), slog.

## Run

```
cp .env.example .env
make run          # :8080
make check        # fmt + lint + test
make migrate-up   # needs DATABASE_URL
```

Health: `GET /healthz` (liveness), `GET /readyz` (pings the database when `DATABASE_URL` is set).

## Architecture: vertical slices + DDD

```
cmd/api/                  entrypoint: config, wiring, graceful shutdown
internal/app/             composition root: builds each slice and mounts its routes
internal/features/<slice>/
    domain/               entities, value rules, repository ports. Pure Go, no HTTP, no SQL
    application/          use cases (one service per slice), orchestrates domain + ports
    infrastructure/       adapters for the ports (Postgres/sqlc, memory)
    transport/            HTTP handlers, DTOs, validation tags, route table
internal/platform/        cross-cutting technical code: config, logger, httpx, database, aiclient
internal/shared/apperr/   the single error type shared by every layer
db/migrations, db/queries goose + sqlc inputs
```

Rules:
- A slice owns everything for its feature. Slices never import each other; share through `platform` or `shared`, or via an application-level port.
- Dependencies point inward: transport -> application -> domain; infrastructure -> domain.
- Handlers have the signature `func(w, r) error`; return an `apperr`, never write error responses by hand.
- Invariants live in the domain constructor (`domain.New`), request-shape checks in DTO `validate` tags.
- `internal/features/note` is a working template (in-memory repo). Copy it for a new slice, then delete it when the first real slice lands.

Contract: [docs/error-contract.md](docs/error-contract.md).

## Adding a Postgres-backed slice

1. `make migrate-new name=create_things`, write SQL.
2. Put queries in `db/queries/things.sql`, run `make sqlc` (generates into `internal/platform/database/sqlcgen`).
3. Implement the domain repository in `infrastructure/` on top of the generated code; translate `pgx.ErrNoRows` to `NOT_FOUND` and unique violations to `CONFLICT`.

## Auth slice

`internal/features/auth` (domain / application / infrastructure / transport). Endpoints under `/api/v1/auth`:

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/register` | create unverified account, email a 6-digit OTP |
| POST | `/verify-email` | consume OTP, mark verified, start session |
| POST | `/resend-otp` | new OTP (60s cooldown, never reveals accounts) |
| POST | `/login` | email + password (`remember` extends the refresh cookie to 30 days) |
| POST | `/google` | exchange a Google access token for a session |
| POST | `/forgot-password`, `/reset-password` | OTP-based reset, ends all sessions |
| POST | `/refresh`, `/logout` | rotate / revoke the `refresh_token` httpOnly cookie |
| GET | `/me` | current user (Bearer access token) |

Access token: 15 min JWT (HS256) returned in JSON. Refresh token: opaque, stored hashed, rotated on every use; a replay after a 10s grace window revokes every session of the user.

Config (`.env`): `JWT_SECRET` (32+ chars), `GOOGLE_CLIENT_ID`, `SMTP_*`, `MAIL_FROM`, TTLs. With no `SMTP_HOST` the OTP is printed to the log (development only; production requires SMTP). Auth is mounted only when `DATABASE_URL` is set; run `make migrate-up` first.
