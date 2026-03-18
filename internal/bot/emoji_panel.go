package bot

import (
	"fmt"
	"strings"

	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

const (
	emojiComponentPrefix          = "emoji:"
	emojiActionRefresh            = "emoji:refresh"
	emojiActionAnalyzeIncremental = "emoji:analyze-incremental"
	emojiActionAnalyzeFull        = "emoji:analyze-full"
	emojiActionViewWorldBook      = "emoji:view-worldbook"
	emojiWorldBookPreviewLimit    = 900
)

func isEmojiInteractionCustomID(customID string) bool {
	return strings.HasPrefix(strings.TrimSpace(customID), emojiComponentPrefix)
}

func isEmojiAsyncInteractionCustomID(customID string) bool {
	switch strings.TrimSpace(customID) {
	case emojiActionAnalyzeIncremental, emojiActionAnalyzeFull:
		return true
	default:
		return false
	}
}

func (h *Handler) EmojiPanelCommandResponse(authorID, guildID, guildName string) (*discordgo.InteractionResponse, error) {
	if strings.TrimSpace(guildID) == "" {
		return simpleEphemeralInteractionResponse("表情管理只能在服务器频道中使用。"), nil
	}

	data, err := h.emojiPanelResponseData(authorID, guildID, guildName, "")
	if err != nil {
		return nil, err
	}
	data.Flags = discordgo.MessageFlagsEphemeral
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	}, nil
}

func (h *Handler) EmojiComponentResponse(authorID, guildID, guildName string, data discordgo.MessageComponentInteractionData) (*discordgo.InteractionResponse, error) {
	if strings.TrimSpace(guildID) == "" {
		return simpleEphemeralInteractionResponse("表情管理只能在服务器频道中使用。"), nil
	}

	switch strings.TrimSpace(data.CustomID) {
	case emojiActionRefresh:
		return h.emojiPanelUpdateResponse(authorID, guildID, guildName, "已刷新表情管理面板。")
	case emojiActionViewWorldBook:
		return h.emojiWorldBookViewResponse(guildID, guildName), nil
	default:
		return h.emojiPanelUpdateResponse(authorID, guildID, guildName, "未知的表情管理操作。")
	}
}

func (h *Handler) emojiPanelUpdateResponse(authorID, guildID, guildName, notice string) (*discordgo.InteractionResponse, error) {
	data, err := h.emojiPanelResponseData(authorID, guildID, guildName, notice)
	if err != nil {
		return nil, err
	}
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: data,
	}, nil
}

func (h *Handler) emojiPanelEdit(authorID, guildID, guildName, notice string) (*discordgo.WebhookEdit, error) {
	data, err := h.emojiPanelResponseData(authorID, guildID, guildName, notice)
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(data.Content)
	components := data.Components
	embeds := data.Embeds
	return &discordgo.WebhookEdit{
		Content:    &content,
		Components: &components,
		Embeds:     &embeds,
	}, nil
}

func (h *Handler) emojiPanelResponseData(authorID, guildID, guildName, notice string) (*discordgo.InteractionResponseData, error) {
	if err := h.ensureRuntimeStore(); err != nil {
		return nil, err
	}

	guildID = strings.TrimSpace(guildID)
	guildName = emojiGuildLabel(guildID, guildName)
	isAdmin := h.runtimeStore.IsAdmin(authorID)
	analyzing := h.isEmojiAnalyzing(guildID)
	profile, _ := h.runtimeStore.GuildEmojiProfile(guildID)
	worldBookText := h.emojiWorldBookText(guildID, profile)

	return &discordgo.InteractionResponseData{
		Content: strings.TrimSpace(notice),
		Embeds: []*discordgo.MessageEmbed{
			h.emojiPanelEmbed(guildID, guildName, profile, worldBookText, notice, isAdmin, analyzing),
		},
		Components: h.emojiPanelComponents(isAdmin, analyzing, strings.TrimSpace(worldBookText) != ""),
	}, nil
}

