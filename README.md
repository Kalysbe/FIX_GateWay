# Broker FIX Gateway

High-throughput Go gateway that routes broker orders to QUIK through replaceable FIX Adapter instances (ARQA / local simulator), with a React admin console.

## Quick start (Docker)

```bash
# from repo root
docker compose up --build

# or
cd deploy
cp .env.example .env
docker compose --env-file .env up --build
```

Copy `deploy/.env.example` → `deploy/.env` to override secrets.

The existing deployment includes ARQA UAT adapter seeds. For an isolated simulator
environment with a separate database and both interfaces, use
[Local testing](docs/local-testing.md).

- Admin UI: http://localhost:5173 (`admin@localhost` / `admin123`)
- API + Swagger: http://localhost:8080/swagger
- Prometheus: http://localhost:9090
- Two FIX simulator acceptors: `:5001`, `:5002`

## Docs

- [Broker API guide (stage 1)](docs/broker-api-guide.md)
- [Architecture](docs/architecture.md)
- [Сверка неопределённых отправок](docs/reconciliation.md)
- [Торговые ограничения](docs/trading-limits.md)
- [Проверка торгового релиза](docs/release-verification.md)
- [Deployment](docs/deployment.md)
- [ARQA requirements checklist](docs/arqa-requirements-checklist.md)
- [План ARQA UAT](docs/arqa-uat-plan.md)
- [ARQA FIX contract](docs/arqa-fix-contract.md)

## Tests

```bash
cd backend
go test ./...
```

## Layout

```text
backend/   Go gateway, migrations, OpenAPI, fixsimulator
frontend/  React admin (Vite + TypeScript + TanStack Query)
deploy/    docker-compose + Prometheus scrape config
docs/      architecture and ARQA contract
```
