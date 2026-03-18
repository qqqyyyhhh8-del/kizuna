package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadReadsDotEnvFile(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	dotEnv := `DISCORD_TOKEN=discord-token
OPENAI_API_KEY=openai-key
OPENAI_BASE_URL=https://example.com/v1
OPENAI_CHAT_MODEL=test-chat
OPENAI_EMBED_BASE_URL=https://embed.example.com/v1
OPENAI_EMBED_API_KEY=embed-key
OPENAI_EMBED_MODEL=test-embed
OPENAI_RERANK_BASE_URL=https://rerank.example.com/v1
OPENAI_RERANK_API_KEY=rerank-key
OPENAI_RERANK_MODEL=test-rerank
OPENAI_HTTP_TIMEOUT_SECONDS=600
SYSTEM_PROMPT="测试 system prompt"
BOT_CONFIG_FILE=runtime-config.json
BOT_SQLITE_PATH=runtime.db
BOT_COMMAND_GUILD_ID=test-guild
PLUGINS_DIR=test-plugins
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotEnv), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	for _, key := range []string{
		"DISCORD_TOKEN",
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"OPENAI_CHAT_MODEL",
		"OPENAI_EMBED_BASE_URL",
		"OPENAI_EMBED_API_KEY",
		"OPENAI_EMBED_MODEL",
		"OPENAI_RERANK_BASE_URL",
		"OPENAI_RERANK_API_KEY",
		"OPENAI_RERANK_MODEL",
		"OPENAI_HTTP_TIMEOUT_SECONDS",
		"SYSTEM_PROMPT",
		"BOT_CONFIG_FILE",
		"BOT_SQLITE_PATH",
		"BOT_COMMAND_GUILD_ID",
		"PLUGINS_DIR",
	} {
		restoreEnvKey(t, key, nil)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Bot.DiscordToken != "discord-token" {
		t.Fatalf("unexpected discord token: %q", cfg.Bot.DiscordToken)
	}
	if cfg.OpenAI.APIKey != "openai-key" {
		t.Fatalf("unexpected api key: %q", cfg.OpenAI.APIKey)
	}
	if cfg.OpenAI.BaseURL != "https://example.com/v1" {
		t.Fatalf("unexpected base url: %q", cfg.OpenAI.BaseURL)
	}
	if cfg.OpenAI.ChatModel != "test-chat" {
		t.Fatalf("unexpected chat model: %q", cfg.OpenAI.ChatModel)
	}
	if cfg.OpenAI.EmbedBaseURL != "https://embed.example.com/v1" {
		t.Fatalf("unexpected embed base url: %q", cfg.OpenAI.EmbedBaseURL)
	}
	if cfg.OpenAI.EmbedAPIKey != "embed-key" {
		t.Fatalf("unexpected embed api key: %q", cfg.OpenAI.EmbedAPIKey)
	}
	if cfg.OpenAI.EmbedModel != "test-embed" {
		t.Fatalf("unexpected embed model: %q", cfg.OpenAI.EmbedModel)
	}
	if cfg.OpenAI.RerankBaseURL != "https://rerank.example.com/v1" {
		t.Fatalf("unexpected rerank base url: %q", cfg.OpenAI.RerankBaseURL)
	}
	if cfg.OpenAI.RerankAPIKey != "rerank-key" {
		t.Fatalf("unexpected rerank api key: %q", cfg.OpenAI.RerankAPIKey)
	}
	if cfg.OpenAI.RerankModel != "test-rerank" {
		t.Fatalf("unexpected rerank model: %q", cfg.OpenAI.RerankModel)
	}
	if cfg.OpenAI.HTTPTimeout != 10*time.Minute {
		t.Fatalf("unexpected http timeout: %v", cfg.OpenAI.HTTPTimeout)
	}
	if cfg.Bot.SystemPrompt != "测试 system prompt" {
		t.Fatalf("unexpected system prompt: %q", cfg.Bot.SystemPrompt)
	}
	if cfg.Bot.ConfigFilePath != "runtime-config.json" {
		t.Fatalf("unexpected config file path: %q", cfg.Bot.ConfigFilePath)
	}
	if cfg.Bot.SQLitePath != "runtime.db" {
		t.Fatalf("unexpected sqlite path: %q", cfg.Bot.SQLitePath)
	}
	if cfg.Bot.CommandGuildID != "test-guild" {
		t.Fatalf("unexpected command guild id: %q", cfg.Bot.CommandGuildID)
	}
	if cfg.Bot.PluginsDir != "test-plugins" {
		t.Fatalf("unexpected plugins dir: %q", cfg.Bot.PluginsDir)
	}
}

func restoreEnvKey(t *testing.T, key string, newValue *string) {
	t.Helper()

	previousValue, existed := os.LookupEnv(key)
	if newValue == nil {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset env %s: %v", key, err)
		}
	} else {
		if err := os.Setenv(key, *newValue); err != nil {
			t.Fatalf("set env %s: %v", key, err)
		}
	}

	t.Cleanup(func() {
		var err error
		if existed {
			err = os.Setenv(key, previousValue)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Fatalf("restore env %s: %v", key, err)
		}
	})
}

func TestLoadDoesNotOverrideExistingEnvironment(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte("DISCORD_TOKEN=file-token\nOPENAI_API_KEY=file-key\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("DISCORD_TOKEN", "env-token")
	t.Setenv("OPENAI_API_KEY", "env-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Bot.DiscordToken != "env-token" {
		t.Fatalf("expected existing env token to win, got %q", cfg.Bot.DiscordToken)
	}
	if cfg.OpenAI.APIKey != "env-key" {
		t.Fatalf("expected existing env key to win, got %q", cfg.OpenAI.APIKey)
	}
}

func TestLoadFallsBackEmbedAndRerankCredentialsToPrimaryOpenAIConfig(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	dotEnv := `DISCORD_TOKEN=discord-token
OPENAI_API_KEY=openai-key
OPENAI_BASE_URL=https://example.com/v1
OPENAI_RERANK_MODEL=test-rerank
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotEnv), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	for _, key := range []string{
		"DISCORD_TOKEN",
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"OPENAI_RERANK_MODEL",
		"OPENAI_EMBED_BASE_URL",
		"OPENAI_EMBED_API_KEY",
		"OPENAI_RERANK_BASE_URL",
		"OPENAI_RERANK_API_KEY",
	} {
		restoreEnvKey(t, key, nil)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.OpenAI.EmbedBaseURL != cfg.OpenAI.BaseURL {
		t.Fatalf("expected embed base url fallback, got %q", cfg.OpenAI.EmbedBaseURL)
	}
	if cfg.OpenAI.EmbedAPIKey != cfg.OpenAI.APIKey {
		t.Fatalf("expected embed api key fallback, got %q", cfg.OpenAI.EmbedAPIKey)
	}
	if cfg.OpenAI.RerankBaseURL != cfg.OpenAI.BaseURL {
		t.Fatalf("expected rerank base url fallback, got %q", cfg.OpenAI.RerankBaseURL)
	}
	if cfg.OpenAI.RerankAPIKey != cfg.OpenAI.APIKey {
		t.Fatalf("expected rerank api key fallback, got %q", cfg.OpenAI.RerankAPIKey)
	}
}