func (h *Handler) emojiPanelEmbed(guildID, guildName string, profile runtimecfg.GuildEmojiProfile, worldBookText, notice string, isAdmin, analyzing bool) *discordgo.MessageEmbed {
	status := "待分析"
	switch {
	case analyzing:
		status = "分析中"
	case strings.TrimSpace(profile.LastAnalyzedAt) != "":
		status = "已完成"
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "服务器",
			Value:  fmt.Sprintf("%s\nID: `%s`", truncateRunes(guildName, 120), guildID),
			Inline: false,
		},
		{
			Name:   "状态",
			Value:  emojiStatusFieldValue(profile, status, analyzing),
			Inline: false,
		},
		{
			Name:   "世界书预览",
			Value:  emojiWorldBookPreview(worldBookText),
			Inline: false,
		},
	}

	descriptionParts := []string{
		"把服务器自定义表情做成 4x4 图组送去分析，并把总结写入世界书。",
	}
	if !isAdmin {
		descriptionParts = append(descriptionParts, "你当前只有查看权限。")
	}
	if strings.TrimSpace(notice) != "" {
		descriptionParts = append(descriptionParts, "提示: "+strings.TrimSpace(notice))
	}

	return &discordgo.MessageEmbed{
		Title:       "服务器表情管理",
		Description: strings.Join(descriptionParts, "\n"),
		Color:       emojiPanelColor(isAdmin, analyzing, strings.TrimSpace(worldBookText) != ""),
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "增量分析只处理新增表情；若检测到删除，会自动回退为全量重建。",
		},
	}
}

func (h *Handler) emojiPanelComponents(isAdmin, analyzing, hasWorldBook bool) []discordgo.MessageComponent {
	disabledManage := !isAdmin || analyzing
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					CustomID: emojiActionAnalyzeIncremental,
					Label:    "增量分析",
					Style:    discordgo.PrimaryButton,
					Disabled: disabledManage,
				},
				discordgo.Button{
					CustomID: emojiActionAnalyzeFull,
					Label:    "完整重建",
					Style:    discordgo.DangerButton,
					Disabled: disabledManage,
				},
				discordgo.Button{
					CustomID: emojiActionRefresh,
					Label:    "刷新",
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					CustomID: emojiActionViewWorldBook,
					Label:    "查看世界书",
					Style:    discordgo.SuccessButton,
					Disabled: !hasWorldBook,
				},
			},
		},
	}
}

func (h *Handler) emojiWorldBookViewResponse(guildID, guildName string) *discordgo.InteractionResponse {
	profile, _ := h.runtimeStore.GuildEmojiProfile(guildID)
	worldBookText := h.emojiWorldBookText(guildID, profile)
	if strings.TrimSpace(worldBookText) == "" {
		return simpleEphemeralInteractionResponse("当前服务器还没有表情世界书内容。")
	}

	embed := &discordgo.MessageEmbed{
		Title:       "世界书预览",
		Description: "服务器: " + emojiGuildLabel(guildID, guildName) + "\n\n```text\n" + truncateRunes(worldBookText, 3900) + "\n```",
		Color:       0x3B82F6,
	}

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:  discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	}
}

func (h *Handler) emojiWorldBookText(guildID string, profile runtimecfg.GuildEmojiProfile) string {
	entries := h.runtimeStore.WorldBookEntriesForGuild(guildID)
	if entry, ok := entries[emojiWorldBookKey(guildID)]; ok {
		return strings.TrimSpace(entry.Content)
	}
	return strings.TrimSpace(profile.Summary)
}

func emojiStatusFieldValue(profile runtimecfg.GuildEmojiProfile, status string, analyzing bool) string {
	lines := []string{
		"当前状态: " + status,
		fmt.Sprintf("当前记录表情数: %d", profile.EmojiCount),
	}
	if analyzing {
		lines = append(lines, "分析任务已经开始，完成后会自动刷新这个面板。")
	}
	if strings.TrimSpace(profile.LastAnalyzeMode) != "" {
		lines = append(lines, "上次模式: "+profile.LastAnalyzeMode)
	}
	if strings.TrimSpace(profile.LastAnalyzedAt) != "" {
		lines = append(lines, "上次分析时间: "+profile.LastAnalyzedAt)
	}
	if strings.TrimSpace(profile.LastAnalyzedBy) != "" {
		lines = append(lines, "上次分析人: "+profile.LastAnalyzedBy)
	}
	return strings.Join(lines, "\n")
}

func emojiWorldBookPreview(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "当前还没有写入世界书；点击“增量分析”或“完整重建”后会在这里展示预览。"
	}
	return "```text\n" + truncateRunes(content, emojiWorldBookPreviewLimit) + "\n```"
}

func emojiGuildLabel(guildID, guildName string) string {
	guildName = strings.TrimSpace(guildName)
	if guildName != "" {
		return guildName
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return "未知服务器"
	}
	return "Guild " + guildID
}

func emojiPanelColor(isAdmin, analyzing, hasWorldBook bool) int {
	switch {
	case analyzing:
		return 0xF59E0B
	case hasWorldBook:
		return 0x10B981
	case isAdmin:
		return 0x3B82F6
	default:
		return 0x6B7280
	}
}
