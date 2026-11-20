"""Billing e2e runner: every dialect x client protocol x stream x scenario.

usage:
  python -I run.py <gateway.exe> <label> [--mode ratio|expr] [--only text]

Starts the gateway on a throwaway SQLite database seeded with one user, one
token and one channel per dialect (all pointing at fake_upstream.py, which must
already listen on :3510), sends every case, and writes runs/<label>-<mode>.jsonl
with what the user paid, the consume/error logs, and the expected charge.

Verdicts:
  PASS  the user paid what the upstream bills (within 3 quota units).
  FAIL  anything else, or a charged request that also wrote an error log, or a
        request that got HTTP 200 without the upstream being called.
  INFO  nothing tells what the upstream billed (an OpenAI-shaped stream that
        broke before its usage frame); compare against a baseline run instead.
Exit code 1 when any case fails.
"""
import argparse
import json
import os
import socket
import sqlite3
import subprocess
import sys
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import cases  # noqa: E402

GW_PORT = int(os.environ.get("BILLING_E2E_GW_PORT", "3530"))
UP = os.environ.get("BILLING_E2E_UPSTREAM", "http://127.0.0.1:3510")
TOKEN = "billinge2ekey000000000000000000000000000000000"
USER_ID = 900

# Client protocols the gateway accepts.
CLIENTS = ["chat", "responses", "claude", "gemini"]
SCENARIOS = ["normal", "error_mid", "cut_mid", "garbage_mid", "error_start", "http_500"]
# Upstream snapshot cases, only for dialects that can carry a billing_usage snapshot.
SNAPSHOT_SCENARIOS = {"oc": ["snapshot_bad_read", "snapshot_bad_write"], "gemini": ["snapshot_bad_read"]}


def expected(dialect, stream, scen, mode, calls):
    """What the upstream bills for this case, "input+partial", or None.

    The upstream protocol the gateway actually called decides what was
    reported before a failure: Claude Messages streams report input and cache
    in message_start and Gemini streams report usage in every frame, so a
    broken stream still bills that; OpenAI-shaped streams report usage only in
    their last frame.
    """
    last = calls[-1] if calls else None
    if last is None or last["status"] != 200 or scen in ("error_start", "http_500"):
        return 0
    if scen == "normal" or scen.startswith("snapshot_"):
        return cases.charge(dialect, mode)
    if not stream:
        # A failed non-stream request returns an HTTP error with no usage.
        return 0
    if last["path"].endswith("/messages") or "enerateContent" in last["path"]:
        return "input+partial"
    return None


def seed(db_path, mode):
    now = int(time.time())
    db = sqlite3.connect(db_path)
    for table in ("users", "tokens", "channels", "abilities", "logs"):
        db.execute(f"delete from {table}")
    db.execute("insert into users (id, username, password, display_name, role, status, quota, used_quota, request_count, `group`, aff_code, auth_version)"
               " values (?, 'e2e', 'x', 'e2e', 1, 1, 1000000000, 0, 0, 'default', 'e2eaff', 1)", (USER_ID,))
    db.execute("insert into tokens (id, user_id, `key`, status, name, created_time, accessed_time, expired_time, remain_quota, unlimited_quota, model_limits_enabled, used_quota, `group`)"
               " values (901, ?, ?, 1, 'e2e', ?, ?, -1, 0, 1, 0, 0, '')", (USER_ID, TOKEN, now, now))
    models = []
    for i, (name, spec) in enumerate(cases.DIALECTS.items(), start=1):
        other = "2025-04-01-preview" if name == "azure" else ""
        db.execute("insert into channels (id, type, `key`, status, name, weight, created_time, base_url, models, `group`, used_quota, priority, other, auto_ban)"
                   " values (?, ?, 'sk-fake', 1, ?, 0, ?, ?, ?, 'default', 0, 0, ?, 0)", (i, spec["type"], "e2e-" + name, now, UP, spec["model"], other))
        db.execute("insert into abilities (`group`, model, channel_id, enabled, priority, weight) values ('default', ?, ?, 1, 0, 0)", (spec["model"], i))
        models.append(spec["model"])

    def per_model(value):
        return json.dumps({m: value for m in models})

    options = {"ModelRatio": per_model(cases.MODEL_RATIO), "CompletionRatio": per_model(cases.COMPLETION_RATIO),
               "CacheRatio": per_model(cases.CACHE_RATIO), "CreateCacheRatio": per_model(cases.CREATE_CACHE_RATIO),
               "RetryTimes": "0", "performance_setting.monitor_enabled": "false"}
    if mode == "expr":
        options["billing_setting.billing_mode"] = json.dumps({m: "tiered_expr" for m in models})
        options["billing_setting.billing_expr"] = json.dumps({m: cases.EXPR for m in models})
    for key, value in options.items():
        db.execute("insert or replace into options (`key`, value) values (?, ?)", (key, value))
    db.commit()


