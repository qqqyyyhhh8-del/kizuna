package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"
	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

func TestSetupPanelCommandResponseIncludesEmbedAndButtons(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "system_prompt": "",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": []
}`)

	handler := newSetupTestHandler(runtimeStore)
	response, err := handler.SetupPanelCommandResponse("admin-1", speechLocation{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
	})
	if err != nil {
		t.Fatalf("setup panel response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral response, got %#v", response.Data)
	}
	if len(response.Data.Embeds) != 1 || response.Data.Embeds[0].Title != "Speech Scope Setup" {
		t.Fatalf("unexpected embeds: %#v", response.Data.Embeds)
	}
	if len(response.Data.Components) != 1 {
		t.Fatalf("expected one button row, got %#v", response.Data.Components)
	}
	row, ok := response.Data.Components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("expected actions row, got %T", response.Data.Components[0])
	}
	if len(row.Components) != 4 {
		t.Fatalf("expected 4 buttons, got %d", len(row.Components))
	}
	serverButton := row.Components[0].(discordgo.Button)
	channelButton := row.Components[1].(discordgo.Button)
	if serverButton.Label != "放行当前服务器" {
		t.Fatalf("unexpected server button label: %q", serverButton.Label)
	}
	if channelButton.Label != "放行当前频道" {
		t.Fatalf("unexpected channel button label: %q", channelButton.Label)
	}
}

func TestSetupComponentResponseTogglesServerAllowAndPersists(t *testing.T) {
	runtimeStore, configPath := newTestStoreForBot(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "system_prompt": "",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": []
}`)

	handler := newSetupTestHandler(runtimeStore)
	response, err := handler.SetupComponentResponse("admin-1", speechLocation{
		GuildID:   "100",
		ChannelID: "200",
	}, discordgo.MessageComponentInteractionData{
		CustomID: setupActionToggleServer,
	})
	if err != nil {
		t.Fatalf("toggle server allow: %v", err)
	}
	if response.Type != discordgo.InteractionResponseUpdateMessage {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || !strings.Contains(response.Data.Content, "已放行当前服务器: `100`") {
		t.Fatalf("unexpected response data: %#v", response.Data)
	}
	if !runtimeStore.AllowsSpeech("100", "", "") {
		t.Fatal("expected guild to be allowed after toggle")
	}

	reopened, err := openRuntimeStore(configPath)
	if err != nil {
		t.Fatalf("reopen runtime store: %v", err)
	}
	if !reopened.AllowsSpeech("100", "", "") {
		t.Fatal("expected guild allowlist entry to persist")
	}
}

func TestSetupPanelDisablesLocalToggleWhenServerAlreadyAllowed(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "system_prompt": "",
  "allowed_guild_ids": ["guild-1"],
  "allowed_channel_ids": ["channel-2"],
  "allowed_thread_ids": []
}`)

	handler := newSetupTestHandler(runtimeStore)
	data, err := handler.setupPanelResponseData(speechLocation{
		GuildID:   "guild-1",
		ChannelID: "channel-2",
	}, "")
	if err != nil {
		t.Fatalf("build setup panel: %v", err)
	}
	row := data.Components[0].(discordgo.ActionsRow)
	serverButton := row.Components[0].(discordgo.Button)
	channelButton := row.Components[1].(discordgo.Button)
	if serverButton.Style != discordgo.DangerButton {
		t.Fatalf("expected server button to become danger, got %v", serverButton.Style)
	}
	if !channelButton.Disabled {
		t.Fatal("expected channel button to be disabled when guild is already allowed")
	}
	if channelButton.Label != "服务器已放行" {
		t.Fatalf("unexpected disabled button label: %q", channelButton.Label)
	}
}

func TestSetupPanelShowsDangerButtonForAllowedThread(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "system_prompt": "",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": ["thread-3"]
}`)

	handler := newSetupTestHandler(runtimeStore)
	data, err := handler.setupPanelResponseData(speechLocation{
		GuildID:   "guild-1",
		ChannelID: "channel-2",
		ThreadID:  "thread-3",
	}, "")
	if err != nil {
		t.Fatalf("build thread setup panel: %v", err)
	}
	row := data.Components[0].(discordgo.ActionsRow)
	threadButton := row.Components[1].(discordgo.Button)
	if threadButton.Style != discordgo.DangerButton {
		t.Fatalf("expected thread button to become danger, got %v", threadButton.Style)
	}
	if threadButton.Label != "取消放行当前子区" {
		t.Fatalf("unexpected thread button label: %q", threadButton.Label)
	}
}

func TestSetupComponentResponseTogglesThreadAllow(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "system_prompt": "",
  "allowed_guild_ids": [],
  "allowed_channel_ids": [],
  "allowed_thread_ids": []
}`)

	handler := newSetupTestHandler(runtimeStore)
	response, err := handler.SetupComponentResponse("admin-1", speechLocation{
		GuildID:   "guild-1",
		ChannelID: "channel-2",
		ThreadID:  "thread-3",
	}, discordgo.MessageComponentInteractionData{
		CustomID: setupActionToggleThread,
	})
	if err != nil {
		t.Fatalf("toggle thread allow: %v", err)
	}
	if response.Type != discordgo.InteractionResponseUpdateMessage {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if !runtimeStore.AllowsSpeech("", "", "thread-3") {
		t.Fatal("expected thread to be allowed after toggle")
	}
}

func TestSetupPanelRejectsNonAdmin(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": [],
  "system_prompt": ""
}`)

	handler := newSetupTestHandler(runtimeStore)
	response, err := handler.SetupPanelCommandResponse("user-1", speechLocation{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
	})
	if err != nil {
		t.Fatalf("setup panel non-admin: %v", err)
	}
	if response.Data == nil || response.Data.Content != permissionDenied() {
		t.Fatalf("unexpected non-admin response: %#v", response.Data)
	}
}

func newSetupTestHandler(runtimeStore *runtimecfg.Store) *Handler {
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

func newTestStoreForBot(t *testing.T, content string) (*runtimecfg.Store, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bot_config.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	store, err := runtimecfg.Open(path)
	if err != nil {
		t.Fatalf("open runtime config: %v", err)
	}
	return store, path
}

func openRuntimeStore(path string) (*runtimecfg.Store, error) {
	return runtimecfg.Open(path)
}
