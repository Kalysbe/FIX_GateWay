FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/trade-reconcile ./cmd/trade-reconcile
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/fixsimulator ./cmd/fixsimulator
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/devcerts ./cmd/devcerts
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/admin-password ./cmd/admin-password

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS gateway
RUN apk add --no-cache ca-certificates tzdata wget
RUN addgroup -g 10001 gateway && adduser -D -H -u 10001 -G gateway gateway && mkdir -p /app/fix-sessions && chown -R gateway:gateway /app
WORKDIR /app
COPY --from=builder /out/gateway /app/gateway
COPY --from=builder /out/trade-reconcile /app/trade-reconcile
COPY --from=builder /out/admin-password /app/admin-password
COPY migrations /app/migrations
COPY docs /app/docs
ENV MIGRATIONS_PATH=/app/migrations
EXPOSE 8080
EXPOSE 8443
EXPOSE 9080
USER 10001:10001
HEALTHCHECK --interval=10s --timeout=5s --retries=5 --start-period=15s \
  CMD wget -qO- http://127.0.0.1:9080/health || exit 1
ENTRYPOINT ["/app/gateway"]

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS devcerts
RUN mkdir /certs && chown 10001:10001 /certs
COPY --from=builder /out/devcerts /app/devcerts
USER 10001:10001
ENTRYPOINT ["/app/devcerts"]

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS fixsimulator
WORKDIR /app
COPY --from=builder /out/fixsimulator /app/fixsimulator
EXPOSE 5001 5002
USER 10001:10001
ENTRYPOINT ["/app/fixsimulator"]
