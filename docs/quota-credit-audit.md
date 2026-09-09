# Wallet Credit Audit

The administrator's user quota dialog has two separately paginated views:

- `view=credits`: committed wallet increases from the main database's
  `quota_credits` table. Each row contains the raw integer quota delta, source,
  time, and available order/task/request/operator identifiers.
- `view=legacy`: existing recharge, refund, system, administrator quota changes,
  and negative-consumption logs. These are evidence, not a reconstructed ledger.
  Omitting `view` preserves the original legacy API response.

Both use `GET /api/user/:id/quota/log?p=1&page_size=10` and the same administrator
role restrictions as user management. The UI explicitly selects `credits` first.

## Coverage

New wallet increases are recorded in the same main-database transaction as the
balance mutation: payment callbacks and manual completions, redemption, check-in,
registration/initial quota, invitee rewards, invitation balance transfers,
administrator additions and positive overrides, and wallet refunds/adjustments.
Failed credit-record writes roll back the corresponding database balance change.
For batched wallet operations, each positive event is retained separately even
when the batch's net balance change is zero or negative. The timestamp is captured
when the credit is queued; records appear in the dialog only after the batch commits.
Existing payment
idempotency controls remain in place; repeated successful refunds from a buggy
caller produce separate records, making the anomaly visible.

Subscription allowance and invitation rewards not yet transferred to the wallet
are not wallet income. Administrator decreases and downward overrides remain in
the legacy management log rather than the positive-only credit table.

Historical unlogged or deleted events cannot be reconstructed. No backfill uses
today's exchange rate to infer old integer deltas. Direct SQL changes and external
writers bypassing these model methods are outside this application-level audit.
Deleting business logs does not delete new credit records. Back up the main
database and restrict direct database write access for stronger audit retention.

## Deployment

The new table is included in both normal and fast GORM migrations. The table uses
portable relational columns for SQLite, MySQL and PostgreSQL, independently of
the configured business log database (including ClickHouse).

Wallet batching follows `BATCH_UPDATE_ENABLED` and `BATCH_UPDATE_INTERVAL`
(default 5 seconds). With Redis enabled, the authoritative live wallet is a
non-expiring `wallet:v1:<user_id>` hash, independent of the expiring authentication
cache. Reservations, settlements and refunds atomically update it and append to
`wallet:v1:<user_id>:events`. `wallet:v1:dirty` identifies wallets awaiting SQL.
Each flush combines a user's deltas into one balance update; positive events are
inserted in chunks of 80 in the same transaction as `users.wallet_sequence`.
Usage counters and token statistics retain their separate existing batching.

The SQL user-row lock and a Redis busy barrier coordinate flushing and immediate
credits. SQLite acquires its writer lock before reading the checkpoint. A later
writer can recover an interrupted flush: events at or below the SQL checkpoint
are not applied again. An SQL commit followed by failed Redis acknowledgment does
not report a failed payment (which could invite a duplicate retry); the wallet
remains fenced until a subsequent flush or wallet read completes acknowledgment.
The recovery loop runs even when normal batching is disabled.

Payment callbacks, administrator credits/overrides, redemption and other explicitly
immediate operations drain pending wallet events before changing the SQL balance.
With batching disabled, ordinary changes attempt an immediate checkpoint; a failed
checkpoint retains the accepted Redis journal for retry. At 4096 pending events,
or when a flush races a mutation, the mutation serializes through SQL instead of
silently dropping a charge. Without Redis, wallets that have never used this journal
use synchronous SQL transactions, including conditional reservations. In-memory
wallet batching without Redis cannot guarantee these invariants and is not used.

Batching reduces balance updates and transaction commits, not the number of credit
records retained. The flush interval is approximate: database execution or failures
can delay visibility beyond 5 seconds. Pending balances and audit records are not
kept in application memory: another process using the same Redis/SQL pair can flush
them. Redis failure does NOT fall back to an old SQL balance. Once enrolled,
`users.wallet_epoch` prevents rebuilding a missing Redis wallet from SQL or switching
that wallet to Redis-disabled mode. Missing journal events also fail closed.

Redis is now accounting storage, not a disposable cache. Use a dedicated, adequately
sized instance with `maxmemory-policy noeviction`, persistence, access restrictions
and monitored backups. `appendonly yes` with `appendfsync always` minimizes the local
crash-loss window but does not make asynchronous failover lossless. Redis rollback
to an older backup or replica may restore a self-consistent old journal; it cannot
be detected on every request without additional durable coordination. Stop all
writers and reconcile Redis/SQL before restoring or failing over with data loss.
Do not use FLUSHDB, cache-clear scripts, mixed-version writers, or manual quota SQL
against this state. Do not clear `wallet_epoch` to bypass a missing-wallet error.

Operation IDs prevent a Redis client's automatic command retry from applying the
same mutation twice within ten minutes. These expiring keys consume memory
proportional to mutation rate. They are not durable request-level idempotency:
separate calls to a refund or credit method are separate events. Existing payment
and task idempotency guards remain necessary. A request lost before its settlement
is accepted by Redis/SQL is outside this journal's recovery boundary; this change
does not add a durable retry queue for all upstream request settlements.

Benchmark Redis persistence latency, journal memory and credit retention before
upgrading a high-throughput deployment. Normal batched mutations do not issue SQL
updates; enrollment, flushing, immediate credits and contention recovery do.

For a complete audit boundary, drain traffic, allow in-flight requests/tasks and
the old batch updater to finish, verify successful batch flushes, migrate the
primary database, and upgrade all writer nodes before resuming traffic. Old nodes
or third-party writers will not maintain the new wallet checkpoint. This release
adds `users.wallet_epoch` and `users.wallet_sequence`; back up SQL and Redis together.
Rollback also requires draining and reconciling all journal events before starting
old binaries. Do not roll back only the application while leaving pending events.

## Regression Checks

`model/wallet_journal_test.go` covers cache expiry/invalidation, commit-to-ack
hydration, lost acknowledgments, new Redis clients, concurrent reservations and
flushes, missing state/events, Redis-disabled fail-closed behavior, pending-debit
overrides and the absence of per-mutation SQL writes in normal batching.
It also exercises all payment providers with pending debits, command replay,
first-enrollment rollback, malformed Redis keys, integer bounds, stale profile
updates and concurrent SQLite connections against a file-backed database.
Existing credit tests cover atomic audit failure, net-zero batches and partial
multi-row insert failure. `service/billing_session_wallet_integrity_test.go` covers
trusted/zero-estimate wallet settlement, exhausted/deleted tokens, token write
failure, repeated settlement, funding retry and refund lifecycle guards.

These local fixtures use SQLite and miniredis. They do not replace production-like
MySQL/PostgreSQL transaction tests, real Redis persistence/failover tests, or load
testing. The existing dialect SQL tests verify row-lock generation, not server
behavior under concurrent connections.

Local verification on 2026-09-09: full `go test ./... -count=1`, targeted wallet
regressions repeated three times, and `go vet ./model ./service` passed. One initial
HTTP/2 GOAWAY test failed with a Windows connection reset; three isolated reruns
and the next full run passed. `-race` was unavailable because this environment
does not have CGo enabled. Real Redis, MySQL and PostgreSQL servers were not tested.

PostgreSQL 9.6 legacy history uses bounded application-side JSON parsing to avoid
failing on malformed historical text. Its total count scans candidate legacy
events; prefer the indexed credit view for ongoing investigations.
