package bot

import (
	"context"
	"strings"
	"testing"

	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"
	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

func TestProactivePanelCommandResponseForAdmin(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "allowed_guild_ids": ["guild-1"],
  "proactive_reply": true,
  "proactive_chance": 12.5
}`)

	handler := newPanelTestHandler(runtimeStore)
	response, err := handler.ProactivePanelCommandResponse("admin-1", speechLocation{
		GuildID: "guild-1",
	})
	if err != nil {
		t.Fatalf("proactive panel response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral response, got %#v", response.Data)
	}
	if len(response.Data.Embeds) != 1 || response.Data.Embeds[0].Title != "Proactive Reply Control" {
		t.Fatalf("unexpected embeds: %#v", response.Data.Embeds)
	}
}

func TestProactiveComponentResponseEnableBlockedWhenLocationNotAllowed(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "allowed_guild_ids": [],
  "proactive_reply": false,
  "proactive_chance": 10
}`)

	handler := newPanelTestHandler(runtimeStore)
	response, err := handler.ProactiveComponentResponse("admin-1", speechLocation{
		GuildID: "guild-1",
	}, discordgo.MessageComponentInteractionData{
		CustomID: proactiveActionEnable,
	})
	if err != nil {
		t.Fatalf("proactive enable response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseUpdateMessage {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	enabled, _ := runtimeStore.ProactiveReplyConfig()
	if enabled {
		t.Fatal("expected proactive reply to stay disabled")
	}
	if response.Data == nil || !strings.Contains(response.Data.Content, "还没有在 `/setup` 里放行") {
		t.Fatalf("unexpected response content: %#v", response.Data)
	}
}

func TestProactiveComponentResponseEnableWhenLocationAllowed(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "allowed_guild_ids": ["guild-1"],
  "proactive_reply": false,
  "proactive_chance": 10
}`)

	handler := newPanelTestHandler(runtimeStore)
	response, err := handler.ProactiveComponentResponse("admin-1", speechLocation{
		GuildID: "guild-1",
	}, discordgo.MessageComponentInteractionData{
		CustomID: proactiveActionEnable,
	})
	if err != nil {
		t.Fatalf("proactive enable response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseUpdateMessage {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	enabled, _ := runtimeStore.ProactiveReplyConfig()
	if !enabled {
		t.Fatal("expected proactive reply to be enabled")
	}
	if response.Data == nil || response.Data.Content != "已开启主动回复。" {
		t.Fatalf("unexpected response content: %#v", response.Data)
	}
}

func TestProactiveModalResponseUpdatesChance(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {},
  "active_persona": "",
  "system_prompt": "",
  "allowed_guild_ids": ["guild-1"],
  "proactive_reply": false,
  "proactive_chance": 1
}`)

	handler := newPanelTestHandler(runtimeStore)
	response, err := handler.ProactiveModalResponse("admin-1", speechLocation{
		GuildID: "guild-1",
	}, discordgo.ModalSubmitInteractionData{
		CustomID: proactiveModalChance,
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: proactiveModalFieldChance, Value: "12.5"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("proactive modal response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	_, chance := runtimeStore.ProactiveReplyConfig()
	if chance != 12.5 {
		t.Fatalf("expected proactive chance 12.5, got %v", chance)
	}
}

func newPanelTestHandler(runtimeStore *runtimecfg.Store) *Handler {
	return NewHandler(
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
}
