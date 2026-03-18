package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL        = "https://api.openai.com/v1"
	defaultChatModel      = "gpt-4o-mini"
	defaultEmbedModel     = "text-embedding-3-small"
	defaultConfigFilePath = "bot_config.json"
	defaultSQLitePath     = "bot.db"
	defaultPluginsDir     = "plugins"
)

type OpenAIConfig struct {
	BaseURL       string
	APIKey        string
	ChatModel     string
	EmbedBaseURL  string
	EmbedAPIKey   string
	EmbedModel    string
	RerankBaseURL string
	RerankAPIKey  string
	RerankModel   string
	HTTPTimeout   time.Duration
}

type BotConfig struct {
	DiscordToken   string
	SystemPrompt   string
	ConfigFilePath string
	SQLitePath     string
	CommandGuildID string
	PluginsDir     string
}

type Config struct {
	OpenAI OpenAIConfig
	Bot    BotConfig
}

func Load() (Config, error) {
	if err := loadDefaultDotEnv(); err != nil {
		return Config{}, err
	}

	discordToken := firstEnvValue("DISCORD_TOKEN", "DISCORDTOKEN", "DISCORD_BOT_TOKEN", "discordtoken")
	if discordToken == "" {
		return Config{}, errors.New("DISCORD_TOKEN is required")
	}
	apiKey := firstEnvValue("OPENAI_API_KEY")
	if apiKey == "" {
		return Config{}, errors.New("OPENAI_API_KEY is required")
	}

	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	chatModel := strings.TrimSpace(os.Getenv("OPENAI_CHAT_MODEL"))
	if chatModel == "" {
		chatModel = defaultChatModel
	}
	embedModel := strings.TrimSpace(os.Getenv("OPENAI_EMBED_MODEL"))
	if embedModel == "" {
		embedModel = defaultEmbedModel
	}
	embedBaseURL := strings.TrimSpace(os.Getenv("OPENAI_EMBED_BASE_URL"))
	if embedBaseURL == "" {
		embedBaseURL = baseURL
	}
	embedAPIKey := strings.TrimSpace(os.Getenv("OPENAI_EMBED_API_KEY"))
	if embedAPIKey == "" {
		embedAPIKey = apiKey
	}
	rerankBaseURL := strings.TrimSpace(os.Getenv("OPENAI_RERANK_BASE_URL"))
	if rerankBaseURL == "" {
		rerankBaseURL = baseURL
	}
	rerankAPIKey := strings.TrimSpace(os.Getenv("OPENAI_RERANK_API_KEY"))
	if rerankAPIKey == "" {
		rerankAPIKey = apiKey
	}
	rerankModel := strings.TrimSpace(os.Getenv("OPENAI_RERANK_MODEL"))
	httpTimeout, err := durationFromEnvSeconds("OPENAI_HTTP_TIMEOUT_SECONDS")
	if err != nil {
		return Config{}, err
	}
	systemPrompt := strings.TrimSpace(os.Getenv("SYSTEM_PROMPT"))
	if systemPrompt == "" {
		systemPrompt = "你是Discord聊天助手，回答清晰、友好，避免重复。"
	}
	configFilePath := strings.TrimSpace(os.Getenv("BOT_CONFIG_FILE"))
	if configFilePath == "" {
		configFilePath = defaultConfigFilePath
	}
	sqlitePath := strings.TrimSpace(os.Getenv("BOT_SQLITE_PATH"))
	if sqlitePath == "" {
		sqlitePath = defaultSQLitePath
	}
	commandGuildID := strings.TrimSpace(os.Getenv("BOT_COMMAND_GUILD_ID"))
	pluginsDir := strings.TrimSpace(os.Getenv("PLUGINS_DIR"))
	if pluginsDir == "" {
		pluginsDir = defaultPluginsDir
	}
	return Config{
		OpenAI: OpenAIConfig{
			BaseURL:       baseURL,
			APIKey:        apiKey,
			ChatModel:     chatModel,
			EmbedBaseURL:  embedBaseURL,
			EmbedAPIKey:   embedAPIKey,
			EmbedModel:    embedModel,
			RerankBaseURL: rerankBaseURL,
			RerankAPIKey:  rerankAPIKey,
			RerankModel:   rerankModel,
			HTTPTimeout:   httpTimeout,
		},
		Bot: BotConfig{
			DiscordToken:   discordToken,
			SystemPrompt:   systemPrompt,
			ConfigFilePath: configFilePath,
			SQLitePath:     sqlitePath,
			CommandGuildID: commandGuildID,
			PluginsDir:     pluginsDir,
		},
	}, nil
}

func loadDefaultDotEnv() error {
	explicitPath := strings.TrimSpace(os.Getenv("BOT_ENV_FILE"))
	if explicitPath != "" {
		return loadDotEnvPaths(explicitPath)
	}

	paths := []string{".env"}
	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		if executableDir != "" {
			paths = append(paths, filepath.Join(executableDir, ".env"))
		}
	}
	return loadDotEnvPaths(paths...)
}

func loadDotEnvPaths(paths ...string) error {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		if err := loadDotEnv(cleanPath); err != nil {
			return err
		}
	}
	return nil
}

func loadDotEnv(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	file, err := os.Open(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := splitDotEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func splitDotEnvLine(line string) (string, string, bool) {
	index := strings.IndexRune(line, '=')
	if index <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:index])
	value := strings.TrimSpace(line[index+1:])
	if key == "" {
		return "", "", false
	}

	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}

	return key, value, true
}

func firstEnvValue(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(strings.TrimSpace(key)))
		if value != "" {
			return value
		}
	}
	return ""
}

func durationFromEnvSeconds(key string) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(strings.TrimSpace(key)))
	if value == "" {
		return 0, nil
	}

	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New(key + " must be an integer number of seconds")
	}
	if seconds < 0 {
		return 0, errors.New(key + " must be >= 0")
	}
	return time.Duration(seconds) * time.Second, nil
}
