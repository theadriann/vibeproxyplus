# AGENTS.md

## Purpose
VibeProxyPlus is a cross-platform local proxy stack for AI coding tools.

It runs:
1. `ThinkingProxy` (this repo, Go) on `127.0.0.1:8317`
2. `CLIProxyAPI` (external binary) on `127.0.0.1:8318`
3. `VibeProxyPlus Desktop` (Tauri v2 + React) in `desktop/` for tray/app UX

Traffic flow:
`Client -> ThinkingProxy (:8317) -> CLIProxyAPI (:8318) -> provider backends`

Primary goals:
- Local unified endpoint for multiple providers/accounts
- Claude thinking-budget model suffix support (`-thinking-<budget>`)
- Generated model configs for Factory CLI and OpenCode

## Core Components
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/cmd/thinking-proxy/main.go`
  - HTTP server entrypoint for ThinkingProxy.
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/internal/proxy/handler.go`
  - Reverse proxy logic and request transformation hook.
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/internal/proxy/thinking.go`
  - Model/body transforms (Claude thinking, Codex responses input normalization, Codex fast/priority aliases).
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/cmd/model-sync/main.go`
  - Pulls upstream model metadata and generates local config artifacts.
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/config/cliproxy.yaml`
  - Runtime config consumed by CLIProxyAPI.
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/desktop/src-tauri/src/main.rs`
  - Desktop backend invoke commands, tray actions, process supervisor, deep-link callback listener (`vibeproxyplus://`), auth polling/fallback, auth account actions (remove/disable/enable), callback-url handling, metrics, safe config controls, model-sync preview/apply, persistent runtime/app logging, management-key bootstrap (`MANAGEMENT_PASSWORD` env for sidecar), and local auth-file discovery from `~/.cli-proxy-api` (fallback when management auth-files is unavailable or empty, with provider normalization from either `provider` or `type` fields).
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/desktop/src/App.tsx`
  - Desktop frontend runtime/auth/metrics/settings UI, safe proxy config editor, model-sync controls.
- `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/docs/desktop-architecture-plan.md`
  - Live desktop architecture and phased implementation plan.

## External Dependencies
- Go toolchain (build + tests).
- `CLIProxyAPI` binary in `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/bin/cli-proxy-api`.
- Network access for model sync sources:
  - `https://raw.githubusercontent.com/router-for-me/CLIProxyAPI/main/internal/registry/model_definitions.go`
  - `https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json`
  - `https://raw.githubusercontent.com/router-for-me/CLIProxyAPI/main/internal/registry/models/codex_client_models.json`
  - `https://models.dev/api.json`

## Build, Run, and Auth Commands
Run from repo root: `/Users/brojbean/code/ai-tools/oss/vibeproxyplus`

- `make download-cliproxy`:
  Download the latest CLIProxyAPI binary into `bin/`.
- `make build`:
  Build ThinkingProxy.
- `make run`:
  Start CLIProxyAPI (`:8318`) and ThinkingProxy (`:8317`).
- `make update-cliproxy`:
  Update CLIProxyAPI binary if newer release exists.
- `make update-and-run`:
  Update and start both proxies.
- `make test`:
  Run Go tests.
- `make clean`:
  Remove built binaries.
- `make desktop-dev`:
  Run the desktop app in dev mode (Tauri + React).
- `make desktop-build-macos`:
  Build macOS desktop app bundle.
- `make desktop-test`:
  Run desktop frontend/backend tests.

Auth helper commands (pass through to CLIProxyAPI):
- `make auth-claude`
- `make auth-codex`
- `make auth-gemini`
- `make auth-antigravity`
- `make auth-copilot` currently reports that CLIProxyAPI no longer exposes the old Copilot login flag.

## Model Sync Commands and Outputs
- `make sync-models`
  - Builds `bin/model-sync`
  - Regenerates:
    - `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/config/models.json`
    - `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/config/factory-config.json`
    - `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/config/opencode-config.json`

