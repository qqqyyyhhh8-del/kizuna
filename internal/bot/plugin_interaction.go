package bot

import (
	"strings"

	"kizuna/pkg/pluginapi"

	"github.com/bwmarrin/discordgo"
)

func pluginSlashCommandRequestFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) pluginapi.SlashCommandRequest {
	location := speechLocationForInteraction(s, i)
	channelName := ""
	threadName := ""
	if channel := resolveDiscordChannel(s, i.ChannelID); channel != nil {
		channelName = strings.TrimSpace(channel.Name)
		if channel.IsThread() {
			threadName = strings.TrimSpace(channel.Name)
		}
	}

	return pluginapi.SlashCommandRequest{
		CommandName: data.Name,
		Guild: pluginapi.GuildInfo{
			ID:   strings.TrimSpace(location.GuildID),
			Name: guildNameFromSession(s, location.GuildID),
		},
		Channel: pluginapi.ChannelInfo{
			ID:         strings.TrimSpace(location.ChannelID),
			Name:       channelName,
			ThreadID:   strings.TrimSpace(location.ThreadID),
			ThreadName: threadName,
		},
		User:    pluginUserInfoFromInteraction(i),
		Options: pluginSlashOptionValues(data.Options),
	}
}

func pluginComponentRequestFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.MessageComponentInteractionData) pluginapi.ComponentRequest {
	location := speechLocationForInteraction(s, i)
	channelName := ""
	threadName := ""
	if channel := resolveDiscordChannel(s, i.ChannelID); channel != nil {
		channelName = strings.TrimSpace(channel.Name)
		if channel.IsThread() {
			threadName = strings.TrimSpace(channel.Name)
		}
	}

	return pluginapi.ComponentRequest{
		CustomID: strings.TrimSpace(data.CustomID),
		Guild: pluginapi.GuildInfo{
			ID:   strings.TrimSpace(location.GuildID),
			Name: guildNameFromSession(s, location.GuildID),
		},
		Channel: pluginapi.ChannelInfo{
			ID:         strings.TrimSpace(location.ChannelID),
			Name:       channelName,
			ThreadID:   strings.TrimSpace(location.ThreadID),
			ThreadName: threadName,
		},
		User:   pluginUserInfoFromInteraction(i),
		Values: append([]string(nil), data.Values...),
	}
}

func pluginModalRequestFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ModalSubmitInteractionData) pluginapi.ModalRequest {
	location := speechLocationForInteraction(s, i)
	channelName := ""
	threadName := ""
	if channel := resolveDiscordChannel(s, i.ChannelID); channel != nil {
		channelName = strings.TrimSpace(channel.Name)
		if channel.IsThread() {
			threadName = strings.TrimSpace(channel.Name)
		}
	}

	return pluginapi.ModalRequest{
		CustomID: strings.TrimSpace(data.CustomID),
		Guild: pluginapi.GuildInfo{
			ID:   strings.TrimSpace(location.GuildID),
			Name: guildNameFromSession(s, location.GuildID),
		},
		Channel: pluginapi.ChannelInfo{
			ID:         strings.TrimSpace(location.ChannelID),
			Name:       channelName,
			ThreadID:   strings.TrimSpace(location.ThreadID),
			ThreadName: threadName,
		},
		User:   pluginUserInfoFromInteraction(i),
		Fields: pluginModalFields(data.Components),
	}
}

func pluginUserInfoFromInteraction(i *discordgo.InteractionCreate) pluginapi.UserInfo {
	if i == nil {
		return pluginapi.UserInfo{}
	}
	if i.Member != nil && i.Member.User != nil {
		return pluginUserInfoFromAuthor(authorFromDiscord(i.Member.User, i.Member))
	}
	if i.User != nil {
		return pluginUserInfoFromAuthor(authorFromDiscord(i.User, nil))
	}
	return pluginapi.UserInfo{}
}

