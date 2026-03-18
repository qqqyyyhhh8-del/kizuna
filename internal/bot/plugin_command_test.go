package bot

import (
	"path/filepath"
	"strings"
	"testing"

	"kizuna/internal/pluginhost"
	"kizuna/pkg/pluginapi"

	"github.com/bwmarrin/discordgo"
)

func TestBuildPluginPanelEmbedIncludesSelectedPluginDetails(t *testing.T) {
	plugin := pluginhost.InstalledPlugin{
		ID:          "official_emoji",
		Name:        "Official Emoji Plugin",
		Version:     "v0.1.0",
		Description: "Emoji analysis",
		Repo:        "https://github.com/example/discord-bot-plugins.git",
		Ref:         "main",
		SourcePath:  "plugins/emoji",
		Enabled:     true,
		GuildMode:   pluginhost.GuildModeAllowlist,
		GuildIDs:    []string{"guild-1"},
		GrantedCaps: []pluginapi.Capability{pluginapi.CapabilityDiscordReadGuildEmojis, pluginapi.CapabilityWorldBookWrite},
		Manifest: pluginapi.Manifest{
			Commands: []pluginapi.CommandSpec{{Name: "emoji", Description: "emoji"}},
		},
	}

	embed := buildPluginPanelEmbed([]pluginhost.InstalledPlugin{plugin}, plugin, true, speechLocation{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
	}, true, "已刷新")
	if embed == nil {
		t.Fatal("expected embed")
	}
	if embed.Title != "Plugin Control Center" {
		t.Fatalf("unexpected title: %q", embed.Title)
	}
	if !strings.Contains(embed.Description, "已刷新") {
		t.Fatalf("expected notice in description, got %q", embed.Description)
	}

	var foundCurrent, foundCaps bool
	for _, field := range embed.Fields {
		if field == nil {
			continue
		}
		if field.Name == "当前选中" {
			foundCurrent = true
			if !strings.Contains(field.Value, "official_emoji") || !strings.Contains(field.Value, "当前服务器可用: 是") {
				t.Fatalf("unexpected selected field: %q", field.Value)
			}
		}
		if field.Name == "授权能力" {
			foundCaps = true
			if !strings.Contains(field.Value, "discord.read_guild_emojis") || !strings.Contains(field.Value, "worldbook.write") {
				t.Fatalf("unexpected capabilities field: %q", field.Value)
			}
		}
	}
	if !foundCurrent {
		t.Fatal("expected 当前选中 field")
	}
	if !foundCaps {
		t.Fatal("expected 授权能力 field")
	}
}

func TestBuildPluginPanelComponentsDisablesPrivilegedActionsForAdmin(t *testing.T) {
	plugin := pluginhost.InstalledPlugin{
		ID:      "official_persona",
		Name:    "Official Persona Plugin",
		Version: "v0.1.0",
		Enabled: true,
		Manifest: pluginapi.Manifest{
			Commands: []pluginapi.CommandSpec{{Name: "persona", Description: "persona"}},
		},
	}

	components := buildPluginPanelComponents([]pluginhost.InstalledPlugin{plugin}, plugin, true, speechLocation{
		GuildID: "guild-1",
	}, true, false)
	if len(components) != 3 {
		t.Fatalf("expected 3 component rows, got %d", len(components))
	}

	row1, ok := components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("expected actions row, got %T", components[0])
	}
	row2, ok := components[1].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("expected actions row, got %T", components[1])
	}

	install := row1.Components[0].(discordgo.Button)
	refresh := row1.Components[3].(discordgo.Button)
	if install.Disabled {
		t.Fatal("expected install button to stay enabled for admin")
	}
	if refresh.Disabled {
		t.Fatal("expected refresh button to stay enabled")
	}

	enable := row2.Components[0].(discordgo.Button)
	allowHere := row2.Components[2].(discordgo.Button)
	if !enable.Disabled {
		t.Fatal("expected enable button to be disabled for non-super-admin")
	}
	if allowHere.Disabled {
		t.Fatal("expected allow_here button to stay enabled for admin in guild")
	}
}

func TestPluginComponentResponseOpenInstallReturnsModalForAdmin(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "system_prompt": ""
}`)
	handler := newPanelTestHandler(runtimeStore)

	manager, err := pluginhost.NewManager(pluginhost.Config{
		PluginsDir:   filepath.Join(t.TempDir(), "plugins"),
		RuntimeStore: runtimeStore,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handler.SetPluginManager(manager)

	response, err := handler.PluginComponentResponse("admin-1", speechLocation{GuildID: "guild-1"}, discordgo.MessageComponentInteractionData{
		CustomID: pluginActionCustomID(pluginActionOpenInstall, ""),
	})
	if err != nil {
		t.Fatalf("open install modal: %v", err)
	}
	if response.Type != discordgo.InteractionResponseModal {
		t.Fatalf("expected modal response, got %v", response.Type)
	}
	if response.Data == nil || response.Data.CustomID != pluginActionCustomID(pluginModalInstall, "") {
		t.Fatalf("unexpected modal data: %#v", response.Data)
	}
}

func TestSlashCommandsExposeSinglePluginPanelCommand(t *testing.T) {
	commands := slashCommands(nil)
	for _, command := range commands {
		if command == nil || command.Name != "plugin" {
			continue
		}
		if len(command.Options) != 0 {
			t.Fatalf("expected /plugin to have no options, got %d", len(command.Options))
		}
		if !strings.Contains(command.Description, "面板") {
			t.Fatalf("unexpected description: %q", command.Description)
		}
		return
	}
	t.Fatal("expected plugin command")
}