def wait_port(up):
    for _ in range(240):
        try:
            socket.create_connection(("127.0.0.1", GW_PORT), timeout=1).close()
            if up:
                return
        except OSError:
            if not up:
                return
        time.sleep(0.5)
    raise SystemExit("gateway port did not " + ("open" if up else "close"))


def start(binary, run_dir, db_path):
    dsn = db_path + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"
    # CountToken off like production, so a missing upstream usage shows up as 0.
    env = dict(os.environ, SQL_DSN="", SQLITE_PATH=dsn, REDIS_CONN_STRING="", LOG_SQL_DSN="", ERROR_LOG_ENABLED="true",
               CountToken="false", SESSION_SECRET="billing-e2e-session-0123456789", CRYPTO_SECRET="billing-e2e-crypto-0123456789",
               BATCH_UPDATE_ENABLED="false")
    out = open(os.path.join(run_dir, "gateway.out"), "a")
    proc = subprocess.Popen([binary, "--port", str(GW_PORT), "--log-dir", os.path.join(run_dir, "logs")], cwd=run_dir, env=env,
                            stdout=out, stderr=subprocess.STDOUT)
    wait_port(True)
    time.sleep(1.5)
    return proc


def stop(proc):
    proc.kill()
    proc.wait()
    wait_port(False)


def build_request(client, model, stream, tag):
    text = f"Reply OK. {tag}"
    if client == "chat":
        body = {"model": model, "stream": stream, "max_tokens": 32, "messages": [{"role": "user", "content": text}]}
        if stream:
            body["stream_options"] = {"include_usage": True}
        return "/v1/chat/completions", body
    if client == "responses":
        return "/v1/responses", {"model": model, "stream": stream, "max_output_tokens": 32, "input": text}
    if client == "claude":
        return "/v1/messages", {"model": model, "stream": stream, "max_tokens": 32, "messages": [{"role": "user", "content": text}]}
    verb = "streamGenerateContent?alt=sse" if stream else "generateContent"
    return f"/v1beta/models/{model}:{verb}", {"contents": [{"role": "user", "parts": [{"text": text}]}]}


