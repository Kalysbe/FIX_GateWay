FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/fixsimulator ./cmd/fixsimulator

FROM alpine:3.20 AS gateway
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /out/gateway /app/gateway
COPY migrations /app/migrations
COPY docs /app/docs
ENV MIGRATIONS_PATH=/app/migrations
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=5 --start-period=15s \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/app/gateway"]

FROM alpine:3.20 AS fixsimulator
WORKDIR /app
COPY --from=builder /out/fixsimulator /app/fixsimulator
EXPOSE 5001 5002
ENTRYPOINT ["/app/fixsimulator"]
