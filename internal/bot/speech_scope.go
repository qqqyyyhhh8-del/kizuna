package bot

import (
	"strings"

	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

type speechLocation struct {
	GuildID           string
	ChannelID         string
	ThreadID          string
	OriginalChannelID string
	ChannelResolved   bool
}

func (h *Handler) AllowsSpeechForMessage(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	if h == nil || h.runtimeStore == nil {
		return true
	}

	mode, _, _, _ := h.runtimeStore.SpeechScope()
	if mode == runtimecfg.SpeechModeNone {
		return false
	}

	location := speechLocationForDiscordMessage(s, m)
	return h.allowsSpeechAtLocation(location)
}

func (h *Handler) allowsSpeechAtLocation(location speechLocation) bool {
	if h == nil || h.runtimeStore == nil {
		return true
	}

	if h.runtimeStore.AllowsSpeech(location.GuildID, location.ChannelID, location.ThreadID) {
		return true
	}

	// If we cannot resolve the channel type, fall back to treating the raw
	// channel ID as either a normal channel or a thread so explicit allowlists
	// still work for uncached locations.
	if location.ChannelResolved {
		return false
	}

	rawChannelID := strings.TrimSpace(location.OriginalChannelID)
	if rawChannelID == "" {
		return false
	}

	if rawChannelID != location.ChannelID && h.runtimeStore.AllowsSpeech(location.GuildID, rawChannelID, "") {
		return true
	}
	if rawChannelID != location.ThreadID && h.runtimeStore.AllowsSpeech(location.GuildID, "", rawChannelID) {
		return true
	}
	return false
}

func speechLocationForDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) speechLocation {
	location := speechLocation{}
	if m == nil || m.Message == nil {
		return location
	}
	return speechLocationForChannel(s, m.GuildID, m.ChannelID)
}

func speechLocationForInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) speechLocation {
	if i == nil || i.Interaction == nil {
		return speechLocation{}
	}
	return speechLocationForChannel(s, i.GuildID, i.ChannelID)
}

func resolveDiscordChannel(s *discordgo.Session, channelID string) *discordgo.Channel {
	channelID = strings.TrimSpace(channelID)
	if s == nil || channelID == "" {
		return nil
	}
	if s.State != nil {
		if channel, err := s.State.Channel(channelID); err == nil && channel != nil {
			return channel
		}
	}
	if s.Client == nil || s.Ratelimiter == nil {
		return nil
	}
	channel, err := s.Channel(channelID)
	if err != nil {
		return nil
	}
	return channel
}

func speechLocationForChannel(s *discordgo.Session, guildID, channelID string) speechLocation {
	location := speechLocation{
		GuildID:           strings.TrimSpace(guildID),
		ChannelID:         strings.TrimSpace(channelID),
		OriginalChannelID: strings.TrimSpace(channelID),
	}

	channel := resolveDiscordChannel(s, location.ChannelID)
	if channel == nil {
		return location
	}
	location.ChannelResolved = true
	if location.GuildID == "" {
		location.GuildID = strings.TrimSpace(channel.GuildID)
	}
	if channel.IsThread() {
		location.ThreadID = strings.TrimSpace(channel.ID)
		if parentID := strings.TrimSpace(channel.ParentID); parentID != "" {
			location.ChannelID = parentID
		}
		return location
	}
	location.ChannelID = strings.TrimSpace(channel.ID)
	return location
}
