package bot

import (
	"fmt"
	"regexp"
	"strings"

	"kizuna/internal/memory"

	"github.com/bwmarrin/discordgo"
)

const (
	imageKindCustomEmoji = "custom_emoji"
	imageKindAttachment  = "attachment"
	emojiWorldBookPrefix = "emoji:guild:"
)

var customEmojiTokenRegexp = regexp.MustCompile(`<(a?):([A-Za-z0-9_~]+):([0-9]{18,20})>`)

type emojiAsset struct {
	ID       string
	Name     string
	Animated bool
	URL      string
	Syntax   string
}

func emojiWorldBookKey(guildID string) string {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return ""
	}
	return emojiWorldBookPrefix + guildID
}

func emojiSyntax(name, id string, animated bool) string {
	name = strings.TrimSpace(name)
	id = strings.TrimSpace(id)
	if name == "" || id == "" {
		return ""
	}
	if animated {
		return fmt.Sprintf("<a:%s:%s>", name, id)
	}
	return fmt.Sprintf("<:%s:%s>", name, id)
}

func emojiCDNURL(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn.discordapp.com/emojis/%s.png?size=128&quality=lossless", id)
}

func emojiAssetFromDiscord(emoji *discordgo.Emoji) (emojiAsset, bool) {
	if emoji == nil {
		return emojiAsset{}, false
	}
	id := strings.TrimSpace(emoji.ID)
	name := strings.TrimSpace(emoji.Name)
	if id == "" || name == "" {
		return emojiAsset{}, false
	}
	return emojiAsset{
		ID:       id,
		Name:     name,
		Animated: emoji.Animated,
		URL:      emojiCDNURL(id),
		Syntax:   emojiSyntax(name, id, emoji.Animated),
	}, true
}

func collectVisualReferences(message *discordgo.Message, content string) []memory.ImageReference {
	refs := append(customEmojiReferencesFromText(content), imageAttachmentReferences(message)...)
	if len(refs) == 0 {
		return nil
	}
	return refs
}

func discordMessageHasVisualInput(message *discordgo.Message) bool {
	if message == nil {
		return false
	}
	if len(customEmojiReferencesFromText(message.Content)) > 0 {
		return true
	}
	return len(imageAttachmentReferences(message)) > 0
}

func customEmojiReferencesFromText(content string) []memory.ImageReference {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	matches := customEmojiTokenRegexp.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	refs := make([]memory.ImageReference, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		animated := strings.TrimSpace(match[1]) == "a"
		name := strings.TrimSpace(match[2])
		id := strings.TrimSpace(match[3])
		url := emojiCDNURL(id)
		if name == "" || id == "" || url == "" {
			continue
		}
		refs = append(refs, memory.ImageReference{
			Kind:     imageKindCustomEmoji,
			Name:     name,
			EmojiID:  id,
			URL:      url,
			Animated: animated,
		})
	}
	return refs
}

func imageAttachmentReferences(message *discordgo.Message) []memory.ImageReference {
	if message == nil || len(message.Attachments) == 0 {
		return nil
	}

	refs := make([]memory.ImageReference, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		if attachment == nil {
			continue
		}
		url := strings.TrimSpace(attachment.URL)
		contentType := strings.TrimSpace(attachment.ContentType)
		if url == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") && !(attachment.Width > 0 && attachment.Height > 0) {
			continue
		}
		refs = append(refs, memory.ImageReference{
			Kind:        imageKindAttachment,
			Name:        strings.TrimSpace(attachment.Filename),
			URL:         url,
			ContentType: contentType,
		})
	}
	return refs
}
