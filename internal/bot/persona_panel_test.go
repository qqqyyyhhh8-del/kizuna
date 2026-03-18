package bot

import (
	"context"
	"strings"
	"testing"

	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"

	"github.com/bwmarrin/discordgo"
)

func TestPersonaPanelCommandResponseIncludesEmbedAndComponents(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {
    "maid": "你是温柔女仆。",
    "butler": "你是专业管家。"
  },
  "active_persona": "maid",
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

	response, err := handler.PersonaPanelCommandResponse("admin-1")
	if err != nil {
		t.Fatalf("panel response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral panel response, got %#v", response.Data)
	}
	if len(response.Data.Embeds) != 1 {
		t.Fatalf("expected one embed, got %#v", response.Data.Embeds)
	}
	if response.Data.Embeds[0].Title != "Persona Studio" {
		t.Fatalf("unexpected embed title: %q", response.Data.Embeds[0].Title)
	}
	if len(response.Data.Components) < 2 {
		t.Fatalf("expected button row and select row, got %#v", response.Data.Components)
	}
}

func TestPersonaComponentResponseDeleteActivePersona(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {
    "maid": "你是温柔女仆。"
  },
  "active_persona": "maid",
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

	response, err := handler.PersonaComponentResponse("admin-1", discordgo.MessageComponentInteractionData{
		CustomID: personaActionDeleteActive,
	})
	if err != nil {
		t.Fatalf("delete active response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseUpdateMessage {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if runtimeStore.ActivePersonaName() != "" {
		t.Fatalf("expected active persona to be cleared, got %q", runtimeStore.ActivePersonaName())
	}
	if _, ok := runtimeStore.PersonaPrompt("maid"); ok {
		t.Fatal("expected deleted persona to be removed")
	}
	if !strings.Contains(response.Data.Content, "已删除人设") {
		t.Fatalf("unexpected response content: %q", response.Data.Content)
	}
}

func TestPersonaComponentResponseOpenEditCurrentModal(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {
    "maid": "你是温柔女仆。"
  },
  "active_persona": "maid",
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

	response, err := handler.PersonaComponentResponse("admin-1", discordgo.MessageComponentInteractionData{
		CustomID: personaActionOpenEditActive,
	})
	if err != nil {
		t.Fatalf("open edit modal response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseModal {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.CustomID != personaModalEditActive {
		t.Fatalf("unexpected modal data: %#v", response.Data)
	}
	row, ok := response.Data.Components[0].(discordgo.ActionsRow)
	if !ok {
		t.Fatalf("expected actions row, got %#v", response.Data.Components[0])
	}
	input, ok := row.Components[0].(discordgo.TextInput)
	if !ok {
		t.Fatalf("expected text input, got %#v", row.Components[0])
	}
	if input.Value != "你是温柔女仆。" {
		t.Fatalf("unexpected modal prefill: %q", input.Value)
	}
}

func TestPersonaModalResponseUpsertsAndActivatesPersona(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
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

	response, err := handler.PersonaModalResponse("admin-1", discordgo.ModalSubmitInteractionData{
		CustomID: personaModalUpsert,
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: personaModalFieldName, Value: "maid"},
				},
			},
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: personaModalFieldPrompt, Value: "你是温柔女仆。"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("modal response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral response, got %#v", response.Data)
	}
	if runtimeStore.ActivePersonaName() != "maid" {
		t.Fatalf("expected active persona maid, got %q", runtimeStore.ActivePersonaName())
	}
	prompt, ok := runtimeStore.PersonaPrompt("maid")
	if !ok || prompt != "你是温柔女仆。" {
		t.Fatalf("unexpected stored prompt: %q, ok=%v", prompt, ok)
	}
}

func TestPersonaModalResponseEditsCurrentPersona(t *testing.T) {
	runtimeStore := newTestRuntimeStore(t, `{
  "super_admin_ids": ["owner-1"],
  "admin_ids": ["admin-1"],
  "personas": {
    "maid": "旧 Prompt"
  },
  "active_persona": "maid",
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

	response, err := handler.PersonaModalResponse("admin-1", discordgo.ModalSubmitInteractionData{
		CustomID: personaModalEditActive,
		Components: []discordgo.MessageComponent{
			&discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: personaModalFieldEditPrompt, Value: "新 Prompt"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("edit current modal response: %v", err)
	}
	if response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	prompt, ok := runtimeStore.PersonaPrompt("maid")
	if !ok || prompt != "新 Prompt" {
		t.Fatalf("unexpected updated prompt: %q, ok=%v", prompt, ok)
	}
	if runtimeStore.ActivePersonaName() != "maid" {
		t.Fatalf("expected active persona maid, got %q", runtimeStore.ActivePersonaName())
	}
	if !strings.Contains(response.Data.Content, "已更新当前人设") {
		t.Fatalf("unexpected response content: %q", response.Data.Content)
	}
}
