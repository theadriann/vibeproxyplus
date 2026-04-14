package proxy

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const realtimeConfigPathEnv = "VIBEPROXYPLUS_CLIPROXY_CONFIG"

type realtimeConfig struct {
	AuthDir             string `yaml:"auth-dir"`
	OpenAICompatibility []struct {
		Name          string `yaml:"name"`
		BaseURL       string `yaml:"base-url"`
		APIKeyEntries []struct {
			APIKey string `yaml:"api-key"`
		} `yaml:"api-key-entries"`
	} `yaml:"openai-compatibility"`
	CodexAPIKey []struct {
		APIKey string `yaml:"api-key"`
	} `yaml:"codex-api-key"`
}

func resolveRealtimeAPIKey() (string, error) {
	configPath := resolveCliproxyConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read cliproxy config %q: %w", configPath, err)
	}

	key, err := resolveRealtimeAPIKeyFromYAML(data)
	if err != nil {
		return "", err
	}
	return key, nil
}

func resolveCliproxyConfigPath() string {
	if custom := strings.TrimSpace(os.Getenv(realtimeConfigPathEnv)); custom != "" {
		return custom
	}
	return filepath.Join("config", "cliproxy.yaml")
}

func resolveRealtimeAPIKeyFromYAML(data []byte) (string, error) {
	var cfg realtimeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse cliproxy config: %w", err)
	}

	for i := range cfg.OpenAICompatibility {
		entry := cfg.OpenAICompatibility[i]
		if !isOpenAIBaseURL(entry.BaseURL) {
			continue
		}
		for j := range entry.APIKeyEntries {
			key := strings.TrimSpace(entry.APIKeyEntries[j].APIKey)
			if key != "" {
				return key, nil
			}
		}
	}

	for i := range cfg.CodexAPIKey {
		key := strings.TrimSpace(cfg.CodexAPIKey[i].APIKey)
		if key != "" {
			return key, nil
		}
	}

	authDir := strings.TrimSpace(cfg.AuthDir)
	if authDir == "" {
		authDir = "~/.cli-proxy-api"
	}
	if key, ok := resolveRealtimeAPIKeyFromAuthDir(authDir); ok {
		return key, nil
	}

	return "", fmt.Errorf("no active realtime API key found in current config")
}

func resolveRealtimeAPIKeyFromAuthDir(authDir string) (string, bool) {
	resolvedDir := strings.TrimSpace(authDir)
	if resolvedDir == "" {
		return "", false
	}

	if strings.HasPrefix(resolvedDir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			resolvedDir = filepath.Join(home, strings.TrimPrefix(resolvedDir, "~/"))
		}
	}

	entries, err := os.ReadDir(resolvedDir)
	if err != nil {
		return "", false
	}

	files := make([]fs.DirEntry, 0, len(entries))
	for i := range entries {
		entry := entries[i]
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(entry.Name()))
		if strings.HasSuffix(name, ".json") {
			files = append(files, entry)
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	for i := range files {
		path := filepath.Join(resolvedDir, files[i].Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var token map[string]any
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}

		tokenType, _ := token["type"].(string)
		if strings.TrimSpace(strings.ToLower(tokenType)) != "codex" {
			continue
		}
		if isTruthy(token["disabled"]) {
			continue
		}
		if isExpiredToken(token["expired"]) {
			continue
		}
		accessToken, _ := token["access_token"].(string)
		if key := strings.TrimSpace(accessToken); key != "" {
			return key, true
		}
	}

	return "", false
}

func isTruthy(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		return err == nil && parsed
	default:
		return false
	}
}

func isExpiredToken(v any) bool {
	switch value := v.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return false
		}
		if parsedBool, err := strconv.ParseBool(trimmed); err == nil {
			return parsedBool
		}
		if parsedTime, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return time.Now().After(parsedTime)
		}
		return false
	default:
		return false
	}
}

func isOpenAIBaseURL(baseURL string) bool {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if host == "" {
		host = strings.ToLower(strings.TrimSpace(parsed.Path))
	}
	return strings.Contains(host, "openai.com")
}
