package main

import (
	"context"
	"log"
	"strings"

	"kizuna/pkg/pluginapi"
)

const styleNoteStorageKey = "style_note"

type styleNotePlugin struct {
	pluginapi.BasePlugin
}

func (p *styleNotePlugin) OnSlashCommand(ctx context.Context, host *pluginapi.HostClient, request pluginapi.SlashCommandRequest) (*pluginapi.InteractionResponse, error) {
	switch strings.TrimSpace(request.CommandName) {
	case "style-note-set":
		note := strings.TrimSpace(findStringOption(request.Options, "note"))
		if note == "" {
			return ephemeralMessage("请输入要保存的 note。"), nil
		}
		if err := host.StorageSet(ctx, styleNoteStorageKey, note); err != nil {
			return nil, err
		}
		return ephemeralMessage("已保存 style note。"), nil
	case "style-note-show":
		note, err := loadStyleNote(ctx, host)
		if err != nil {
			return nil, err
		}
		if note == "" {
			return ephemeralMessage("当前没有保存 style note。"), nil
		}
		return &pluginapi.InteractionResponse{
			Type: pluginapi.InteractionResponseTypeMessage,
			Message: &pluginapi.InteractionMessage{
				Ephemeral: true,
				Embeds: []pluginapi.Embed{
					{
						Title:       "Current Style Note",
						Description: note,
						Color:       0x2563EB,
					},
				},
			},
		}, nil
	case "style-note-clear":
		if err := host.StorageSet(ctx, styleNoteStorageKey, ""); err != nil {
			return nil, err
		}
		return ephemeralMessage("已清空 style note。"), nil
	default:
		return ephemeralMessage("未知命令。"), nil
	}
}

func (p *styleNotePlugin) OnPromptBuild(ctx context.Context, host *pluginapi.HostClient, request pluginapi.PromptBuildRequest) (*pluginapi.PromptBuildResponse, error) {
	note, err := loadStyleNote(ctx, host)
	if err != nil {
		return nil, err
	}
	if note == "" {
		return nil, nil
	}
	return &pluginapi.PromptBuildResponse{
		Blocks: []pluginapi.PromptBlock{
			{
				Role:    "system",
				Content: "Style Note Plugin:\n" + note,
			},
		},
	}, nil
}

func loadStyleNote(ctx context.Context, host *pluginapi.HostClient) (string, error) {
	var note string
	_, err := host.StorageGet(ctx, styleNoteStorageKey, &note)
	return strings.TrimSpace(note), err
}

func findStringOption(options []pluginapi.CommandOptionValue, name string) string {
	for _, option := range options {
		if strings.TrimSpace(option.Name) == strings.TrimSpace(name) {
			return strings.TrimSpace(option.StringValue)
		}
		if nested := findStringOption(option.Options, name); nested != "" {
			return nested
		}
	}
	return ""
}

func ephemeralMessage(content string) *pluginapi.InteractionResponse {
	return &pluginapi.InteractionResponse{
		Type: pluginapi.InteractionResponseTypeMessage,
		Message: &pluginapi.InteractionMessage{
			Content:   strings.TrimSpace(content),
			Ephemeral: true,
		},
	}
}

func main() {
	manifest, err := pluginapi.ReadManifest("plugin.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := pluginapi.Serve(manifest, &styleNotePlugin{}); err != nil {
		log.Fatal(err)
	}
}
