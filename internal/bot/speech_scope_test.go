package bot

import (
	"context"
	"testing"

	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"

	"github.com/bwmarrin/discordgo"
)

func TestAllowsSpeechForThreadMessageUsesParentChannelAndThreadIDs(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "speech_mode": "allowlist",
  "allowed_guild_ids": [],
  "allowed_channel_ids": ["parent-1"],
  "allowed_thread_ids": []
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

	session := &discordgo.Session{State: discordgo.NewState()}
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID:       "guild-1",
		Channels: []*discordgo.Channel{{ID: "parent-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText}},
		Threads:  []*discordgo.Channel{{ID: "thread-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildPublicThread, ParentID: "parent-1"}},
	}); err != nil {
		t.Fatalf("guild add: %v", err)
	}

	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID:   "guild-1",
			ChannelID: "thread-1",
		},
	}

	if !handler.AllowsSpeechForMessage(session, message) {
		t.Fatal("expected parent channel allowlist to permit thread message")
	}
}

func TestAllowsSpeechForMessageBlocksWhenModeNone(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "speech_mode": "none"
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

	if handler.AllowsSpeechForMessage(&discordgo.Session{}, &discordgo.MessageCreate{
		Message: &discordgo.Message{GuildID: "guild-1", ChannelID: "channel-1"},
	}) {
		t.Fatal("expected none mode to block message")
	}
}

func TestAllowsSpeechForMessageSupportsSecondGuildAndChannelEntries(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "speech_mode": "allowlist",
  "allowed_guild_ids": ["guild-1", "guild-2"],
  "allowed_channel_ids": ["channel-1", "channel-2"],
  "allowed_thread_ids": ["thread-1", "thread-2"]
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

	if !handler.AllowsSpeechForMessage(&discordgo.Session{}, &discordgo.MessageCreate{
		Message: &discordgo.Message{GuildID: "guild-2", ChannelID: "other-channel"},
	}) {
		t.Fatal("expected second guild allowlist entry to permit message")
	}

	if !handler.AllowsSpeechForMessage(&discordgo.Session{}, &discordgo.MessageCreate{
		Message: &discordgo.Message{GuildID: "other-guild", ChannelID: "channel-2"},
	}) {
		t.Fatal("expected second channel allowlist entry to permit message")
	}
}

func TestAllowsSpeechForMessageAllowsRawThreadIDWhenChannelIsUnresolved(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "speech_mode": "allowlist",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": ["thread-2"]
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

	if !handler.AllowsSpeechForMessage(&discordgo.Session{}, &discordgo.MessageCreate{
		Message: &discordgo.Message{GuildID: "guild-2", ChannelID: "thread-2"},
	}) {
		t.Fatal("expected unresolved raw thread id to permit message")
	}
}