Current behavior:
- Model sync is deterministic run-to-run against the same upstream payloads.
- Model sync supplements upstream catalogs with Anthropic's generally available Claude Fable 5 when CLIProxyAPI/shared catalogs have not caught up yet.
- Codex/OpenAI fast, verbosity, and reasoning-summary config variants are derived from CLIProxyAPI's Codex client model catalog.
- Current synced CLIProxyAPI catalog exposes 5 Codex/OpenAI models; upstream removed legacy GPT 5.2 and GPT 5.3 Codex base entries from generated Factory/OpenCode configs.
- Output can still change over time when upstream model metadata changes.

## Validation Checklist for Changes
For runtime proxy changes:
1. Run `go test ./...`.
2. If behavior changed, add/update tests in:
   - `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/internal/proxy/thinking_test.go`

For desktop app changes:
1. Run `make desktop-test`.
2. If backend behavior changed, run:
   - `cd /Users/brojbean/code/ai-tools/oss/vibeproxyplus/desktop/src-tauri && cargo test`
3. Smoke-check tray start/stop and `/health` updates through desktop UI.

For model-sync changes:
1. Run `go test ./...`.
2. Run `make sync-models` twice.
3. Confirm no second-run diff when upstream inputs are unchanged.

For command/config changes:
1. Verify `make run` path still works.
2. Verify affected files in `config/` are updated consistently.

## Context Discovery Workflow for Agents
When starting work:
1. Read this file first.
2. Read `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/README.md` for user-facing intent.
3. Read `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/Makefile` for operational commands.
4. Inspect relevant component files (`cmd/`, `internal/`, `config/`).
5. For desktop work, inspect `desktop/src-tauri` and `desktop/src` first.
6. Run focused tests before and after changes.

When debugging request behavior:
- Inspect logs in `/Users/brojbean/code/ai-tools/oss/vibeproxyplus/config/logs/`.
- Follow request path through `handler.go` then `thinking.go`.
- For desktop runtime/auth/management issues, inspect `/Users/brojbean/.vibeproxyplus/logs/desktop.log`.

## Maintenance Policy (Keep This a Live Spec)
When you add, change, or improve project behavior, update this `AGENTS.md` in the same PR/commit if any of these changed:
- Architecture or request flow
- Commands or build/run/auth workflow
- Generated config artifacts or model-sync behavior
- Test/validation expectations
- Key dependencies or source-of-truth files
- Desktop app interfaces/events/schema or local metrics behavior

Rules:
- Keep it concise and factual.
- Do not add speculative roadmap text.
- Do not duplicate full README content.
- Prefer short, high-signal updates over growth.

## Current Known Invariants
- ThinkingProxy listens on `127.0.0.1:8317` and forwards to `127.0.0.1:8318`.
- Claude thinking transforms are done in proxy-layer request rewrite; Claude Fable 5 and Claude Opus 4.7/4.8 use adaptive level-based thinking aliases.
- Codex responses input normalization (string -> list payload form) is handled in proxy-layer request rewrite for `/v1/responses` paths.
- Codex aliases such as `gpt-5.5(fast)`, `gpt-5.5(high-fast)`, and `gpt-5.5(high-fast-verbose-summary)` are translated into OpenAI request fields in proxy-layer request rewrite; known Codex models whose client catalog disables reasoning summaries omit unsolicited `reasoning.summary: "auto"` because OpenAI rejects `reasoning.summary: "none"`.
- Droid summarizer/compaction requests with an explicit `reasoning.effort` preserve that body effort instead of re-applying Codex model reasoning suffixes such as `(high)` and set `truncation: "auto"`, which avoids unnecessarily shrinking usable input context on very large compactions.
- Opt-in traffic debugging is controlled by `VIBEPROXYPLUS_TRAFFIC_DEBUG=1`; logs are written to `~/.vibeproxyplus/logs/traffic-debug` unless `VIBEPROXYPLUS_TRAFFIC_DEBUG_DIR` is set.
- Droid summarizer/compaction responses that return HTTP 2xx with no usable Responses API text are converted to HTTP 502 so clients can treat them as failures instead of empty successful summaries.
- Desktop auth account lists derive providers from management auth-files `provider` or `type`; if the management list is empty/unavailable, local `~/.cli-proxy-api` files are used.
