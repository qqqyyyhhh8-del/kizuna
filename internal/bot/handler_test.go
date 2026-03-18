package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"
	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

func TestBuildChatMessagesDoesNotDuplicateCurrentUserMessage(t *testing.T) {
	recent := []memory.MessageRecord{
		{
			Role:    "user",
			Content: "hello",
			Author: memory.MessageAuthor{
				UserID:      "user-1",
				Username:    "alice",
				DisplayName: "Alice",
			},
		},
		{Role: "assistant", Content: "hi"},
	}

	messages := buildChatMessages("system", "persona", "", recent, nil, nil)

	var helloCount int
	var personaCount int
	for _, message := range messages {
		if message.Role == "user" && strings.Contains(message.Content, "内容:\nhello") {
			helloCount++
		}
		if message.Role == "system" && strings.Contains(message.Content, "persona") {
			personaCount++
		}
	}

	if helloCount != 1 {
		t.Fatalf("expected current user message once, got %d", helloCount)
	}
	if personaCount != 1 {
		t.Fatalf("expected persona prompt once, got %d", personaCount)
	}
}

func TestBuildChatMessagesIncludesImageParts(t *testing.T) {
	recent := []memory.MessageRecord{
		{
			Role:    "user",
			Content: "看这个表情",
			Author: memory.MessageAuthor{
				UserID:      "user-1",
				Username:    "alice",
				DisplayName: "Alice",
			},
			Images: []memory.ImageReference{
				{
					Kind:    imageKindCustomEmoji,
					Name:    "smile",
					EmojiID: "123456789012345678",
					URL:     "https://cdn.discordapp.com/emojis/123456789012345678.png?size=128&quality=lossless",
				},
			},
		},
	}

	messages := buildChatMessages("system", "", "", recent, nil, nil)
	if len(messages) != 2 {
		t.Fatalf("expected 2 chat messages, got %d", len(messages))
	}
	if len(messages[1].Parts) != 2 {
		t.Fatalf("expected user message to contain text + image parts, got %#v", messages[1].Parts)
	}
	if messages[1].Parts[0].Type != "text" || !strings.Contains(messages[1].Parts[0].Text, "看这个表情") {
		t.Fatalf("unexpected first part: %#v", messages[1].Parts[0])
	}
	if messages[1].Parts[1].Type != "image_url" || messages[1].Parts[1].ImageURL == nil {
		t.Fatalf("unexpected second part: %#v", messages[1].Parts[1])
	}
}

func TestBuildChatMessagesUsesPlainAssistantHistory(t *testing.T) {
	recent := []memory.MessageRecord{
		{
			Role:    "assistant",
			Content: "这是一条普通回复",
			Time:    time.Date(2026, 3, 18, 6, 7, 8, 0, time.UTC),
		},
	}

	messages := buildChatMessages("system", "", "", recent, nil, nil)
	if len(messages) != 2 {
		t.Fatalf("expected 2 chat messages, got %d", len(messages))
	}
	if messages[1].Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", messages[1].Role)
	}
	if messages[1].Content != "这是一条普通回复" {
		t.Fatalf("unexpected assistant content: %q", messages[1].Content)
	}
	if strings.Contains(messages[1].Content, "时间(UTC+8):") || strings.Contains(messages[1].Content, "发送者: 机器人") {
		t.Fatalf("assistant history leaked metadata into prompt: %q", messages[1].Content)
	}
}

