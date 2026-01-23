# VibeProxyPlus

A cross-platform alternative to [VibeProxy](https://github.com/automazeio/vibeproxy), built on [CLIProxyAPIPlus](https://github.com/router-for-me/CLIProxyAPIPlus).

## Why?

[VibeProxy](https://github.com/automazeio/vibeproxy) is a great macOS menu bar app for using AI subscriptions with coding tools. However:

- **macOS only** - No Windows/Linux support
- **No model configs** - Doesn't generate usable configs for tools like Factory CLI

This project provides:
- **Cross-platform** - Works on macOS, Windows, and Linux
- **Extended thinking** - Adds thinking budget support for Claude models
- **Model configs** - Generates ready-to-use configs for Factory CLI and OpenCode

> **Note**: Model configs are provided as-is and may not always be up-to-date. Run `make sync-models` to fetch the latest from upstream sources.

## Architecture

```
Client → ThinkingProxy (:8317) → CLIProxyAPIPlus (:8318) → Claude/OpenAI/Gemini/etc
```

## Quick Start

```bash
# 1. Download CLIProxyAPIPlus
make download-cliproxy

# 2. Authenticate (pick the providers you need)
make auth-claude       # Claude models
make auth-codex        # GPT-5/Codex models
make auth-gemini       # Gemini models
make auth-antigravity  # Antigravity (Gemini 3 Pro, etc)
make auth-copilot      # GitHub Copilot

# 3. Run
make run
```

## Factory CLI Setup

```bash
make sync-models
cp config/factory-config.json ~/.factory/settings.json
```

Or merge `customModels` array into your existing `~/.factory/settings.json`.

## Thinking Models

Append `-thinking-BUDGET` to Claude models to enable extended thinking:

| Suffix | Budget | Use Case |
|--------|--------|----------|
| `-thinking-4000` | 4K | Quick reasoning |
| `-thinking-10000` | 10K | Standard |
| `-thinking-32000` | 32K | Deep analysis |

Example: `claude-opus-4-5-20251101-thinking-32000`

## Commands

```bash
make build            # Build ThinkingProxy
make run              # Start both proxies
make sync-models      # Regenerate model configs
make test             # Run tests
make clean            # Remove binaries
```

## Windows

```bash
# Download cli-proxy-api-plus_windows_amd64.exe from releases
# Rename to bin/cli-proxy-api-plus.exe
go build -o bin\thinking-proxy.exe .\cmd\thinking-proxy
scripts\start.bat
```

## Health Check

```bash
curl http://localhost:8317/health
# {"status":"healthy"}
```

## Contributing

This is a personal open source project. Contributions, issues, and PRs are welcome!

## Credits

- [VibeProxy](https://github.com/automazeio/vibeproxy) - Inspiration for this project
- [CLIProxyAPIPlus](https://github.com/router-for-me/CLIProxyAPIPlus) - The underlying proxy engine
