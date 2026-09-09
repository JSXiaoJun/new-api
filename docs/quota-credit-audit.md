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
Failed credit-record writes roll back the wallet increase. Existing payment
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

Wallet debits and credits now write synchronously even with batching enabled.
Keeping a debit only in memory while committing its refund could otherwise mint
quota after a process restart. Token and usage statistics remain batched. Expect
additional database writes for wallet billing; benchmark with production traffic
before upgrading a high-throughput deployment.

For a complete audit boundary, drain traffic, allow in-flight requests/tasks and
the old batch updater to finish, verify successful batch flushes, migrate the
primary database, and upgrade all writer nodes before resuming traffic. Old nodes
or third-party writers will not create the new credit records.

PostgreSQL 9.6 legacy history uses bounded application-side JSON parsing to avoid
failing on malformed historical text. Its total count scans candidate legacy
events; prefer the indexed credit view for ongoing investigations.