def send(path, body):
    headers = {"Content-Type": "application/json", "Authorization": "Bearer sk-" + TOKEN, "x-api-key": "sk-" + TOKEN,
               "x-goog-api-key": "sk-" + TOKEN, "anthropic-version": "2023-06-01"}
    req = urllib.request.Request(f"http://127.0.0.1:{GW_PORT}{path}", data=json.dumps(body).encode(), method="POST", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as err:
        return err.code, err.read().decode("utf-8", "replace")
    except Exception as err:
        return 0, str(err)


def upstream_calls(rid):
    try:
        with urllib.request.urlopen(f"{UP}/__seen?rid={rid}", timeout=5) as resp:
            return json.loads(resp.read())["paths"]
    except Exception:
        return []


def read_logs(db, after_id):
    logs = []
    for typ, prompt, compl, quota, other in db.execute(
            "select type, prompt_tokens, completion_tokens, quota, other from logs where id > ? order by id", (after_id,)):
        o = json.loads(other or "{}")
        admin = o.get("admin_info") or {}
        logs.append({"type": {2: "consume", 5: "error"}.get(typ, typ), "prompt": prompt, "compl": compl, "quota": quota,
                     "cache_read": o.get("cache_tokens"), "cache_write": o.get("cache_creation_tokens"),
                     "cache_5m": o.get("cache_creation_tokens_5m"), "cache_1h": o.get("cache_creation_tokens_1h"),
                     "public_failure": (o.get("stream_status") or {}).get("status") == "error" or bool(o.get("termination")),
                     "admin_failure": "upstream_billed_failure" in admin, "billing_path": admin.get("usage_billing_path")})
    return logs


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary")
    parser.add_argument("label")
    parser.add_argument("--mode", default="ratio", choices=["ratio", "expr"])
    parser.add_argument("--only", default="", help="run only cases whose id contains this text")
    args = parser.parse_args()

    try:
        urllib.request.urlopen(f"{UP}/__seen?rid=0", timeout=5).read()
    except Exception as err:
        raise SystemExit(f"fake upstream is not answering on {UP}: {err}")

    run_dir = os.path.join(HERE, "runs", f"{args.label}-{args.mode}")
    os.makedirs(run_dir, exist_ok=True)
    for name in os.listdir(run_dir):
        if os.path.isfile(os.path.join(run_dir, name)):
            os.remove(os.path.join(run_dir, name))
    db_path = os.path.join(run_dir, "one-api.db")
    stop(start(args.binary, run_dir, db_path))  # migrate the fresh database
    seed(db_path, args.mode)

    plan = []
    for dialect in cases.DIALECTS:
        for client in CLIENTS:
            for stream in (True, False):
                for scen in SCENARIOS + SNAPSHOT_SCENARIOS.get(dialect, []):
                    case_id = f"{dialect}/{client}/{'stream' if stream else 'plain'}/{scen}"
                    if args.only in case_id:
                        plan.append((case_id, dialect, client, stream, scen))

    out_path = os.path.join(HERE, "runs", f"{args.label}-{args.mode}.jsonl")
    counts = {"PASS": 0, "FAIL": 0, "INFO": 0}
    stamp = str(int(time.time()))
    proc = start(args.binary, run_dir, db_path)
    try:
        db = sqlite3.connect(db_path, timeout=10)
        with open(out_path, "w", encoding="utf-8") as out:
            for n, (case_id, dialect, client, stream, scen) in enumerate(plan, start=1):
                rid = f"{stamp}-{n}"
                path, body = build_request(client, cases.DIALECTS[dialect]["model"], stream, f"SCEN:{scen} RID:{rid}")
                before_quota = db.execute("select quota from users where id=?", (USER_ID,)).fetchone()[0]
                before_log = db.execute("select coalesce(max(id),0) from logs").fetchone()[0]
                status, raw = send(path, body)
                # Wait until a log exists and the balance stops moving.
                paid = None
                for _ in range(30):
                    time.sleep(0.3)
                    now = before_quota - db.execute("select quota from users where id=?", (USER_ID,)).fetchone()[0]
                    if now == paid and db.execute("select count(*) from logs where id > ?", (before_log,)).fetchone()[0] > 0:
                        break
                    paid = now
                logs = read_logs(db, before_log)
                calls = upstream_calls(rid)
                want = expected(dialect, stream, scen, args.mode, calls)
                if want == "input+partial":
                    consume = [entry for entry in logs if entry["type"] == "consume"]
                    want = cases.charge(dialect, args.mode, output_tokens=consume[0]["compl"] if consume else 0)
                # Rejected before the upstream (an unsupported protocol pair):
                # nothing may be charged. HTTP 200 without any upstream call
                # means the harness is broken.
                unsupported = not calls and status != 200
                if want is None:
                    verdict = "INFO"
                else:
                    ok = paid is not None and abs(paid - round(want)) <= 3 and bool(calls or unsupported)
                    # A charged failure must read like a success to its user.
                    if paid and any(entry["type"] == "error" for entry in logs):
                        ok = False
                    verdict = "PASS" if ok else "FAIL"
                counts[verdict] += 1
                row = {"id": case_id, "verdict": verdict, "http": status, "unsupported": unsupported, "paid": paid,
                       "expected": None if want is None else round(want), "upstream_calls": calls, "logs": logs,
                       "client_body": raw[:300] if verdict != "PASS" else ""}
                out.write(json.dumps(row, ensure_ascii=False) + "\n")
                out.flush()
                print(f"{verdict} {case_id} paid={paid} expected={row['expected']} http={status}", flush=True)
    finally:
        stop(proc)
    print(f"{counts['PASS']} pass, {counts['FAIL']} fail, {counts['INFO']} info of {len(plan)} -> {out_path}")
    sys.exit(1 if counts["FAIL"] else 0)


if __name__ == "__main__":
    main()
