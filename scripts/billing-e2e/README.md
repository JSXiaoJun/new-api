# Billing e2e (simulated upstream)

Local end-to-end check of what a user is charged for every LLM protocol the
gateway relays. It starts a real gateway binary on a throwaway SQLite database,
points one channel per provider dialect at a fake upstream, sends every client
protocol × stream × failure scenario through it, and compares the charge with
what the upstream bills. Nothing here touches production.

Run it before pushing any change to usage parsing, stream handling, settlement,
or billing logs. It caught real bugs (Claude 1h cache writes billed at the 5m
price through `/v1/responses`, failures that refunded what the upstream billed).

## Files

| File | Purpose |
|---|---|
| `fake_upstream.py` | Fake provider on `:3510`: Claude Messages, OpenAI Chat, OpenAI Responses, Gemini, Azure deployments. Unknown paths answer 404 like a real provider. `GET /__seen?rid=<id>` lists the upstream calls of one request. |
| `cases.py` | Dialects (channel type, model, usage field layout), canonical usage numbers, prices, and the expected charge. Edit here to add a provider. |
| `run.py` | Runs the matrix against one gateway binary, writes `runs/<label>-<mode>.jsonl`, exits 1 on any FAIL. |
| `compare.py` | Diffs two result files case by case (baseline vs change). |
| `runs/` | Output, git-ignored. |

Dialects: `claude` (Anthropic, type 14), `openai` (1), `azure` (3), `deepseek`
(43, `prompt_cache_hit_tokens`), `kimi` (25, top-level `cached_tokens`), `glm`
(26), `gemini` (24), and `oc`: an upstream new-api relaying Claude in OpenAI
format with a `billing_usage` snapshot (the shape behind the 2026-11-19 cache
incident).

Client protocols: `/v1/chat/completions`, `/v1/responses`, `/v1/messages`,
`/v1beta/models/*:generateContent`, each streamed and not.

Scenarios: `normal`, `error_mid` (error event after output started), `cut_mid`
(connection drops), `garbage_mid` (malformed frame), `error_start` (error before
any usage), `http_500`, and for snapshot-carrying dialects `snapshot_bad_read` /
`snapshot_bad_write` (the upstream snapshot disagrees with the native usage).

Both pricing modes are covered: `--mode ratio` (model/completion/cache ratios)
and `--mode expr` (tiered billing expression with `cr`, `cc`, `cc1h`).

## Run

Needs Go and Python 3. From the repository root (bash):

```bash
# 1. Build the change, and a baseline from HEAD for comparison.
go build -o /tmp/billing-e2e/new.exe .
rm -rf /tmp/billing-e2e/base-src && mkdir -p /tmp/billing-e2e/base-src && git archive HEAD | tar -x -C /tmp/billing-e2e/base-src
mkdir -p /tmp/billing-e2e/base-src/web/dist && cp -r web/dist/. /tmp/billing-e2e/base-src/web/dist/
(cd /tmp/billing-e2e/base-src && go build -o /tmp/billing-e2e/base.exe .)

# 2. Start the fake upstream (keep it running).
python -I scripts/billing-e2e/fake_upstream.py 3510 &

# 3. Run both binaries in both pricing modes (about 4 minutes each).
for m in ratio expr; do
  python -I scripts/billing-e2e/run.py /tmp/billing-e2e/new.exe new --mode $m
  python -I scripts/billing-e2e/run.py /tmp/billing-e2e/base.exe base --mode $m
done

# 4. Every listed change must be intended.
python -I scripts/billing-e2e/compare.py scripts/billing-e2e/runs/base-ratio.jsonl scripts/billing-e2e/runs/new-ratio.jsonl
python -I scripts/billing-e2e/compare.py scripts/billing-e2e/runs/base-expr.jsonl scripts/billing-e2e/runs/new-expr.jsonl
```

On Windows pass the binary as a Windows path (`$(cygpath -w /tmp/billing-e2e/new.exe)`)
and note that `web/dist` must exist before `go build` (it is embedded).
`--only <text>` runs only cases whose id contains the text, e.g. `--only oc/responses`.
Ports: `BILLING_E2E_GW_PORT` (default 3530) and `BILLING_E2E_UPSTREAM`
(default `http://127.0.0.1:3510`).

## Reading the result

- `PASS`: the user paid what the upstream bills, within 3 quota units.
- `FAIL`: wrong charge; or a charged request that also wrote an error log (a
  charged failure must read like a normal consume log); or HTTP 200 without
  any upstream call (the harness is broken, e.g. the fake upstream is down).
- `INFO`: nothing tells what the upstream billed. An OpenAI-shaped stream that
  breaks before its final usage frame has no upstream usage; the gateway's local
  estimate is shown and only `compare.py` against the baseline judges it.

A request rejected before reaching the upstream (an unsupported protocol pair
such as Gemini format on a DeepSeek channel) must charge 0.

Each row in the `.jsonl` keeps the HTTP status, the upstream calls, the consume
or error logs (tokens, cache read/write, 5m/1h split, public failure markers,
`admin_info.usage_billing_path`) and, for non-passing cases, the start of the
client response.

## Adding coverage

- A new provider: add a dialect to `cases.DIALECTS` and its usage layout to
  `openai_chat_usage` / `openai_responses_usage` (or a new handler in
  `fake_upstream.py` for a new wire format). Use the field names from the
  provider's documentation or a captured real response.
- A new failure shape: add a scenario name to `run.SCENARIOS`, implement it in
  each `fake_upstream.py` handler, and state what upstream bills in
  `run.expected`.

For a check against a live deployment, the probes in this repository's history
(`/api/log/token` read with a test token) compare real upstream usage with what
the gateway logged; this harness covers everything that can be simulated.
