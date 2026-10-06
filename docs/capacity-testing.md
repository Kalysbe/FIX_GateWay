# Local FIX capacity measurement

Run `node scripts/local-capacity.mjs` from the repository root with the isolated
`deploy/docker-compose.local.yml` stack running. The script accepts only the
fixed loopback port 28080, verifies the internal simulator network/database and
requires simulator adapters. It never targets the UAT listener on port 8080.

The benchmark creates a dedicated broker/account/token, raises only that
broker's rate limits, sends alternating BUY/SELL limit orders, and measures
each adapter separately and then both together. Adapter settings are restored
and the token is revoked on normal completion or a handled error. It retains
orders and execution records for auditing; it does not delete trading records.

The default screening uses 20-second steps at 10, 25, 50, 100, 200, and 400
offered orders/second and stops a mode at its first unstable step. In PowerShell,
a longer confirmation run can be configured with:

```powershell
$env:CAPACITY_SECONDS = '60'
$env:CAPACITY_RATES = '50,60,75'
$env:CAPACITY_MODES = 'adapter-1,adapter-2,both'
$env:CAPACITY_WARMUP = '1' # separate 10-second, 10/s warm-up per mode
node scripts/local-capacity.mjs
```

Durations are limited to 10–120 seconds per step and rates to 1–1000/s. At most
128 HTTP requests are in flight. Generator skips, HTTP admission refusals, and
network failures are recorded separately from accepted requests. A refused
request is not treated as a lost accepted order. There are no automatic retries
during a measured step because those would change the offered load.

JSON evidence is written under `artifacts/capacity/<run-id>.json` (gitignored).
ACK latency uses the first persisted ACKNOWLEDGED event minus order creation;
fill latency uses the persisted execution timestamp. The simulator schedules
fills three seconds after sending ACK, so fill latency includes this intentional
delay. Sampled throughput uses differences in DB counts after five seconds of
warm-up. These are end-to-end measurements, including persistence, not wire-only
FIX timings.

Warm-up records are tagged separately and excluded from the minute-step report.
Input rates are nominally paced; launching SQL sampling processes on Windows can
cause short submission bursts. Inspect measured accepted/ACK rates and admission
refusals rather than treating the requested input rate as a precise wire rate.

The `stable` screening criterion requires at least 95% of the offered load
accepted, no network failures, no generator skips, a bounded ending queue,
small measured queue growth, ACK p95 below 1500ms, and no FAILED outbox entries.
This is explicitly not a zero-refusal guarantee: consult `statuses` and
`rejectionFree` independently. After stopping submissions the script waits for
the accepted queue to drain, then requires one durable outbox row, a venue ACK,
and exactly one full fill for every accepted order.

A tested stable step is a lower bound and the first overloaded step is an upper
bound for this environment. Both adapters share one serial outbox worker, so
the benchmark cannot establish an independent physical ceiling of each adapter
or predict a remote venue's limit. Longer soak tests, cancellations, reconnects,
venue throttling and other workloads require separate measurements.