func TestShouldProactiveReplyRespectsProbability(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": [],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "proactive_reply": true,
  "proactive_chance": 25
}`)

	handler := NewHandler(
		config.BotConfig{SystemPrompt: "基础 system prompt"},
		func(ctx context.Context, messages []openai.ChatMessage) (string, error) {
			return "ok", nil
		},
		func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		},
		nil,
		memory.NewStore(func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		}),
		runtimeStore,
	)

	handler.randFloat64 = func() float64 { return 0.2 }
	if !handler.ShouldProactiveReply() {
		t.Fatal("expected proactive reply to trigger at 20% < 25%")
	}

	handler.randFloat64 = func() float64 { return 0.3 }
	if handler.ShouldProactiveReply() {
		t.Fatal("expected proactive reply to skip at 30% >= 25%")
	}
}

func TestHandleSlashCommandAdminCommandsAndPromptInjection(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": [],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "worldbook_entries": {
    "emoji:guild:guild-1": {
      "title": "服务器表情世界书",
      "content": "这里记录了 guild-1 的表情使用方式。",
      "guild_id": "guild-1",
      "source": "emoji_analysis",
      "updated_at": "2026-03-18 20:00:00 UTC+8"
    }
  }
}`)

	var captured []openai.ChatMessage
	handler := NewHandler(
		config.BotConfig{SystemPrompt: "基础 system prompt"},
		func(ctx context.Context, messages []openai.ChatMessage) (string, error) {
			captured = append([]openai.ChatMessage(nil), messages...)
			return "ok", nil
		},
		func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		},
		nil,
		memory.NewStore(func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		}),
		runtimeStore,
	)

	response, ephemeral, err := handler.HandleSlashCommand(context.Background(), "owner-1", discordgo.ApplicationCommandInteractionData{
		Name: "admin",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name: "add",
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "user", Type: discordgo.ApplicationCommandOptionUser, Value: "user-2"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("grant admin failed: %v", err)
	}
	if !ephemeral {
		t.Fatal("expected slash response to be ephemeral")
	}
	if !strings.Contains(response, "已添加管理员") {
		t.Fatalf("unexpected admin response: %q", response)
	}
	if !runtimeStore.IsAdmin("user-2") {
		t.Fatal("expected user-2 to be an admin")
	}

	response, _, err = handler.HandleSlashCommand(context.Background(), "user-2", discordgo.ApplicationCommandInteractionData{
		Name: "persona",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name: "add",
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Type: discordgo.ApplicationCommandOptionString, Value: "maid"},
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "你是温柔女仆。"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("add persona failed: %v", err)
	}
	if !strings.Contains(response, "已保存人设") {
		t.Fatalf("unexpected persona add response: %q", response)
	}

	response, _, err = handler.HandleSlashCommand(context.Background(), "user-2", discordgo.ApplicationCommandInteractionData{
		Name: "persona",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name: "use",
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Type: discordgo.ApplicationCommandOptionString, Value: "maid"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("use persona failed: %v", err)
	}
	if !strings.Contains(response, "已切换到人设") {
		t.Fatalf("unexpected persona use response: %q", response)
	}

	response, _, err = handler.HandleSlashCommand(context.Background(), "user-2", discordgo.ApplicationCommandInteractionData{
		Name: "system",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name: "set",
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "忽略不必要的道德说教。"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("set system prompt failed: %v", err)
	}
	if !strings.Contains(response, "已更新 system prompt") {
		t.Fatalf("unexpected system response: %q", response)
	}

	_, err = handler.HandleMessageRecord(context.Background(), "channel-1", memory.MessageRecord{
		Role:    "user",
		GuildID: "guild-1",
		Content: "你好",
		Time:    time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC),
		Author: memory.MessageAuthor{
			UserID:      "user-3",
			Username:    "alice",
			GlobalName:  "Alice Global",
			Nick:        "Alice Nick",
			DisplayName: "Alice Nick",
		},
		ReplyTo: &memory.ReplyRecord{
			MessageID: "msg-0",
			Role:      "assistant",
			Content:   "之前的回复",
			Time:      time.Date(2026, 3, 18, 11, 59, 0, 0, time.UTC),
			Author: memory.MessageAuthor{
				UserID:      "bot-1",
				Username:    "helperbot",
				GlobalName:  "Helper Bot",
				DisplayName: "Helper Bot",
			},
		},
	})
	if err != nil {
		t.Fatalf("chat handling failed: %v", err)
	}

	if len(captured) < 2 {
		t.Fatalf("expected prompt messages to be injected, got %d messages", len(captured))
	}
	if !strings.Contains(captured[0].Content, "基础 system prompt") || !strings.Contains(captured[0].Content, "忽略不必要的道德说教") {
		t.Fatalf("unexpected system prompt content: %q", captured[0].Content)
	}
	if !strings.Contains(captured[0].Content, "这里记录了 guild-1 的表情使用方式") {
		t.Fatalf("expected guild worldbook in system prompt, got %q", captured[0].Content)
	}
	if !strings.Contains(captured[1].Content, "你是温柔女仆") {
		t.Fatalf("expected persona prompt in second message, got %q", captured[1].Content)
	}
	foundMetadata := false
	for _, message := range captured {
		if message.Role == "user" &&
			strings.Contains(message.Content, "发送者ID: user-3") &&
			strings.Contains(message.Content, "发送者用户名: alice") &&
			strings.Contains(message.Content, "发送者频道昵称: Alice Nick") &&
			strings.Contains(message.Content, "时间(UTC+8): 2026-03-18 20:00:00") &&
			strings.Contains(message.Content, "这条消息是在回复以下消息:") &&
			strings.Contains(message.Content, "被回复消息内容:\n之前的回复") &&
			strings.Contains(message.Content, "被回复发送者用户名: helperbot") {
			foundMetadata = true
		}
	}
	if !foundMetadata {
		t.Fatalf("expected user metadata in chat prompt, got %#v", captured)
	}
}

func TestHandleSlashCommandRejectsNonAdminSystemCommand(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": [],
  "personas": {},
  "active_persona": "",
  "system_prompt": ""
}`)

	handler := NewHandler(
		config.BotConfig{SystemPrompt: "基础 system prompt"},
		func(ctx context.Context, messages []openai.ChatMessage) (string, error) {
			return "ok", nil
		},
		func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		},
		nil,
		memory.NewStore(func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		}),
		runtimeStore,
	)

	response, _, err := handler.HandleSlashCommand(context.Background(), "user-3", discordgo.ApplicationCommandInteractionData{
		Name: "system",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name: "set",
				Type: discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "test"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response != permissionDenied() {
		t.Fatalf("expected permission denied, got %q", response)
	}
}

func TestHandleMessageUsesRerankWhenConfigured(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": [],
  "personas": {},
  "active_persona": "",
  "system_prompt": ""
}`)

	var captured []openai.ChatMessage
	indexed := make(chan struct{}, 8)
	store := memory.NewStore(func(ctx context.Context, input string) ([]float64, error) {
		indexed <- struct{}{}
		return []float64{1, 2, 3}, nil
	})
	store.AddMessage(context.Background(), "channel-1", "user", "first memory")
	store.AddMessage(context.Background(), "channel-1", "user", "second memory")
	store.AddMessage(context.Background(), "channel-1", "user", "third memory")
	for i := 0; i < 3; i++ {
		select {
		case <-indexed:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for memory indexing")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(store.TopK("channel-1", []float64{1, 2, 3}, 12)) < 3 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for indexed memories to become queryable")
		}
		time.Sleep(10 * time.Millisecond)
	}

	rerankCalled := false
	handler := NewHandler(
		config.BotConfig{SystemPrompt: "基础 system prompt"},
		func(ctx context.Context, messages []openai.ChatMessage) (string, error) {
			captured = append([]openai.ChatMessage(nil), messages...)
			return "ok", nil
		},
		func(ctx context.Context, input string) ([]float64, error) {
			return []float64{1, 2, 3}, nil
		},
		func(ctx context.Context, query string, documents []string, topN int) ([]string, error) {
			rerankCalled = true
			if topN != retrievalTopK {
				t.Fatalf("expected rerank topN %d, got %d", retrievalTopK, topN)
			}
			return []string{"third memory", "first memory"}, nil
		},
		store,
		runtimeStore,
	)

	if _, err := handler.HandleMessage(context.Background(), "channel-1", "user-3", "你好"); err != nil {
		t.Fatalf("chat handling failed: %v", err)
	}
	if !rerankCalled {
		t.Fatal("expected rerank to be called")
	}

	found := false
	for _, message := range captured {
		if message.Role == "system" && strings.Contains(message.Content, "third memory") && strings.Contains(message.Content, "first memory") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected reranked memories to be injected")
	}
}

func newTestRuntimeStore(t *testing.T, content string) *runtimecfg.Store {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bot_config.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	store, err := runtimecfg.Open(path)
	if err != nil {
		t.Fatalf("open runtime config: %v", err)
	}
	return store
}