func pluginSlashOptionValues(options []*discordgo.ApplicationCommandInteractionDataOption) []pluginapi.CommandOptionValue {
	values := make([]pluginapi.CommandOptionValue, 0, len(options))
	for _, option := range options {
		if option == nil {
			continue
		}
		value := pluginapi.CommandOptionValue{
			Name: option.Name,
			Type: pluginCommandOptionTypeFromDiscord(option.Type),
		}
		switch option.Type {
		case discordgo.ApplicationCommandOptionString:
			value.StringValue = strings.TrimSpace(option.StringValue())
		case discordgo.ApplicationCommandOptionInteger:
			value.IntegerValue = option.IntValue()
		case discordgo.ApplicationCommandOptionNumber:
			value.NumberValue = option.FloatValue()
		case discordgo.ApplicationCommandOptionBoolean:
			value.BooleanValue = option.BoolValue()
		case discordgo.ApplicationCommandOptionUser:
			user := option.UserValue(nil)
			if user != nil {
				value.UserID = strings.TrimSpace(user.ID)
			}
		case discordgo.ApplicationCommandOptionChannel:
			channel := option.ChannelValue(nil)
			if channel != nil {
				value.ChannelID = strings.TrimSpace(channel.ID)
			}
		case discordgo.ApplicationCommandOptionRole:
			role := option.RoleValue(nil, "")
			if role != nil {
				value.RoleID = strings.TrimSpace(role.ID)
			}
		case discordgo.ApplicationCommandOptionSubCommand:
			value.Options = pluginSlashOptionValues(option.Options)
		}
		values = append(values, value)
	}
	return values
}

func pluginModalFields(components []discordgo.MessageComponent) map[string]string {
	fields := map[string]string{}
	for _, component := range components {
		var children []discordgo.MessageComponent
		switch row := component.(type) {
		case discordgo.ActionsRow:
			children = row.Components
		case *discordgo.ActionsRow:
			if row != nil {
				children = row.Components
			}
		}
		for _, child := range children {
			switch input := child.(type) {
			case discordgo.TextInput:
				fields[strings.TrimSpace(input.CustomID)] = strings.TrimSpace(input.Value)
			case *discordgo.TextInput:
				if input != nil {
					fields[strings.TrimSpace(input.CustomID)] = strings.TrimSpace(input.Value)
				}
			}
		}
	}
	return fields
}

func pluginCommandOptionTypeFromDiscord(value discordgo.ApplicationCommandOptionType) pluginapi.CommandOptionType {
	switch value {
	case discordgo.ApplicationCommandOptionInteger:
		return pluginapi.CommandOptionTypeInteger
	case discordgo.ApplicationCommandOptionNumber:
		return pluginapi.CommandOptionTypeNumber
	case discordgo.ApplicationCommandOptionBoolean:
		return pluginapi.CommandOptionTypeBoolean
	case discordgo.ApplicationCommandOptionUser:
		return pluginapi.CommandOptionTypeUser
	case discordgo.ApplicationCommandOptionChannel:
		return pluginapi.CommandOptionTypeChannel
	case discordgo.ApplicationCommandOptionRole:
		return pluginapi.CommandOptionTypeRole
	case discordgo.ApplicationCommandOptionSubCommand:
		return pluginapi.CommandOptionTypeSubcommand
	default:
		return pluginapi.CommandOptionTypeString
	}
}

func discordInteractionResponseFromPlugin(response *pluginapi.InteractionResponse) *discordgo.InteractionResponse {
	if response == nil {
		return simpleEphemeralInteractionResponse("插件没有返回可用响应。")
	}
	switch response.Type {
	case pluginapi.InteractionResponseTypeUpdate:
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: discordInteractionResponseData(response.Message),
		}
	case pluginapi.InteractionResponseTypeModal:
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: discordModalResponseData(response.Modal),
		}
	case pluginapi.InteractionResponseTypeDeferUpdate:
		return deferredMessageUpdateResponse()
	default:
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: discordInteractionResponseData(response.Message),
		}
	}
}

func discordInteractionResponseData(message *pluginapi.InteractionMessage) *discordgo.InteractionResponseData {
	if message == nil {
		return &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "插件没有返回消息内容。",
		}
	}
	data := &discordgo.InteractionResponseData{
		Content: strings.TrimSpace(message.Content),
		Embeds:  discordEmbedsFromPlugin(message.Embeds),
	}
	if message.Ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	if len(message.Components) > 0 {
		data.Components = discordActionRowsFromPlugin(message.Components)
	}
	return data
}

