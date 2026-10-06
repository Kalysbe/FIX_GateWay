# Gateway rate limiting

The trading ingress in this repository is REST, authenticated by API token and
the broker identity loaded by `brokerAuth`. The exchange connection is FIX.
The existing bidirectional gRPC service is admin realtime, authenticated by JWT
and mTLS; it accepts Subscribe and Ping, not trading submissions. No protobuf
or trading workflow was changed.

## Implementation choice

Three options were considered: local buckets in one Gateway, local buckets with
sticky broker routing across replicas, and a distributed Redis budget. The
current deployment has one active Gateway, so local admission avoids introducing
a network dependency in the trading path. Redis in the neighboring Broker_App
deployment is not used by this Gateway.

**Limits are per process.** Before running multiple Gateway replicas, route all
tokens/connections of a broker to one replica and explicitly partition global
capacity between replicas, or implement distributed admission. Sticky routing
alone does not enforce a cluster-wide global limit or survive failover without
a possible fresh burst. Do not claim these metrics or limits are cluster-wide.

## Admission

Global aggregate (if configured), global category, broker aggregate (if
configured), broker category, operation, and each layer's concurrency admission
are checked before business logic. Each layer is nonblocking. Partial admission
releases all acquired concurrency slots when a subsequent layer rejects.
Consumed rate tokens are not refunded on downstream rejection: global protection
counts attempted load. There is no request waiting queue inside the limiter.
The existing durable order outbox is unchanged.

GCRA implements sustained rate and burst with an atomic virtual arrival time.
The hot path has no database/network calls or global mutex. Concurrency uses CAS
and release is idempotent. Buckets are keyed by authenticated broker ID, never
IP, token or connection. Rates, bursts and concurrency are independent values.
Increasing a rate may briefly retain debt accumulated at the previous rate;
configuration refresh never resets an exhausted bucket.

Broker categories: Trading (SubmitOrder, CancelOrder, MaintainPositions), Queries (GetOrder,
GetOrders, GetSecurities, GetPositions, GetAccounts, GetTrades, GetOrderBook, OtherQuery), System (GetFirm). Actual HTTP
route names are mapped to bounded operation names, never arbitrary URLs. There
is no ReplaceOrder route to integrate in this repository. Add future route/RPC
mappings at their authenticated dispatch point and configure a policy; the
limiter itself does not need new algorithms or operation-specific branches.

The existing broker fields `rate_limit_rps` and `rate_limit_orders_per_sec` remain
inherited category defaults. An explicit category policy overrides its inherited
default. **Raising SubmitOrder to 5000 while leaving Trading at 20 still limits
the broker to 20/s.** Edit all relevant parent policies for the intended budget.
Disabling an operation policy removes only that layer; parents still apply.

System is independent of Queries, so polling order statuses no longer consumes
the GetFirm category budget. Broker_App's per-order HealthCheck and interpretation
of 429 as Offline are separate client behavior; this change does not remove those
checks or silently retry failed real orders.

REST rejections use HTTP 429, code RESOURCE_EXHAUSTED, Retry-After in whole
seconds and retry_after_ms. Authenticate first, then admit. Existing process-wide
pre-auth concurrency protection remains in place to bound authentication DB
work; it can reject with 503 before per-broker admission. This limiter does not
eliminate authentication's existing DB work per request.

Admin gRPC admission uses the authenticated user ID in a separate admin scope.
Connect holds a concurrency slot for the stream lifetime; Subscribe and Ping
are checked per incoming message across all streams of that user. Rejection ends
the stream with ResourceExhausted and google.rpc.RetryInfo. Existing slow-consumer
protection controls outbound events; inbound trading admission does not limit
OrderAccepted/Trade/PositionChanged delivery.

## Configuration and audit

Migration 000020 creates `rate_limit_policies`. Existing rates are preserved;
initial global safety policies are configurable rows, not Go constants.
The process refuses startup if policies cannot be loaded. Refresh runs every
second; errors preserve the last known configuration. Admin updates refresh
immediately without disconnecting existing streams. Restart restores policy,
but runtime counters and available burst restart with the process.

GET /api/v1/admin/rate-limits returns explicit policies, effective inherited
policies, per-process usage and available operation mappings.
PUT on the same path requires ADMIN or SUPER_ADMIN, accepts scope, subject,
operation, rate, burst, concurrent, enabled and a change reason. Broker subject
is its numeric database ID; global/admin subject is '*'. Admin wildcard policies
create separate runtime buckets per authenticated user. Policy and audit writes
commit in the same PostgreSQL transaction, with actor, stored before-state,
after-state, broker, reason and timestamp.

Rate Limits in the React console edits these values and shows current/peak
attempted traffic, concurrency, rejected totals and status. Current RPS is the
count in the current second, not a moving average. Prometheus rate() is preferable
for longer windows. No arbitrary business counter reset occurs at second boundaries.

RATE_LIMIT_MAX_STATES bounds runtime bucket count (default 100000). State is
retained until restart so active identities cannot gain a fresh bucket by churn.
Exhaustion fails closed without allocating new buckets; budget this capacity for
all broker/admin category and operation combinations. Disabled gates retain past
statistics. Runtime statistics are local and reset after restart.

## Monitoring

The grpc_rate_limit_* metric family covers shared REST/gRPC admission despite the
prefix requested in the specification. Labels are scope, broker_id (admin user ID
for admin scope), method (operation/category). Each gate counts requests reaching
that layer; don't sum category and operation counters as end-to-end order totals.
Global rejection prevents a request from reaching downstream broker counters.
Use global rejection and per-layer rejection together. Configured policy gauges
exist before traffic. Limiter latency is a histogram in seconds.

Prometheus rules cover 80/90 percent utilization for broker/global budgets,
repeated rejections, concurrency saturation and p99 admission latency. The
Grafana dashboard JSON is importable; select the Prometheus datasource. No Grafana
or Alertmanager is provisioned by the current Compose deployment. Rules evaluate
locally, but notifications need the operator's Alertmanager configuration.

Rate-limit warning logs are sampled once per gate per second; request diagnostics
for unsampled admission rejects are suppressed. Metrics count every rejected
decision. No accepted-order audit is suppressed.

## Verification and deployment

Run `go test -race ./...`, and the opt-in integration test with
RATE_LIMIT_TEST_DATABASE_URL pointing to an empty disposable database named
rate_limit_test. Integration covers policy persistence, before/after audit and
restart. gRPC tests use real generated clients and multiple streams sharing one
identity, including updates without reconnecting.

`go test ./internal/ratelimit -run TestAdmissionLoadProfiles -v` exercises paced
1000, 5000, 10000, 20000, 50000 attempts/s in memory. The deterministic overload
test validates 10000 attempts/s vs a 5000/s policy plus initial burst and another
independent broker. `go test ./internal/ratelimit -run '^$' -bench BenchmarkAdmission
-benchmem` measures parallel admission CPU time and allocations. These are limiter
tests, not evidence of 50000 real trading orders/s through PostgreSQL/FIX/QUIK.
Full trading capacity must be tested against an isolated simulator deployment.

Apply migration 000020 through the normal startup migration mechanism when
deploying the rebuilt Gateway and frontend. Reload Prometheus with the mounted
rule file. Tune global/process concurrency after measuring the whole trading
pipeline. This implementation does not automatically raise production broker
limits or replay failed orders.
