package bot

import (
	"fmt"
	"strings"
	"time"

	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

const (
	setupComponentPrefix      = "setup:"
	setupActionToggleServer   = "setup:toggle-server"
	setupActionToggleChannel  = "setup:toggle-channel"
	setupActionToggleThread   = "setup:toggle-thread"
	setupActionRefresh        = "setup:refresh"
	setupActionClearAll       = "setup:clear-all"
	setupEmbedColorInfo       = 0x2563EB
	setupEmbedColorSuccess    = 0x059669
	setupListPreviewMaxRunes  = 400
	setupStatusPreviewMaxRune = 1024
)

type setupPanelState struct {
	Location        speechLocation
	Mode            string
	GuildIDs        []string
	ChannelIDs      []string
	ThreadIDs       []string
	ServerAllowed   bool
	LocalAllowed    bool
	LocationAllowed bool
	LocalScope      string
	LocalTargetID   string
}

func isSetupInteractionCustomID(customID string) bool {
	return strings.HasPrefix(strings.TrimSpace(customID), setupComponentPrefix)
}

func (h *Handler) SetupPanelCommandResponse(authorID string, location speechLocation) (*discordgo.InteractionResponse, error) {
	if err := h.ensureRuntimeStore(); err != nil {
		return nil, err
	}
	if !h.runtimeStore.IsAdmin(authorID) {
		return simpleEphemeralInteractionResponse(permissionDenied()), nil
	}
	if strings.TrimSpace(location.GuildID) == "" {
		return simpleEphemeralInteractionResponse("允许发言范围管理只能在服务器频道或子区中使用。"), nil
	}

	data, err := h.setupPanelResponseData(location, "")
	if err != nil {
		return nil, err
	}
	data.Flags = discordgo.MessageFlagsEphemeral
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	}, nil
}

func (h *Handler) SetupComponentResponse(authorID string, location speechLocation, data discordgo.MessageComponentInteractionData) (*discordgo.InteractionResponse, error) {
	if err := h.ensureRuntimeStore(); err != nil {
		return nil, err
	}
	if !h.runtimeStore.IsAdmin(authorID) {
		return simpleEphemeralInteractionResponse(permissionDenied()), nil
	}
	if strings.TrimSpace(location.GuildID) == "" {
		return simpleEphemeralInteractionResponse("允许发言范围管理只能在服务器频道或子区中使用。"), nil
	}

	state := h.setupPanelState(location)

	switch strings.TrimSpace(data.CustomID) {
	case setupActionRefresh:
		return h.setupPanelUpdateResponse(location, "已刷新允许发言范围面板。")
	case setupActionClearAll:
		if err := h.runtimeStore.SetAllowedGuildIDs(nil); err != nil {
			return nil, err
		}
		if err := h.runtimeStore.SetAllowedChannelIDs(nil); err != nil {
			return nil, err
		}
		if err := h.runtimeStore.SetAllowedThreadIDs(nil); err != nil {
			return nil, err
		}
		if err := h.runtimeStore.SetSpeechMode(runtimecfg.SpeechModeAllowlist); err != nil {
			return nil, err
		}
		return h.setupPanelUpdateResponse(location, "已清空所有允许发言范围。机器人现在默认不会在任何服务器、频道或子区发言。")
	case setupActionToggleServer:
		guildID := strings.TrimSpace(location.GuildID)
		if guildID == "" {
			return h.setupPanelUpdateResponse(location, "当前不在服务器上下文中，无法配置服务器放行。")
		}
		if state.ServerAllowed {
			if err := h.runtimeStore.RemoveAllowedGuildID(guildID); err != nil {
				return nil, err
			}
			return h.setupPanelUpdateResponse(location, fmt.Sprintf("已取消放行当前服务器: `%s`", guildID))
		}
		if err := h.runtimeStore.AddAllowedGuildID(guildID); err != nil {
			return nil, err
		}
		return h.setupPanelUpdateResponse(location, fmt.Sprintf("已放行当前服务器: `%s`", guildID))
	case setupActionToggleChannel:
		if state.ServerAllowed {
			return h.setupPanelUpdateResponse(location, "当前服务器已经放行，无需再单独配置当前频道。")
		}
		channelID := strings.TrimSpace(location.ChannelID)
		if channelID == "" {
			return h.setupPanelUpdateResponse(location, "当前不在普通频道中，无法配置频道放行。")
		}
		if state.LocalScope != "channel" {
			return h.setupPanelUpdateResponse(location, "当前不在普通频道中，无法配置频道放行。")
		}
		if state.LocalAllowed {
			if err := h.runtimeStore.RemoveAllowedChannelID(channelID); err != nil {
				return nil, err
			}
			return h.setupPanelUpdateResponse(location, fmt.Sprintf("已取消放行当前频道: `%s`", channelID))
		}
		if err := h.runtimeStore.AddAllowedChannelID(channelID); err != nil {
			return nil, err
		}
		return h.setupPanelUpdateResponse(location, fmt.Sprintf("已放行当前频道: `%s`", channelID))
	case setupActionToggleThread:
		if state.ServerAllowed {
			return h.setupPanelUpdateResponse(location, "当前服务器已经放行，无需再单独配置当前子区。")
		}
		threadID := strings.TrimSpace(location.ThreadID)
		if threadID == "" {
			return h.setupPanelUpdateResponse(location, "当前不在子区/线程/帖子内，无法配置子区放行。")
		}
		if state.LocalScope != "thread" {
			return h.setupPanelUpdateResponse(location, "当前不在子区/线程/帖子内，无法配置子区放行。")
		}
		if state.LocalAllowed {
			if err := h.runtimeStore.RemoveAllowedThreadID(threadID); err != nil {
				return nil, err
			}
			return h.setupPanelUpdateResponse(location, fmt.Sprintf("已取消放行当前子区: `%s`", threadID))
		}
		if err := h.runtimeStore.AddAllowedThreadID(threadID); err != nil {
			return nil, err
		}
		return h.setupPanelUpdateResponse(location, fmt.Sprintf("已放行当前子区: `%s`", threadID))
	default:
		return h.setupPanelUpdateResponse(location, "未知的允许发言范围操作。")
	}
}