func TestLoadDotEnvPathsUsesLaterExistingPathWhenEarlierMissing(t *testing.T) {
	tempDir := t.TempDir()
	secondPath := filepath.Join(tempDir, "second.env")
	if err := os.WriteFile(secondPath, []byte("DISCORD_TOKEN=discord-token\n"), 0o600); err != nil {
		t.Fatalf("write second env: %v", err)
	}

	restoreEnvKey(t, "DISCORD_TOKEN", nil)
	if err := loadDotEnvPaths(filepath.Join(tempDir, "missing.env"), secondPath); err != nil {
		t.Fatalf("loadDotEnvPaths: %v", err)
	}
	if got := os.Getenv("DISCORD_TOKEN"); got != "discord-token" {
		t.Fatalf("expected env from fallback path, got %q", got)
	}
}

func TestLoadAcceptsDiscordTokenAlias(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	dotEnv := `discordtoken=alias-token
OPENAI_API_KEY=openai-key
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotEnv), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	restoreEnvKey(t, "DISCORD_TOKEN", nil)
	restoreEnvKey(t, "DISCORDTOKEN", nil)
	restoreEnvKey(t, "DISCORD_BOT_TOKEN", nil)
	restoreEnvKey(t, "discordtoken", nil)
	restoreEnvKey(t, "OPENAI_API_KEY", nil)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Bot.DiscordToken != "alias-token" {
		t.Fatalf("expected alias token, got %q", cfg.Bot.DiscordToken)
	}
}

func TestLoadRejectsInvalidOpenAIHTTPTimeout(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	dotEnv := `DISCORD_TOKEN=discord-token
OPENAI_API_KEY=openai-key
OPENAI_HTTP_TIMEOUT_SECONDS=abc
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotEnv), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	restoreEnvKey(t, "DISCORD_TOKEN", nil)
	restoreEnvKey(t, "OPENAI_API_KEY", nil)
	restoreEnvKey(t, "OPENAI_HTTP_TIMEOUT_SECONDS", nil)

	_, err = Load()
	if err == nil || err.Error() != "OPENAI_HTTP_TIMEOUT_SECONDS must be an integer number of seconds" {
		t.Fatalf("unexpected error: %v", err)
	}
}
