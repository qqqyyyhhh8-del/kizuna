package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"discordbot/internal/config"
	"discordbot/internal/memory"
	"discordbot/internal/openai"
)

const (
	maxHistoryMessages  = 14
	summaryTriggerCount = 18
)

type ChatFn func(ctx context.Context, messages []openai.ChatMessage) (string, error)
type EmbedFn func(ctx context.Context, input string) ([]float64, error)

type Handler struct {
	cfg     config.BotConfig
	chatFn  ChatFn
	embedFn EmbedFn
	store   *memory.Store
}

func NewHandler(cfg config.BotConfig, chatFn ChatFn, embedFn EmbedFn, store *memory.Store) *Handler {
	return &Handler{
		cfg:     cfg,
		chatFn:  chatFn,
		embedFn: embedFn,
		store:   store,
	}
}

func (h *Handler) HandleMessage(ctx context.Context, channelID, content string) (string, error) {
	h.store.AddMessage(ctx, channelID, "user", content)

	summary, recent := h.store.SummaryAndRecent(channelID)
	if shouldSummarize(recent) {
		summaryContent, err := h.chatFn(ctx, summarizationPrompt(summary, recent))
		if err != nil {
			log.Printf("summary error: %v", err)
		} else {
			h.store.SetSummary(channelID, summaryContent)
			h.store.TrimHistory(channelID, maxHistoryMessages)
			summary = summaryContent
			_, recent = h.store.SummaryAndRecent(channelID)
		}
	}

	var retrieved []string
	queryEmbedding, err := h.embedFn(ctx, content)
	if err != nil {
		log.Printf("embed error: %v", err)
	} else {
		retrieved = h.store.TopK(channelID, queryEmbedding, 4)
	}

	response, err := h.chatFn(ctx, buildChatMessages(h.cfg.SystemPrompt, summary, recent, retrieved, content))
	if err != nil {
		return "", err
	}
	h.store.AddMessage(ctx, channelID, "assistant", response)
	return response, nil
}

func shouldSummarize(messages []memory.MessageRecord) bool {
	return len(messages) >= summaryTriggerCount
}

func summarizationPrompt(summary string, messages []memory.MessageRecord) []openai.ChatMessage {
	var builder strings.Builder
	if summary != "" {
		builder.WriteString("当前已知摘要:\n")
		builder.WriteString(summary)
		builder.WriteString("\n\n")
	}
	builder.WriteString("需要总结的新对话:\n")
	for _, msg := range messages {
		builder.WriteString(fmt.Sprintf("[%s] %s\n", msg.Role, msg.Content))
	}
	builder.WriteString("\n请用简洁中文更新摘要，保留关键事实、偏好和待办。")
	return []openai.ChatMessage{
		{Role: "system", Content: "你是一个对话摘要助手。"},
		{Role: "user", Content: builder.String()},
	}
}

func buildChatMessages(systemPrompt, summary string, recent []memory.MessageRecord, retrieved []string, userContent string) []openai.ChatMessage {
	messages := []openai.ChatMessage{{Role: "system", Content: systemPrompt}}
	if summary != "" {
		messages = append(messages, openai.ChatMessage{
			Role:    "system",
			Content: "对话摘要:\n" + summary,
		})
	}
	if len(retrieved) > 0 {
		messages = append(messages, openai.ChatMessage{
			Role:    "system",
			Content: "相关记忆(仅供参考):\n- " + strings.Join(retrieved, "\n- "),
		})
	}
	for _, msg := range recent {
		messages = append(messages, openai.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	messages = append(messages, openai.ChatMessage{Role: "user", Content: userContent})
	return messages
}

func DefaultTimeout() time.Duration {
	return 45 * time.Second
}