func discordModalResponseData(modal *pluginapi.ModalResponse) *discordgo.InteractionResponseData {
	if modal == nil {
		return &discordgo.InteractionResponseData{
			CustomID: "plugin:empty-modal",
			Title:    "Plugin Modal",
		}
	}
	components := make([]discordgo.MessageComponent, 0, len(modal.Fields))
	for _, field := range modal.Fields {
		style := discordgo.TextInputShort
		if strings.EqualFold(strings.TrimSpace(field.Style), "paragraph") {
			style = discordgo.TextInputParagraph
		}
		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.TextInput{
					CustomID:    strings.TrimSpace(field.CustomID),
					Label:       strings.TrimSpace(field.Label),
					Style:       style,
					Placeholder: strings.TrimSpace(field.Placeholder),
					Value:       strings.TrimSpace(field.Value),
					Required:    field.Required,
					MinLength:   field.MinLength,
					MaxLength:   field.MaxLength,
				},
			},
		})
	}
	return &discordgo.InteractionResponseData{
		CustomID:   strings.TrimSpace(modal.CustomID),
		Title:      strings.TrimSpace(modal.Title),
		Components: components,
	}
}

func discordEmbedsFromPlugin(embeds []pluginapi.Embed) []*discordgo.MessageEmbed {
	converted := make([]*discordgo.MessageEmbed, 0, len(embeds))
	for _, embed := range embeds {
		fields := make([]*discordgo.MessageEmbedField, 0, len(embed.Fields))
		for _, field := range embed.Fields {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   strings.TrimSpace(field.Name),
				Value:  strings.TrimSpace(field.Value),
				Inline: field.Inline,
			})
		}
		converted = append(converted, &discordgo.MessageEmbed{
			Title:       strings.TrimSpace(embed.Title),
			Description: strings.TrimSpace(embed.Description),
			Color:       embed.Color,
			Fields:      fields,
			Footer: &discordgo.MessageEmbedFooter{
				Text: strings.TrimSpace(embed.Footer),
			},
		})
	}
	return converted
}

func discordActionRowsFromPlugin(rows []pluginapi.ActionRow) []discordgo.MessageComponent {
	converted := make([]discordgo.MessageComponent, 0, len(rows))
	for _, row := range rows {
		buttons := make([]discordgo.MessageComponent, 0, len(row.Buttons))
		for _, button := range row.Buttons {
			buttons = append(buttons, discordgo.Button{
				CustomID: strings.TrimSpace(button.CustomID),
				Label:    strings.TrimSpace(button.Label),
				Style:    discordButtonStyle(button.Style),
				Disabled: button.Disabled,
			})
		}
		for _, menu := range row.SelectMenus {
			options := make([]discordgo.SelectMenuOption, 0, len(menu.Options))
			for _, option := range menu.Options {
				options = append(options, discordgo.SelectMenuOption{
					Label:       strings.TrimSpace(option.Label),
					Value:       strings.TrimSpace(option.Value),
					Description: strings.TrimSpace(option.Description),
					Default:     option.Default,
				})
			}
			minValues := max(menu.MinValues, 0)
			maxValues := menu.MaxValues
			if maxValues <= 0 {
				maxValues = 1
			}
			buttons = append(buttons, discordgo.SelectMenu{
				CustomID:    strings.TrimSpace(menu.CustomID),
				Placeholder: strings.TrimSpace(menu.Placeholder),
				MinValues:   &minValues,
				MaxValues:   maxValues,
				Options:     options,
				Disabled:    menu.Disabled,
			})
		}
		converted = append(converted, discordgo.ActionsRow{
			Components: buttons,
		})
	}
	return converted
}

func discordButtonStyle(style string) discordgo.ButtonStyle {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "secondary":
		return discordgo.SecondaryButton
	case "success":
		return discordgo.SuccessButton
	case "danger":
		return discordgo.DangerButton
	default:
		return discordgo.PrimaryButton
	}
}

func denyPluginErrorInteractionResponse(content string) *pluginapi.InteractionResponse {
	return &pluginapi.InteractionResponse{
		Type: pluginapi.InteractionResponseTypeMessage,
		Message: &pluginapi.InteractionMessage{
			Content:   strings.TrimSpace(content),
			Ephemeral: true,
		},
	}
}
