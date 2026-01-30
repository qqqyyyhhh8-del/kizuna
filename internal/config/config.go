package config

import (
	"errors"
	"os"
	"strings"
)

const (
	defaultBaseURL    = "https://api.openai.com/v1"
	defaultChatModel  = "gpt-4o-mini"
	defaultEmbedModel = "text-embedding-3-small"
)

type OpenAIConfig struct {
	BaseURL    string
	APIKey     string
	ChatModel  string
	EmbedModel string
}

type BotConfig struct {
	DiscordToken string
	SystemPrompt string
}

type Config struct {
	OpenAI OpenAIConfig
	Bot    BotConfig
}

func Load() (Config, error) {
	discordToken := strings.TrimSpace(os.Getenv("DISCORD_TOKEN"))
	if discordToken == "" {
		return Config{}, errors.New("DISCORD_TOKEN is required")
	}
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
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
	systemPrompt := strings.TrimSpace(os.Getenv("SYSTEM_PROMPT"))
	if systemPrompt == "" {
		systemPrompt = "你是Discord聊天助手，回答清晰、友好，避免重复。"
	}

	return Config{
		OpenAI: OpenAIConfig{
			BaseURL:    baseURL,
			APIKey:     apiKey,
			ChatModel:  chatModel,
			EmbedModel: embedModel,
		},
		Bot: BotConfig{
			DiscordToken: discordToken,
			SystemPrompt: systemPrompt,
		},
	}, nil
}
