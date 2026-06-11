#!/usr/bin/env bash
set -euo pipefail

PORT="${PORT:-8765}"
TMP_ROOT="${TMP_ROOT:-$(mktemp -d -t droid-provider-probe.XXXXXX)}"
SETTINGS="$TMP_ROOT/settings.json"
WORK_DIR="$TMP_ROOT/work"
CAPTURE_DIR="$TMP_ROOT/captures"
SERVER_LOG="$TMP_ROOT/server.log"

OPENAI_MODEL="${OPENAI_MODEL:-gpt-5.5}"
ANTHROPIC_MODEL="${ANTHROPIC_MODEL:-claude-opus-4-8}"
OPENAI_EFFORTS="${OPENAI_EFFORTS:-low medium high xhigh}"
ANTHROPIC_EFFORTS="${ANTHROPIC_EFFORTS:-low medium high xhigh max}"
PROMPT="${PROMPT:-Reply with exactly: ok. Do not use tools.}"

mkdir -p "$WORK_DIR" "$CAPTURE_DIR"

cat > "$SETTINGS" <<JSON
{
  "customModels": [
    {
      "model": "${OPENAI_MODEL}",
      "displayName": "Probe OpenAI",
      "baseUrl": "http://127.0.0.1:${PORT}/v1",
      "apiKey": "dummy",
      "provider": "openai",
      "maxOutputTokens": 1024
    },
    {
      "model": "${ANTHROPIC_MODEL}",
      "displayName": "Probe Anthropic",
      "baseUrl": "http://127.0.0.1:${PORT}",
      "apiKey": "dummy",
      "provider": "anthropic",
      "maxOutputTokens": 1024
    }
  ]
}
JSON

cat > "$TMP_ROOT/capture_server.py" <<'PY'
#!/usr/bin/env python3
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CAPTURE_DIR = os.environ["CAPTURE_DIR"]
PORT = int(os.environ["PORT"])
OPENAI_MODEL = os.environ["OPENAI_MODEL"]
ANTHROPIC_MODEL = os.environ["ANTHROPIC_MODEL"]


def dump_json(path, value):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(value, f, indent=2, sort_keys=True)
        f.write("\n")


