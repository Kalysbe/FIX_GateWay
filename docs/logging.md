# Logging policy

The default logs retain errors and warnings, failed authentication, admin
logins and audit records, order lifecycle events, and FIX session transitions.

HTTP request records are emitted only for POST, PATCH and DELETE, for both
brokers and admins. GET, HEAD, PUT and OPTIONS are excluded even when slow or
failed; the corresponding HTTP error record is excluded too. Existing records
remain in history. Independent authentication/security audit and business events
are still retained; they are not HTTP access records.

Event-log aggregates (broker activity, daily request counts, latency summaries
and error rates) now cover these recorded methods only. Process HTTP metrics
continue counting all methods. Routine token authentication successes and
generic outgoing FIX diagnostic events remain excluded.

Routine FIX market-data, position, and instrument synchronization details use
DEBUG in the process logger and are hidden at the default INFO level. Warnings,
errors, session connections, and gateway startup/shutdown remain visible.

This policy applies to new events; existing log history is retained.
