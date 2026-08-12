# Deployment

## Prerequisites

- Docker + Docker Compose
- Or locally: Go 1.22+, Node 20+, PostgreSQL 16

## Docker Compose (recommended)

```bash
# repo root
docker compose up --build

# or from deploy/
cp .env.example .env   # optional overrides
docker compose --env-file .env up --build
```

Services:

| Service | URL |
|---------|-----|
| Gateway API | http://localhost:8080 |
| Swagger UI | http://localhost:8080/swagger |
| Admin UI | http://localhost:5173 |
| Prometheus | http://localhost:9090 |
| FIX simulators | localhost:5001, :5002 |

Default admin:

```text
email: admin@localhost
password: admin123
```

## Environment

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | PostgreSQL DSN (local default: `postgres://postgres:123@localhost:5432/fix_gateway?sslmode=disable`) |
| `JWT_SECRET` | Admin JWT HMAC secret |
| `API_TOKEN_PEPPER` | Pepper for API token hashing |
| `APP_ENV` / `TOKEN_PREFIX` | `sk_live_` in production, `sk_test_` otherwise (or override) |
| `REQUIRE_HTTPS` | Reject non-HTTPS broker/admin traffic when `true` |
| `HMAC_REQUIRED` | Require `X-Timestamp` / `X-Nonce` / `X-Signature` when `true` |
| `DEFAULT_RATE_LIMIT_RPS` / `DEFAULT_ORDER_RATE_LIMIT` | Per-token request and order rate limits |
| `AUTH_FAIL_MAX_PER_MINUTE` | Brute-force threshold for failed Bearer auth per IP |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | Bootstrap admin |
| `LB_STRATEGY` | `least_connections` (default), `round_robin`, `weighted_round_robin` |
| `CORS_ORIGINS` | Comma-separated origins |

Adapter secrets (username/password) are referenced by env var **names** stored in DB (`username_env`, `password_env`), never returned to the frontend.

## Local (without Docker for gateway)

1. Start Postgres and apply migrations (`MIGRATIONS_PATH=migrations`).
2. Update seeded adapters host from `fixsimulator` to `127.0.0.1` if needed.
3. Run `go run ./cmd/fixsimulator` and `go run ./cmd/gateway`.
4. Run frontend: `cd frontend && npm install && npm run dev`.

## ARQA production

1. Complete [arqa-requirements-checklist.md](./arqa-requirements-checklist.md).
2. Insert/update `adapters` rows with `type=arqa`, real host/port/comp IDs.
3. Provide credentials via ENV referenced by `username_env` / `password_env`.
4. Set `reset_on_logon=false` unless ARQA requires reset.
5. Verify `/ready` shows `adapters_available >= 1` and place a UAT order.

## Health

- `GET /health` — process up
- `GET /ready` — DB up and ≥1 healthy adapter
- `GET /metrics` — Prometheus metrics
