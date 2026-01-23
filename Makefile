.PHONY: build run run-all run-cliproxy run-thinking-proxy download-cliproxy auth-claude auth-codex auth-gemini auth-antigravity auth-copilot test clean sync-models

# Detect OS and architecture
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
    ifeq ($(UNAME_M),arm64)
        CLIPROXY_ARCH := darwin_arm64
        CLIPROXY_EXT := tar.gz
    else
        CLIPROXY_ARCH := darwin_amd64
        CLIPROXY_EXT := tar.gz
    endif
else ifeq ($(UNAME_S),Linux)
    ifeq ($(UNAME_M),aarch64)
        CLIPROXY_ARCH := linux_arm64
    else
        CLIPROXY_ARCH := linux_amd64
    endif
    CLIPROXY_EXT := tar.gz
else
    CLIPROXY_ARCH := windows_amd64
    CLIPROXY_EXT := zip
endif

build:
	go build -o bin/thinking-proxy ./cmd/thinking-proxy

test:
	go test ./... -v

download-cliproxy:
	@echo "Downloading CLIProxyAPIPlus..."
	@mkdir -p bin
	@VERSION=$$(curl -sI https://github.com/router-for-me/CLIProxyAPIPlus/releases/latest | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n'); \
	VERSION_NUM=$$(echo $$VERSION | sed 's/^v//'); \
	ASSET="CLIProxyAPIPlus_$${VERSION_NUM}_$(CLIPROXY_ARCH).$(CLIPROXY_EXT)"; \
	URL="https://github.com/router-for-me/CLIProxyAPIPlus/releases/download/$$VERSION/$$ASSET"; \
	echo "Downloading $$URL"; \
	curl -L -o bin/cliproxy-archive.$(CLIPROXY_EXT) "$$URL" && \
	cd bin && \
	if [ "$(CLIPROXY_EXT)" = "tar.gz" ]; then \
		tar -xzf cliproxy-archive.$(CLIPROXY_EXT); \
	else \
		unzip -o cliproxy-archive.$(CLIPROXY_EXT); \
	fi && \
	rm -f cliproxy-archive.$(CLIPROXY_EXT) README*.md LICENSE config.example.yaml && \
	chmod +x cli-proxy-api-plus 2>/dev/null || true
	@echo "Done. Binary at bin/cli-proxy-api-plus"

run-cliproxy:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml

run-thinking-proxy: build
	./bin/thinking-proxy

run: build
	@echo "Starting CLIProxyAPIPlus on :8318 and ThinkingProxy on :8317"
	@echo "Press Ctrl+C to stop both"
	@./bin/cli-proxy-api-plus -config config/cliproxy.yaml & \
	CLIPROXY_PID=$$!; \
	sleep 1; \
	./bin/thinking-proxy; \
	kill $$CLIPROXY_PID 2>/dev/null || true

auth-claude:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml -claude-login

auth-codex:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml -codex-login

auth-gemini:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml -login

auth-antigravity:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml -antigravity-login

auth-copilot:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml -github-copilot-login

clean:
	rm -rf bin/thinking-proxy bin/model-sync

sync-models:
	go build -o bin/model-sync ./cmd/model-sync
	./bin/model-sync -output config/models.json -factory config/factory-config.json -opencode config/opencode-config.json
