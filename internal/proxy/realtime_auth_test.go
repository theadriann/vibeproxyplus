package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRealtimeAPIKeyFromYAML_OpenAICompatFirst(t *testing.T) {
	data := []byte(`
openai-compatibility:
  - name: openrouter
    base-url: https://openrouter.ai/api/v1
    api-key-entries:
      - api-key: sk-openrouter-1
  - name: openai
    base-url: https://api.openai.com/v1
    api-key-entries:
      - api-key: sk-openai-1
      - api-key: sk-openai-2
codex-api-key:
  - api-key: sk-codex-1
`)

	got, err := resolveRealtimeAPIKeyFromYAML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sk-openai-1" {
		t.Fatalf("key = %q, want %q", got, "sk-openai-1")
	}
}

func TestResolveRealtimeAPIKeyFromYAML_FallbackCodex(t *testing.T) {
	data := []byte(`
openai-compatibility:
  - name: openrouter
    base-url: https://openrouter.ai/api/v1
    api-key-entries:
      - api-key: sk-openrouter-1
codex-api-key:
  - api-key: sk-codex-1
  - api-key: sk-codex-2
`)

	got, err := resolveRealtimeAPIKeyFromYAML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sk-codex-1" {
		t.Fatalf("key = %q, want %q", got, "sk-codex-1")
	}
}

func TestResolveRealtimeAPIKeyFromYAML_FallbackAuthDir(t *testing.T) {
	tmpDir := t.TempDir()
	authFile := filepath.Join(tmpDir, "codex-user.json")
	content := `{"type":"codex","access_token":"oauth-codex-token","disabled":false,"expired":false}`
	if err := os.WriteFile(authFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	data := []byte(`
auth-dir: ` + tmpDir + `
openai-compatibility:
  - name: openrouter
    base-url: https://openrouter.ai/api/v1
    api-key-entries: []
codex-api-key: []
`)

	got, err := resolveRealtimeAPIKeyFromYAML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "oauth-codex-token" {
		t.Fatalf("key = %q, want %q", got, "oauth-codex-token")
	}
}

func TestResolveRealtimeAPIKeyFromYAML_NoKey(t *testing.T) {
	tmpDir := t.TempDir()
	data := []byte(`
auth-dir: ` + tmpDir + `
openai-compatibility:
  - name: openrouter
    base-url: https://openrouter.ai/api/v1
    api-key-entries: []
codex-api-key:
  - api-key: ""
`)

	_, err := resolveRealtimeAPIKeyFromYAML(data)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveRealtimeAPIKeyFromAuthDir_PicksFirstActiveCodex(t *testing.T) {
	tmpDir := t.TempDir()
	files := map[string]string{
		"a-disabled.json": `{"type":"codex","access_token":"x","disabled":true,"expired":false}`,
		"b-other.json":    `{"type":"claude","access_token":"x","disabled":false,"expired":false}`,
		"c-active.json":   `{"type":"codex","access_token":"token-1","disabled":false,"expired":"2099-01-01T00:00:00Z"}`,
		"d-active.json":   `{"type":"codex","access_token":"token-2","disabled":false,"expired":false}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got, ok := resolveRealtimeAPIKeyFromAuthDir(tmpDir)
	if !ok {
		t.Fatal("expected to resolve key, got none")
	}
	if got != "token-1" {
		t.Fatalf("key = %q, want %q", got, "token-1")
	}
}

func TestIsExpiredToken(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{name: "nil", in: nil, want: false},
		{name: "bool true", in: true, want: true},
		{name: "bool false", in: false, want: false},
		{name: "future timestamp", in: "2099-01-01T00:00:00Z", want: false},
		{name: "past timestamp", in: "2000-01-01T00:00:00Z", want: true},
	}

	for i := range tests {
		t.Run(tests[i].name, func(t *testing.T) {
			if got := isExpiredToken(tests[i].in); got != tests[i].want {
				t.Fatalf("isExpiredToken(%v) = %v, want %v", tests[i].in, got, tests[i].want)
			}
		})
	}
}

func TestIsOpenAIBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "openai", url: "https://api.openai.com/v1", want: true},
		{name: "openrouter", url: "https://openrouter.ai/api/v1", want: false},
		{name: "empty", url: "", want: false},
		{name: "invalid", url: "://bad", want: false},
	}

	for i := range tests {
		t.Run(tests[i].name, func(t *testing.T) {
			got := isOpenAIBaseURL(tests[i].url)
			if got != tests[i].want {
				t.Fatalf("isOpenAIBaseURL(%q) = %v, want %v", tests[i].url, got, tests[i].want)
			}
		})
	}
}
