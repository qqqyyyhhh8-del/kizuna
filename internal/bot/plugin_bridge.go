package bot

import (
	"strings"
	"time"

	"kizuna/internal/memory"
	"kizuna/pkg/pluginapi"

	"github.com/bwmarrin/discordgo"
)

func pluginMessageContextFromRecord(record memory.MessageRecord) pluginapi.MessageContext {
	context := pluginapi.MessageContext{
		Guild: pluginapi.GuildInfo{
			ID: strings.TrimSpace(record.GuildID),
		},
		Content: strings.TrimSpace(record.Content),
		Time:    record.Time.Format(time.RFC3339),
		Author:  pluginUserInfoFromAuthor(record.Author),
	}

	if record.ReplyTo != nil {
		context.ReplyTo = &pluginapi.ReplyInfo{
			MessageID: strings.TrimSpace(record.ReplyTo.MessageID),
			Role:      strings.TrimSpace(record.ReplyTo.Role),
			Content:   strings.TrimSpace(record.ReplyTo.Content),
			Time:      record.ReplyTo.Time.Format(time.RFC3339),
			Author:    pluginUserInfoFromAuthor(record.ReplyTo.Author),
		}
	}
	if len(record.Images) > 0 {
		images := make([]pluginapi.ImageReference, 0, len(record.Images))
		for _, image := range record.Images {
			images = append(images, pluginapi.ImageReference{
				Kind:        strings.TrimSpace(image.Kind),
				Name:        strings.TrimSpace(image.Name),
				EmojiID:     strings.TrimSpace(image.EmojiID),
				URL:         strings.TrimSpace(image.URL),
				Animated:    image.Animated,
				ContentType: strings.TrimSpace(image.ContentType),
			})
		}
		context.Images = images
	}
	return context
}

func messageRecordFromPluginMessage(message pluginapi.MessageContext) memory.MessageRecord {
	record := memory.MessageRecord{
		Role:    "user",
		GuildID: strings.TrimSpace(message.Guild.ID),
		Content: strings.TrimSpace(message.Content),
		Author: memory.MessageAuthor{
			UserID:      strings.TrimSpace(message.Author.ID),
			Username:    strings.TrimSpace(message.Author.Username),
			GlobalName:  strings.TrimSpace(message.Author.GlobalName),
			Nick:        strings.TrimSpace(message.Author.Nick),
			DisplayName: strings.TrimSpace(message.Author.DisplayName),
		},
	}
	if parsedTime, err := time.Parse(time.RFC3339, strings.TrimSpace(message.Time)); err == nil {
		record.Time = parsedTime
	}
	if message.ReplyTo != nil {
		reply := &memory.ReplyRecord{
			MessageID: strings.TrimSpace(message.ReplyTo.MessageID),
			Role:      strings.TrimSpace(message.ReplyTo.Role),
			Content:   strings.TrimSpace(message.ReplyTo.Content),
			Author: memory.MessageAuthor{
				UserID:      strings.TrimSpace(message.ReplyTo.Author.ID),
				Username:    strings.TrimSpace(message.ReplyTo.Author.Username),
				GlobalName:  strings.TrimSpace(message.ReplyTo.Author.GlobalName),
				Nick:        strings.TrimSpace(message.ReplyTo.Author.Nick),
				DisplayName: strings.TrimSpace(message.ReplyTo.Author.DisplayName),
			},
		}
		if parsedTime, err := time.Parse(time.RFC3339, strings.TrimSpace(message.ReplyTo.Time)); err == nil {
			reply.Time = parsedTime
		}
		record.ReplyTo = reply
	}
	if len(message.Images) > 0 {
		record.Images = make([]memory.ImageReference, 0, len(message.Images))
		for _, image := range message.Images {
			record.Images = append(record.Images, memory.ImageReference{
				Kind:        strings.TrimSpace(image.Kind),
				Name:        strings.TrimSpace(image.Name),
				EmojiID:     strings.TrimSpace(image.EmojiID),
				URL:         strings.TrimSpace(image.URL),
				Animated:    image.Animated,
				ContentType: strings.TrimSpace(image.ContentType),
			})
		}
	}
	return record
}

func pluginMessageEventFromDiscord(s *discordgo.Session, m *discordgo.MessageCreate, content string) pluginapi.MessageEvent {
	event := pluginapi.MessageEvent{}
	if m == nil || m.Message == nil {
		return event
	}

	location := speechLocationForDiscordMessage(s, m)
	channelName := ""
	threadName := ""
	if channel := resolveDiscordChannel(s, m.ChannelID); channel != nil {
		channelName = strings.TrimSpace(channel.Name)
		if channel.IsThread() {
			threadName = strings.TrimSpace(channel.Name)
		}
	}

	event.Message = pluginapi.MessageContext{
		MessageID: strings.TrimSpace(m.ID),
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
		Content: strings.TrimSpace(content),
		Time:    m.Timestamp.Format(time.RFC3339),
		Author:  pluginUserInfoFromAuthor(authorFromDiscord(m.Author, m.Member)),
	}
	event.Message.MentionedBot = mentionsUser(m.Mentions, botUserIDFromSession(s))
	event.Message.RepliedToBot = repliesToUser(m.Message, botUserIDFromSession(s))

	if reply := replyRecordFromDiscord(m.ReferencedMessage, botUserIDFromSession(s)); reply != nil {
		event.Message.ReplyTo = &pluginapi.ReplyInfo{
			MessageID: strings.TrimSpace(reply.MessageID),
			Role:      strings.TrimSpace(reply.Role),
			Content:   strings.TrimSpace(reply.Content),
			Time:      reply.Time.Format(time.RFC3339),
			Author:    pluginUserInfoFromAuthor(reply.Author),
		}
	}
	visuals := collectVisualReferences(m.Message, content)
	if len(visuals) > 0 {
		event.Message.Images = make([]pluginapi.ImageReference, 0, len(visuals))
		for _, image := range visuals {
			event.Message.Images = append(event.Message.Images, pluginapi.ImageReference{
				Kind:        strings.TrimSpace(image.Kind),
				Name:        strings.TrimSpace(image.Name),
				EmojiID:     strings.TrimSpace(image.EmojiID),
				URL:         strings.TrimSpace(image.URL),
				Animated:    image.Animated,
				ContentType: strings.TrimSpace(image.ContentType),
			})
		}
	}
	return event
}

func pluginUserInfoFromAuthor(author memory.MessageAuthor) pluginapi.UserInfo {
	return pluginapi.UserInfo{
		ID:          strings.TrimSpace(author.UserID),
		Username:    strings.TrimSpace(author.Username),
		GlobalName:  strings.TrimSpace(author.GlobalName),
		Nick:        strings.TrimSpace(author.Nick),
		DisplayName: strings.TrimSpace(author.DisplayName),
	}
}
