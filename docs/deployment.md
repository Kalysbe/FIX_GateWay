# Deployment

## Prerequisites

Dashboard business metrics are calculated by PostgreSQL function `gateway_dashboard_compute()`
(migration 000021) and stored in `gateway_dashboard_cache` (migration 000022).
`gateway_dashboard_snapshot()` reads that persisted snapshot immediately. The gateway triggers
`gateway_refresh_dashboard_snapshot()` in the background at most once per minute; a database
advisory lock prevents duplicate refreshes across gateway instances. The migration warms the
cache once. Successful data survives gateway restarts; failed refreshes do not replace it with
zeroes. A lifecycle-bound gateway worker checks refreshes every 15 seconds even without
dashboard visitors. Persisted data older than five minutes remains available with `stale: true`,
and the dashboard displays a warning and `generated_at`. An empty cache returns a retryable
503 response instead of a JSON parsing error.
The authenticated API also shares a 5-second memory cache between viewers (up to one minute
of stale memory data during refresh). Live monitoring renders independently of daily statistics.

Set `PROMETHEUS_URL` for gateway access to Prometheus (`http://prometheus:9090` in Compose,
`http://localhost:9090` for a local process). Monitoring history uses the
[Prometheus range query API](https://prometheus.io/docs/prometheus/latest/querying/api/):
10 minutes of history with 15-second steps, minute-window rates and a shared 15-second cache.
Prometheus downtime does not block business statistics or current runtime measurements.
Both Compose stacks include persistent Prometheus storage.

- Docker + Docker Compose
- Or locally: Go 1.26.7+, Node 20+, PostgreSQL 16

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
| `APP_ENV` | `development` or `production`; production validates strong secrets, verified database TLS and HTTPS |
| `REQUIRE_HTTPS` | Reject non-HTTPS broker/admin traffic when `true`. In `APP_ENV=production` this is mandatory unless `ALLOW_INSECURE_HTTP=true` |
| `HMAC_REQUIRED` | Require `X-Timestamp` / `X-Nonce` / `X-Signature` when `true`. Default `false`, including production; supplied signatures are always checked |
| `GRPC_MTLS_REQUIRED` | Require client certificates when `true`. Default `false`; server TLS and token authorization remain required |
| `GRPC_CLIENT_CERT` / `GRPC_CLIENT_KEY` | Internal HTTP-to-gRPC adapter client credentials. Empty by default; with mTLS enabled, default to `bridge.crt` / `bridge.key` beside `GRPC_TLS_CERT` |
| `TRUSTED_PROXIES` | CIDRs of reverse proxies. `X-Forwarded-For` / `X-Forwarded-Proto` are trusted only from these peers. Leave empty if the gateway is reached directly |
| `ALLOW_INSECURE_DB` | Production override for a connection without TLS or with a plaintext fallback |
| `ALLOW_INSECURE_HTTP` | Production override to boot without `REQUIRE_HTTPS` |
| `DOCS_PUBLIC` | Serve Swagger/OpenAPI. Defaults to off in production |
| `DEFAULT_RATE_LIMIT_RPS` / `DEFAULT_ORDER_RATE_LIMIT` | Per-token request and order rate limits |
| `AUTH_FAIL_MAX_PER_MINUTE` | Brute-force threshold for failed Bearer auth per IP |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | Bootstrap admin |
| `LB_STRATEGY` | `least_connections` (default), `round_robin`, `weighted_round_robin` |
| `CORS_ORIGINS` | Comma-separated origins |
| `QUIK_API_BASE_URL` | Default origin of the internal QUIK Module API (`http://192.168.0.89:5000`). Operators can override it from the admin UI |

Adapter secrets (username/password) are referenced by env var **names** stored in DB (`username_env`, `password_env`), never returned to the frontend.

## Local (without Docker for gateway)

1. Start a separate development PostgreSQL database and set `DATABASE_URL` to it.
2. From `backend/`, run `go run ./cmd/devcerts -out certs` to generate the development TLS certificates.
3. Gateway applies migrations automatically (`MIGRATIONS_PATH=migrations`). The historical UAT migrations enable real ARQA adapters and disable simulators. For a simulator-only environment, use [Local testing](local-testing.md), which prepares the database before gateway startup.
4. For a manually prepared simulator-only database, set the simulator adapter hosts to `127.0.0.1`, disable all ARQA adapters, then run `go run ./cmd/fixsimulator` and `go run ./cmd/gateway` in separate terminals from `backend/`.
5. Run admin frontend: `cd frontend && npm ci && npm run dev`. Broker clients connect through the [REST + gRPC API](broker-api-guide.md).

The supplied Compose stack is a development configuration. Production requires
an operator-managed database connection with verified TLS, HTTPS termination,
VPN-only broker access, strong secrets, and production certificates. Setting
`APP_ENV=production` alone does not configure these dependencies.

## Applying updates and Windows recovery

### Сервер 172.23.10.96 и production QUIK

Для существующего стенда на `172.23.10.96` используется пара `deploy/docker-compose.yml` + `deploy/docker-compose.override.yml`. Override задаёт `ADAPTER_HOST_OVERRIDE=172.23.10.84` для всех FIX-сессий, включая резервную, и `QUIK_API_BASE_URL=http://172.23.10.84:5000`. Админка остаётся на `5173`, API — на `8081`. Локальный тестовый стенд без этого override использует `192.168.0.89`.

При переключении существующей БД также задать `adapters.host=172.23.10.84` для QUIK-адаптеров и `gateway_settings.quik_api_base_url=http://172.23.10.84:5000`: сохранённый origin API имеет приоритет над env. Если подключён опциональный read-only QUIK export, его `QUIK_EXPORT_DATABASE_URL` тоже должен указывать на `172.23.10.84`; credentials и настройки TLS сохранить. Основная БД самого gateway остаётся отдельной.

```bash
docker compose -p deploy -f deploy/docker-compose.yml -f deploy/docker-compose.override.yml up -d --build
```

Эта пара сохраняет настройки существующего стенда и не включает сама по себе строгий production-режим. Для него использовать отдельный `deploy/docker-compose.production.yml` и [production runbook](production-operations.md).

After source changes, run `docker compose up -d --build` from the repository root
to rebuild and deploy, then verify `/health`, `/ready`, and both interface flows.
The PM2 supervisor recovers stopped Compose services using the existing images;
recovery does not rebuild source changes. Its health check covers all configured
services and accepts the successful one-shot certificate generator.
Running containers with failing health checks are explicitly restarted. Windows
boot uses the current user's Node installation; set `GATEWAY_NODE_DIR` and
`GATEWAY_PM2_CLI` if Node or PM2 is installed elsewhere. `PM2_HOME` can be supplied
by the operator and defaults to the current user profile.

## ARQA production

Set `FIX_SESSION_DIR` to durable storage for sequence numbers and outbound FIX
messages. Compose mounts the `fix-sessions` volume at `/app/fix-sessions`.
Without this setting, a local process uses a temporary directory. Preserve this
store together with the database when planning recovery; do not share one FIX
session store between concurrently running gateway processes. Restoring PostgreSQL
alone does not establish safe FIX sequence recovery against ARQA.

1. Complete [arqa-requirements-checklist.md](./arqa-requirements-checklist.md).
2. Insert/update `adapters` rows with `type=arqa`, real host/port/comp IDs.
3. Provide credentials via ENV referenced by `username_env` / `password_env`.
4. Set `reset_on_logon=false` unless ARQA requires reset.
5. Verify `/ready` shows `adapters_available >= 1` and place a UAT order.

## Health

- `GET /health` — process up
- `GET /ready` — DB up and ≥1 healthy adapter
- `GET /metrics` — Prometheus metrics

## Native realtime certificates

gRPC realtime requires server TLS certificates before gateway startup. Broker access defaults to VPN + TLS + Bearer token; mTLS and HMAC are optional. Development Compose provisions TLS PKI; production must replace it with operator-provisioned certificates. See [realtime-grpc.md](realtime-grpc.md) for ports, browser adapter and certificate settings.