func (h *Handler) setupPanelUpdateResponse(location speechLocation, notice string) (*discordgo.InteractionResponse, error) {
	data, err := h.setupPanelResponseData(location, notice)
	if err != nil {
		return nil, err
	}
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: data,
	}, nil
}

func (h *Handler) setupPanelResponseData(location speechLocation, notice string) (*discordgo.InteractionResponseData, error) {
	if err := h.ensureRuntimeStore(); err != nil {
		return nil, err
	}

	state := h.setupPanelState(location)
	return &discordgo.InteractionResponseData{
		Content:    strings.TrimSpace(notice),
		Embeds:     []*discordgo.MessageEmbed{buildSetupPanelEmbed(state, notice)},
		Components: buildSetupPanelComponents(state),
	}, nil
}

func (h *Handler) setupPanelState(location speechLocation) setupPanelState {
	mode, guilds, channels, threads := h.runtimeStore.SpeechScope()
	state := setupPanelState{
		Location:        location,
		Mode:            mode,
		GuildIDs:        guilds,
		ChannelIDs:      channels,
		ThreadIDs:       threads,
		ServerAllowed:   containsTrimmedString(guilds, location.GuildID),
		LocationAllowed: h.allowsSpeechAtLocation(location),
	}

	switch {
	case strings.TrimSpace(location.ThreadID) != "":
		state.LocalScope = "thread"
		state.LocalTargetID = strings.TrimSpace(location.ThreadID)
		state.LocalAllowed = containsTrimmedString(threads, state.LocalTargetID)
	case strings.TrimSpace(location.ChannelID) != "":
		state.LocalScope = "channel"
		state.LocalTargetID = strings.TrimSpace(location.ChannelID)
		state.LocalAllowed = containsTrimmedString(channels, state.LocalTargetID)
	}

	return state
}

func buildSetupPanelEmbed(state setupPanelState, notice string) *discordgo.MessageEmbed {
	descriptionParts := []string{
		"管理机器人允许发言的服务器、频道和子区。绿色按钮表示点击后放行，红色按钮表示点击后取消放行。",
		"如果当前服务器已经放行，则当前频道/子区的单独按钮会被锁定，因为服务器规则已经覆盖了更细粒度的位置。",
	}
	if state.ServerAllowed && state.LocalScope != "" {
		descriptionParts = append(descriptionParts, "当前服务器已经放行，因此当前"+setupLocalScopeName(state.LocalScope)+"按钮不可操作。")
	}
	if strings.TrimSpace(notice) != "" {
		descriptionParts = append(descriptionParts, "提示: "+strings.TrimSpace(notice))
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "当前交互位置",
			Value:  setupLocationLabel(state.Location),
			Inline: false,
		},
		{
			Name:   "当前位置可发言",
			Value:  boolLabel(state.LocationAllowed),
			Inline: true,
		},
		{
			Name:   "当前服务器",
			Value:  setupAllowStatusLabel(state.ServerAllowed),
			Inline: true,
		},
	}
	if state.LocalScope != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "当前" + setupLocalScopeName(state.LocalScope),
			Value:  setupLocalStatusLabel(state),
			Inline: true,
		})
	}
	fields = append(fields,
		&discordgo.MessageEmbedField{
			Name:   "已放行服务器",
			Value:  truncateRunes(renderIDList(state.GuildIDs), setupListPreviewMaxRunes),
			Inline: false,
		},
		&discordgo.MessageEmbedField{
			Name:   "已放行频道",
			Value:  truncateRunes(renderIDList(state.ChannelIDs), setupListPreviewMaxRunes),
			Inline: false,
		},
		&discordgo.MessageEmbedField{
			Name:   "已放行子区",
			Value:  truncateRunes(renderIDList(state.ThreadIDs), setupListPreviewMaxRunes),
			Inline: false,
		},
		&discordgo.MessageEmbedField{
			Name:   "当前白名单状态",
			Value:  truncateRunes(setupSummaryForState(state), setupStatusPreviewMaxRune),
			Inline: false,
		},
	)

	return &discordgo.MessageEmbed{
		Title:       "Speech Scope Setup",
		Description: strings.Join(descriptionParts, "\n\n"),
		Color:       setupPanelColor(state.LocationAllowed),
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "服务器放行优先级最高；清空全部会恢复默认拒绝发言状态。",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

func buildSetupPanelComponents(state setupPanelState) []discordgo.MessageComponent {
	buttons := []discordgo.MessageComponent{
		discordgo.Button{
			CustomID: setupActionToggleServer,
			Label:    setupServerButtonLabel(state.ServerAllowed),
			Style:    setupToggleButtonStyle(state.ServerAllowed, discordgo.SuccessButton),
			Disabled: strings.TrimSpace(state.Location.GuildID) == "",
		},
	}

	if button, ok := buildSetupLocalToggleButton(state); ok {
		buttons = append(buttons, button)
	}

	buttons = append(buttons,
		discordgo.Button{
			CustomID: setupActionRefresh,
			Label:    "刷新",
			Style:    discordgo.SecondaryButton,
		},
		discordgo.Button{
			CustomID: setupActionClearAll,
			Label:    "清空全部",
			Style:    discordgo.DangerButton,
			Disabled: len(state.GuildIDs)+len(state.ChannelIDs)+len(state.ThreadIDs) == 0,
		},
	)

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: buttons},
	}
}

