"""Fake LLM upstream for the billing e2e (scripts/billing-e2e).

One process serves every provider shape the gateway relays to:

  Claude Messages      POST /v1/messages, /anthropic/v1/messages, /api/anthropic/v1/messages
  OpenAI Chat          POST /v1/chat/completions, /chat/completions, /api/paas/v4/chat/completions,
                            /openai/deployments/<d>/chat/completions (Azure)
  OpenAI Responses     POST /v1/responses, /responses, /openai/deployments/<d>/responses
  Gemini               POST /v1beta/models/<m>:generateContent / :streamGenerateContent

The model name picks the usage dialect (see DIALECTS in cases.py); a "SCEN:<name>" tag in
the prompt picks the behaviour: normal | error_mid | cut_mid | error_start | http_500 |
garbage_mid | snapshot_bad_read | snapshot_bad_write. Usage numbers come from cases.py so
the runner can compute the expected charge from the same source.
"""
import json
import os
import re
import socket
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import cases  # noqa: E402

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 3510
SEEN = {}


def scenario(body):
    match = re.search(r"SCEN:([a-z_0-9]+)", json.dumps(body))
    return match.group(1) if match else "normal"


def dialect_of(model):
    for name, spec in cases.DIALECTS.items():
        if model == spec["model"]:
            return name
    return "openai"


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    # ---- transport helpers ----
    def sse_start(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()

    def chunk(self, text):
        data = text.encode()
        self.wfile.write(b"%x\r\n%s\r\n" % (len(data), data))
        self.wfile.flush()

    def end_chunks(self):
        self.wfile.write(b"0\r\n\r\n")
        self.wfile.flush()

    def cut(self):
        # The connection drops without a protocol terminal.
        self.wfile.flush()
        self.close_connection = True
        try:
            self.connection.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass

    def send_json(self, obj, status=200):
        data = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        # /__seen?rid=<id> -> the upstream calls that request made.
        rid = self.path.split("rid=")[-1]
        self.send_json({"paths": SEEN.get(rid, [])})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length) or b"{}")
        scen = scenario(body)
        path = self.path.split("?")[0]
        is_gemini = re.match(r"^/v1(beta)?/models/[^/]+:(stream)?[gG]enerateContent$", path) is not None
        known = path.endswith(("/messages", "/chat/completions", "/responses")) or is_gemini
        rid = re.search(r"RID:([A-Za-z0-9-]+)", json.dumps(body))
        if rid:
            status = 404 if not known else (500 if scen == "http_500" else 200)
            SEEN.setdefault(rid.group(1), []).append({"path": path, "status": status})
        model = body.get("model") or ""
        if not model and "models/" in path:
            model = re.search(r"models/([^:/]+)", path).group(1)
        if not model and "/deployments/" in path:
            model = path.split("/deployments/")[1].split("/")[0]
        dialect = dialect_of(model)
        if not known:
            # As a real provider would.
            return self.send_json({"error": {"message": "fake upstream: unknown path " + path}}, 404)
        if scen == "http_500":
            return self.send_json({"error": {"message": "upstream exploded", "type": "server_error"}}, 500)
        if path.endswith("/messages"):
            return self.claude(body, scen, dialect)
        if path.endswith("/chat/completions"):
            return self.chat(body, scen, dialect)
        if path.endswith("/responses"):
            return self.openai_responses(body, scen, dialect)
        return self.gemini(scen, dialect, ":streamGenerateContent" in path)

    # ---- Claude Messages ----
    def claude(self, body, scen, dialect):
        u = cases.usage_for(dialect)
        usage = {"input_tokens": u["input"], "cache_read_input_tokens": u["cache_read"],
                 "cache_creation_input_tokens": u["cache_write_5m"] + u["cache_write_1h"],
                 "cache_creation": {"ephemeral_5m_input_tokens": u["cache_write_5m"], "ephemeral_1h_input_tokens": u["cache_write_1h"]},
                 "output_tokens": u["output"]}
        start_usage = dict(usage, output_tokens=1)
        if not body.get("stream"):
            if scen in ("error_mid", "error_start", "cut_mid", "garbage_mid"):
                return self.send_json({"type": "error", "error": {"type": "api_error", "message": "Upstream request failed"}}, 500)
            return self.send_json({"id": "msg_e2e", "type": "message", "role": "assistant", "model": body.get("model"),
                                   "content": [{"type": "text", "text": "OK"}], "stop_reason": "end_turn", "usage": usage})
        self.sse_start()
        if scen == "error_start":
            self.chunk('event: error\ndata: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}\n\n')
            return self.end_chunks()
        self.chunk("event: message_start\ndata: " + json.dumps({"type": "message_start", "message": {
            "id": "msg_e2e", "type": "message", "role": "assistant", "model": body.get("model"), "content": [],
            "stop_reason": None, "usage": start_usage}}) + "\n\n")
        self.chunk('event: content_block_start\ndata: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}\n\n')
        self.chunk('event: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}\n\n')
        if scen == "error_mid":
            self.chunk('event: error\ndata: {"type":"error","error":{"type":"api_error","message":"Upstream request failed"}}\n\n')
            return self.end_chunks()
        if scen == "garbage_mid":
            self.chunk("event: content_block_delta\ndata: {not json\n\n")
            return self.end_chunks()
        if scen == "cut_mid":
            return self.cut()
        self.chunk('event: content_block_stop\ndata: {"type":"content_block_stop","index":0}\n\n')
        self.chunk("event: message_delta\ndata: " + json.dumps({"type": "message_delta", "delta": {"stop_reason": "end_turn"}, "usage": usage}) + "\n\n")
        self.chunk('event: message_stop\ndata: {"type":"message_stop"}\n\n')
        self.end_chunks()

    # ---- OpenAI Chat (incl. vendor dialects) ----
    def chat(self, body, scen, dialect):
        usage = cases.openai_chat_usage(dialect, scen)
        if not body.get("stream"):
            if scen in ("error_mid", "error_start", "cut_mid", "garbage_mid"):
                return self.send_json({"error": {"message": "Upstream request failed", "type": "server_error"}}, 500)
            return self.send_json({"id": "c1", "object": "chat.completion", "model": body.get("model"),
                                   "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK"}, "finish_reason": "stop"}],
                                   "usage": usage})
        self.sse_start()
        if scen == "error_start":
            self.chunk('data: {"error":{"message":"Overloaded","type":"server_error"}}\n\n')
            return self.end_chunks()
        self.chunk('data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"}}]}\n\n')
        if scen == "error_mid":
            self.chunk('data: {"error":{"message":"Upstream request failed","type":"server_error"}}\n\n')
            return self.end_chunks()
        if scen == "garbage_mid":
            self.chunk("data: {not json\n\n")
            return self.end_chunks()
        if scen == "cut_mid":
            return self.cut()
        self.chunk('data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n')
        self.chunk("data: " + json.dumps({"id": "c1", "object": "chat.completion.chunk", "model": "m", "choices": [], "usage": usage}) + "\n\n")
        self.chunk("data: [DONE]\n\n")
        self.end_chunks()

    # ---- OpenAI Responses ----
    def openai_responses(self, body, scen, dialect):
        usage = cases.openai_responses_usage(dialect, scen)
        response = {"id": "resp_e2e", "object": "response", "status": "completed", "model": body.get("model"),
                    "output": [{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed",
                                "content": [{"type": "output_text", "text": "OK", "annotations": []}]}], "usage": usage}
        if not body.get("stream"):
            if scen in ("error_mid", "error_start", "cut_mid", "garbage_mid"):
                return self.send_json({"error": {"message": "Upstream request failed", "type": "server_error"}}, 500)
            return self.send_json(response)
        self.sse_start()
        created = dict(response, status="in_progress", output=[], usage=None)
        if scen == "error_start":
            self.chunk('event: error\ndata: {"type":"error","message":"Overloaded","code":"server_error"}\n\n')
            return self.end_chunks()
        self.chunk("event: response.created\ndata: " + json.dumps({"type": "response.created", "response": created}) + "\n\n")
        self.chunk('event: response.output_text.delta\ndata: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"OK"}\n\n')
        if scen == "error_mid":
            failed = dict(created, status="failed", error={"code": "server_error", "message": "Upstream request failed"})
            self.chunk("event: response.failed\ndata: " + json.dumps({"type": "response.failed", "response": failed}) + "\n\n")
            return self.end_chunks()
        if scen == "garbage_mid":
            self.chunk("event: response.output_text.delta\ndata: {not json\n\n")
            return self.end_chunks()
        if scen == "cut_mid":
            return self.cut()
        self.chunk("event: response.completed\ndata: " + json.dumps({"type": "response.completed", "response": response}) + "\n\n")
        self.end_chunks()

    # ---- Gemini ----
    def gemini(self, scen, dialect, stream):
        u = cases.usage_for(dialect)
        meta = {"promptTokenCount": u["prompt_total"], "candidatesTokenCount": u["output"] - u["reasoning"],
                "thoughtsTokenCount": u["reasoning"], "totalTokenCount": u["prompt_total"] + u["output"],
                "cachedContentTokenCount": u["cache_read"], "promptTokensDetails": [{"modality": "TEXT", "tokenCount": u["prompt_total"]}]}
        if scen == "snapshot_bad_read":
            # An upstream gateway's OpenAI snapshot that lost the cache reads.
            meta["billing_usage"] = {"source": "oai_chat", "semantic": "openai", "openai_usage": {
                "prompt_tokens": u["prompt_total"], "completion_tokens": u["output"], "total_tokens": u["prompt_total"] + u["output"],
                "prompt_tokens_details": {"cached_tokens": 0}}}
        final = {"candidates": [{"content": {"role": "model", "parts": [{"text": "OK"}]}, "finishReason": "STOP"}], "usageMetadata": meta}
        if not stream:
            if scen in ("error_mid", "error_start", "cut_mid", "garbage_mid"):
                return self.send_json({"error": {"code": 500, "message": "Upstream request failed", "status": "INTERNAL"}}, 500)
            return self.send_json(final)
        self.sse_start()
        if scen == "error_start":
            self.chunk('data: {"error":{"code":503,"message":"Overloaded","status":"UNAVAILABLE"}}\n\n')
            return self.end_chunks()
        first = {"candidates": [{"content": {"role": "model", "parts": [{"text": "O"}]}}],
                 "usageMetadata": {"promptTokenCount": u["prompt_total"], "totalTokenCount": u["prompt_total"],
                                   "cachedContentTokenCount": u["cache_read"]}}
        self.chunk("data: " + json.dumps(first) + "\n\n")
        if scen in ("error_mid", "garbage_mid"):
            self.chunk("data: {not json\n\n")
            return self.end_chunks()
        if scen == "cut_mid":
            return self.cut()
        self.chunk("data: " + json.dumps(final) + "\n\n")
        self.end_chunks()


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
