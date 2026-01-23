.PHONY: build run run-all run-cliproxy run-thinking-proxy download-cliproxy update-cliproxy update-and-run auth-claude auth-codex auth-gemini auth-antigravity auth-copilot test clean sync-models

# Detect OS and architecture
ifeq ($(OS),Windows_NT)
    CLIPROXY_ARCH := windows_amd64
    CLIPROXY_EXT := zip
else
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
endif

build:
	go build -o bin/thinking-proxy ./cmd/thinking-proxy

test:
	go test ./... -v

download-cliproxy:

ifeq ($(OS),Windows_NT)
	@echo "Downloading CLIProxyAPIPlus..."
	@powershell -NoProfile -ExecutionPolicy Bypass -Command "$$ErrorActionPreference = 'Stop'; New-Item -ItemType Directory -Force -Path bin | Out-Null; $$json = Invoke-RestMethod -Uri 'https://api.github.com/repos/router-for-me/CLIProxyAPIPlus/releases/latest'; $$version = $$json.tag_name; $$versionNum = $$version.TrimStart('v'); $$asset = \"CLIProxyAPIPlus_$${versionNum}_$(CLIPROXY_ARCH).$(CLIPROXY_EXT)\"; $$url = \"https://github.com/router-for-me/CLIProxyAPIPlus/releases/download/$${version}/$${asset}\"; Write-Host \"Downloading $${url}\"; $$archive = \"bin/cliproxy-archive.$(CLIPROXY_EXT)\"; Invoke-WebRequest -Uri $${url} -OutFile $${archive}; Expand-Archive -Path $${archive} -DestinationPath bin -Force; Remove-Item -Force $${archive}; Remove-Item -Force -ErrorAction SilentlyContinue bin/README*.md, bin/LICENSE, bin/config.example.yaml; Write-Host 'Done. Binary at bin/cli-proxy-api-plus.exe'"
else
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
endif

update-cliproxy:

ifeq ($(OS),Windows_NT)
	@powershell -NoProfile -ExecutionPolicy Bypass -Command "$$ErrorActionPreference = 'Stop'; New-Item -ItemType Directory -Force -Path bin | Out-Null; $$json = Invoke-RestMethod -Uri 'https://api.github.com/repos/router-for-me/CLIProxyAPIPlus/releases/latest'; $$version = $$json.tag_name; $$versionNum = $$version.TrimStart('v'); $$exe = 'bin/cli-proxy-api-plus.exe'; if (Test-Path $$exe) { $$firstLine = & $$exe 2>&1 | Select-Object -First 1; $$match = [regex]::Match($$firstLine, '\\d+\\.\\d+\\.\\d+-\\d+'); if ($$match.Success) { $$current = $$match.Value } else { $$current = '0.0.0' }; Write-Host \"Current: $${current}, Latest: $${versionNum}\"; if ($$current -eq $$versionNum) { Write-Host 'Already up to date.'; exit 0 } } else { Write-Host 'CLIProxyAPIPlus not installed.' }; Write-Host \"Downloading $${versionNum}...\"; $$asset = \"CLIProxyAPIPlus_$${versionNum}_$(CLIPROXY_ARCH).$(CLIPROXY_EXT)\"; $$url = \"https://github.com/router-for-me/CLIProxyAPIPlus/releases/download/$${version}/$${asset}\"; $$archive = \"bin/cliproxy-archive.$(CLIPROXY_EXT)\"; Invoke-WebRequest -Uri $${url} -OutFile $${archive}; Expand-Archive -Path $${archive} -DestinationPath bin -Force; Remove-Item -Force $${archive}; Remove-Item -Force -ErrorAction SilentlyContinue bin/README*.md, bin/LICENSE, bin/config.example.yaml; Write-Host \"Updated to $${versionNum}\""
else
	@mkdir -p bin
	@LATEST=$$(curl -sI https://github.com/router-for-me/CLIProxyAPIPlus/releases/latest | grep -i '^location:' | sed 's/.*tag\///' | tr -d '\r\n'); \
	LATEST_NUM=$$(echo $$LATEST | sed 's/^v//'); \
	if [ -f bin/cli-proxy-api-plus ]; then \
		CURRENT=$$(./bin/cli-proxy-api-plus 2>&1 | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+-[0-9]+' | head -1 || echo "0.0.0"); \
		echo "Current: $$CURRENT, Latest: $$LATEST_NUM"; \
		if [ "$$CURRENT" = "$$LATEST_NUM" ]; then \
			echo "Already up to date."; \
			exit 0; \
		fi; \
	else \
		echo "CLIProxyAPIPlus not installed."; \
	fi; \
	echo "Downloading $$LATEST_NUM..."; \
	ASSET="CLIProxyAPIPlus_$${LATEST_NUM}_$(CLIPROXY_ARCH).$(CLIPROXY_EXT)"; \
	URL="https://github.com/router-for-me/CLIProxyAPIPlus/releases/download/$$LATEST/$$ASSET"; \
	curl -L -o bin/cliproxy-archive.$(CLIPROXY_EXT) "$$URL" && \
	cd bin && \
	if [ "$(CLIPROXY_EXT)" = "tar.gz" ]; then \
		tar -xzf cliproxy-archive.$(CLIPROXY_EXT); \
	else \
		unzip -o cliproxy-archive.$(CLIPROXY_EXT); \
	fi && \
	rm -f cliproxy-archive.$(CLIPROXY_EXT) README*.md LICENSE config.example.yaml && \
	chmod +x cli-proxy-api-plus 2>/dev/null || true; \
	echo "Updated to $$LATEST_NUM"
endif

update-and-run: update-cliproxy build
	@echo "Starting CLIProxyAPIPlus on :8318 and ThinkingProxy on :8317"
	@echo "Press Ctrl+C to stop both"
	@./bin/cli-proxy-api-plus -config config/cliproxy.yaml & \
	CLIPROXY_PID=$$!; \
	sleep 1; \
	./bin/thinking-proxy; \
	kill $$CLIPROXY_PID 2>/dev/null || true

run-cliproxy:
	./bin/cli-proxy-api-plus -config config/cliproxy.yaml

run-thinking-proxy: build
	./bin/thinking-proxy

run: build

ifeq ($(OS),Windows_NT)
	@cmd /c "scripts\\start.bat"
else
	@echo "Starting CLIProxyAPIPlus on :8318 and ThinkingProxy on :8317"
	@echo "Press Ctrl+C to stop both"
	@./bin/cli-proxy-api-plus -config config/cliproxy.yaml & \
	CLIPROXY_PID=$$!; \
	sleep 1; \
	./bin/thinking-proxy; \
	kill $$CLIPROXY_PID 2>/dev/null || true
endif

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
