# Logging policy

The default logs retain errors and warnings, failed authentication, admin
logins and audit records, order lifecycle events, and FIX session transitions.

Successful admin GET/HEAD polling, health checks, OPTIONS requests, redundant
token authentication events, and generic outgoing FIX diagnostic events are
excluded from the observability event stream. Slow requests and failures remain
visible, including failed admin reads and health checks.

Successful broker API requests remain recorded because broker activity,
request statistics, latency summaries, and error-rate alerts currently use
these records. Removing them requires moving those aggregates out of the
event log first. The separate security audit remains unchanged.

Routine FIX market-data, position, and instrument synchronization details use
DEBUG in the process logger and are hidden at the default INFO level. Warnings,
errors, session connections, and gateway startup/shutdown remain visible.

This policy applies to new events; existing log history is retained.