func buildSetupLocalToggleButton(state setupPanelState) (discordgo.Button, bool) {
	switch state.LocalScope {
	case "channel":
		if state.ServerAllowed {
			return discordgo.Button{
				CustomID: setupActionToggleChannel,
				Label:    "服务器已放行",
				Style:    discordgo.SecondaryButton,
				Disabled: true,
			}, true
		}
		return discordgo.Button{
			CustomID: setupActionToggleChannel,
			Label:    setupLocalButtonLabel("频道", state.LocalAllowed),
			Style:    setupToggleButtonStyle(state.LocalAllowed, discordgo.PrimaryButton),
			Disabled: false,
		}, true
	case "thread":
		if state.ServerAllowed {
			return discordgo.Button{
				CustomID: setupActionToggleThread,
				Label:    "服务器已放行",
				Style:    discordgo.SecondaryButton,
				Disabled: true,
			}, true
		}
		return discordgo.Button{
			CustomID: setupActionToggleThread,
			Label:    setupLocalButtonLabel("子区", state.LocalAllowed),
			Style:    setupToggleButtonStyle(state.LocalAllowed, discordgo.PrimaryButton),
			Disabled: false,
		}, true
	default:
		return discordgo.Button{}, false
	}
}

func setupSummaryForState(state setupPanelState) string {
	status := "当前状态：默认不发言（还没有放行任何位置）"
	if state.Mode == runtimecfg.SpeechModeNone {
		status = "当前状态：已完全禁言"
	} else if len(state.GuildIDs)+len(state.ChannelIDs)+len(state.ThreadIDs) > 0 {
		status = "当前状态：仅在以下白名单位置发言"
	}

	return strings.Join([]string{
		status,
		"服务器 ID:",
		renderIDList(state.GuildIDs),
		"频道 ID:",
		renderIDList(state.ChannelIDs),
		"子区 ID:",
		renderIDList(state.ThreadIDs),
	}, "\n")
}

func setupLocationLabel(location speechLocation) string {
	lines := []string{
		"服务器: " + firstNonEmpty(codeValue(strings.TrimSpace(location.GuildID)), "无"),
		"频道: " + firstNonEmpty(codeValue(strings.TrimSpace(location.ChannelID)), "无"),
	}
	if strings.TrimSpace(location.ThreadID) != "" {
		lines = append(lines, "子区: "+codeValue(strings.TrimSpace(location.ThreadID)))
	}
	return strings.Join(lines, "\n")
}

func setupAllowStatusLabel(allowed bool) string {
	if allowed {
		return "已放行"
	}
	return "未放行"
}

func setupLocalStatusLabel(state setupPanelState) string {
	switch {
	case state.LocalScope == "":
		return "当前不在可配置的频道或子区中。"
	case state.ServerAllowed:
		return "已被当前服务器放行覆盖"
	case state.LocalAllowed:
		return "已放行"
	default:
		return "未放行"
	}
}

func setupServerButtonLabel(allowed bool) string {
	if allowed {
		return "取消放行服务器"
	}
	return "放行当前服务器"
}

func setupLocalButtonLabel(scope string, allowed bool) string {
	scope = strings.TrimSpace(scope)
	if allowed {
		return "取消放行当前" + scope
	}
	return "放行当前" + scope
}

func setupLocalScopeName(scope string) string {
	switch strings.TrimSpace(scope) {
	case "thread":
		return "子区"
	case "channel":
		return "频道"
	default:
		return "位置"
	}
}

func setupToggleButtonStyle(allowed bool, defaultStyle discordgo.ButtonStyle) discordgo.ButtonStyle {
	if allowed {
		return discordgo.DangerButton
	}
	return defaultStyle
}

func setupPanelColor(locationAllowed bool) int {
	if locationAllowed {
		return setupEmbedColorSuccess
	}
	return setupEmbedColorInfo
}