class Handler(BaseHTTPRequestHandler):
    server_version = "DroidProviderProbe/1.0"

    def log_message(self, fmt, *args):
        print("%s - %s" % (self.log_date_time_string(), fmt % args), flush=True)

    def _write_json(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _write_sse(self, events):
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("cache-control", "no-cache")
        self.end_headers()
        for event, payload in events:
            self.wfile.write(f"event: {event}\n".encode("utf-8"))
            self.wfile.write(("data: " + json.dumps(payload) + "\n\n").encode("utf-8"))
            self.wfile.flush()

    def do_GET(self):
        if self.path in ("/v1/models", "/models"):
            self._write_json(200, {
                "object": "list",
                "data": [
                    {"id": OPENAI_MODEL, "object": "model"},
                    {"id": ANTHROPIC_MODEL, "object": "model"},
                ],
            })
            return
        self._write_json(404, {"error": "not found", "path": self.path})

    def do_POST(self):
        length = int(self.headers.get("content-length", "0") or "0")
        raw = self.rfile.read(length)
        try:
            body = json.loads(raw.decode("utf-8")) if raw else {}
        except Exception:
            body = {"_raw": raw.decode("utf-8", errors="replace")}

        stamp = time.strftime("%Y%m%d-%H%M%S")
        safe_path = self.path.strip("/").replace("/", "_") or "root"
        idx = len([n for n in os.listdir(CAPTURE_DIR) if n.endswith(".json")]) + 1
        capture = {
            "method": "POST",
            "path": self.path,
            "headers": {k.lower(): v for k, v in self.headers.items()},
            "body": body,
        }
        dump_json(os.path.join(CAPTURE_DIR, f"{idx:02d}-{stamp}-{safe_path}.json"), capture)
        with open(os.path.join(CAPTURE_DIR, "requests.ndjson"), "a", encoding="utf-8") as f:
            f.write(json.dumps(capture, sort_keys=True) + "\n")

        if self.path == "/v1/responses":
            self.handle_openai(body)
            return
        if self.path == "/v1/messages":
            self.handle_anthropic(body)
            return
        self._write_json(404, {"error": "not found", "path": self.path})

    def handle_openai(self, body):
        model = body.get("model", OPENAI_MODEL)
        response = {
            "id": "resp_probe",
            "object": "response",
            "created_at": int(time.time()),
            "status": "completed",
            "model": model,
            "output": [{
                "id": "msg_probe",
                "type": "message",
                "status": "completed",
                "role": "assistant",
                "content": [{
                    "type": "output_text",
                    "text": "ok",
                    "annotations": [],
                }],
            }],
            "usage": {
                "input_tokens": 1,
                "output_tokens": 1,
                "total_tokens": 2,
            },
        }
        if body.get("stream") is True:
            events = [
                ("response.created", {**response, "status": "in_progress", "output": []}),
                ("response.output_item.added", {"type": "response.output_item.added", "output_index": 0, "item": response["output"][0]}),
                ("response.output_text.delta", {"type": "response.output_text.delta", "item_id": "msg_probe", "output_index": 0, "content_index": 0, "delta": "ok"}),
                ("response.output_text.done", {"type": "response.output_text.done", "item_id": "msg_probe", "output_index": 0, "content_index": 0, "text": "ok"}),
                ("response.completed", {"type": "response.completed", "response": response}),
            ]
            self._write_sse(events)
            return
        self._write_json(200, response)

    def handle_anthropic(self, body):
        model = body.get("model", ANTHROPIC_MODEL)
        message = {
            "id": "msg_probe",
            "type": "message",
            "role": "assistant",
            "model": model,
            "content": [{"type": "text", "text": "ok"}],
            "stop_reason": "end_turn",
            "stop_sequence": None,
            "usage": {"input_tokens": 1, "output_tokens": 1},
        }
        if body.get("stream") is True:
            events = [
                ("message_start", {"type": "message_start", "message": {**message, "content": [], "stop_reason": None, "usage": {"input_tokens": 1, "output_tokens": 0}}}),
                ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}}),
                ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "ok"}}),
                ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 1}}),
                ("message_stop", {"type": "message_stop"}),
            ]
            self._write_sse(events)
            return
        self._write_json(200, message)


if __name__ == "__main__":
    server = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"listening on http://127.0.0.1:{PORT}", flush=True)
    server.serve_forever()
PY

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

PORT="$PORT" CAPTURE_DIR="$CAPTURE_DIR" OPENAI_MODEL="$OPENAI_MODEL" ANTHROPIC_MODEL="$ANTHROPIC_MODEL" python3 "$TMP_ROOT/capture_server.py" > "$SERVER_LOG" 2>&1 &
SERVER_PID=$!
sleep 0.5

echo "Probe temp directory: $TMP_ROOT"
echo "Settings file: $SETTINGS"
echo "Capture directory: $CAPTURE_DIR"
echo "Server log: $SERVER_LOG"
echo
echo "Custom model IDs:"
echo "  OpenAI:    custom:Probe-OpenAI-0"
echo "  Anthropic: custom:Probe-Anthropic-1"
echo

run_probe() {
  local label="$1"
  local model="$2"
  local effort="$3"
  echo "==> $label reasoning effort: $effort"
  if droid --settings "$SETTINGS" exec \
    --cwd "$WORK_DIR" \
    --model "$model" \
    --reasoning-effort "$effort" \
    "$PROMPT"; then
    echo
  else
    echo "droid exec failed for $label/$effort; captured requests, if any, are still in $CAPTURE_DIR" >&2
    echo
  fi
}

for effort in $OPENAI_EFFORTS; do
  run_probe "OpenAI" "custom:Probe-OpenAI-0" "$effort"
done

for effort in $ANTHROPIC_EFFORTS; do
  run_probe "Anthropic" "custom:Probe-Anthropic-1" "$effort"
done

echo "Captured request files:"
find "$CAPTURE_DIR" -maxdepth 1 -type f -name '*.json' -print | sort
echo
echo "Inspect with:"
echo "  jq '.path, .body.model, .body.reasoning, .body.thinking, .body.output_config' '$CAPTURE_DIR'/*.json"
