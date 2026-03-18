package bot

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kizuna/internal/memory"

	"github.com/bwmarrin/discordgo"
)

func TestPromptContentForMessageIgnoresUnmentionedGuildMessages(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "hello there",
		},
	}

	content, ok := promptContentForMessage(session, message)
	if ok {
		t.Fatalf("expected guild message without mention to be ignored, got %q", content)
	}
}

func TestPromptContentForMessageTrimsBotMention(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID:  "guild-1",
			Content:  "<@bot-1>   help me",
			Mentions: []*discordgo.User{{ID: "bot-1"}},
		},
	}

	content, ok := promptContentForMessage(session, message)
	if !ok {
		t.Fatal("expected mentioned guild message to be accepted")
	}
	if content != "help me" {
		t.Fatalf("expected trimmed content, got %q", content)
	}
}

func TestPromptContentForMessageAcceptsReplyToBot(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "follow up question",
			ReferencedMessage: &discordgo.Message{
				Author: &discordgo.User{ID: "bot-1"},
			},
		},
	}

	content, ok := promptContentForMessage(session, message)
	if !ok {
		t.Fatal("expected reply to bot to be accepted")
	}
	if content != "follow up question" {
		t.Fatalf("expected original content, got %q", content)
	}
}

func TestProactivePromptContentForMessageAcceptsGuildMessageWithoutMention(t *testing.T) {
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "普通群聊消息",
		},
	}

	content, ok := proactivePromptContentForMessage(message)
	if !ok {
		t.Fatal("expected proactive prompt content to accept guild message")
	}
	if content != "普通群聊消息" {
		t.Fatalf("unexpected proactive content: %q", content)
	}
}

func TestProactivePromptContentForMessageRejectsDM(t *testing.T) {
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			Content: "dm message",
		},
	}

	if _, ok := proactivePromptContentForMessage(message); ok {
		t.Fatal("expected proactive prompt content to reject dm")
	}
}

func TestPromptContentForMessageIgnoresReplyToOtherUser(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "follow up question",
			ReferencedMessage: &discordgo.Message{
				Author: &discordgo.User{ID: "user-2"},
			},
		},
	}

	content, ok := promptContentForMessage(session, message)
	if ok {
		t.Fatalf("expected reply to other user to be ignored, got %q", content)
	}
}

func TestStartTypingLoopSendsImmediatelyAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	sendCh := make(chan struct{}, 8)
	stopTyping := startTypingLoop(ctx, "channel-1", func(channelID string) error {
		if channelID != "channel-1" {
			t.Fatalf("unexpected channel id: %s", channelID)
		}
		calls.Add(1)
		sendCh <- struct{}{}
		return nil
	}, 10*time.Millisecond)

	waitForTypingCall(t, sendCh)
	waitForTypingCall(t, sendCh)

	stopTyping()
	callCount := calls.Load()

	time.Sleep(30 * time.Millisecond)
	if calls.Load() != callCount {
		t.Fatalf("expected typing loop to stop at %d calls, got %d", callCount, calls.Load())
	}
}

func TestMessageRecordForDiscordMessageIncludesUserMetadata(t *testing.T) {
	timestamp := time.Date(2026, 3, 18, 12, 34, 56, 0, time.UTC)
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg-1",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
			Timestamp: timestamp,
			Author: &discordgo.User{
				ID:         "user-1",
				Username:   "alice",
				GlobalName: "Alice Global",
			},
			Member: &discordgo.Member{
				Nick: "Alice Nick",
			},
		},
	}

	record := messageRecordForDiscordMessage(message, "hello", "bot-1")

	expected := memory.MessageRecord{
		Role:    "user",
		GuildID: "guild-1",
		Content: "hello",
		Time:    timestamp,
		Author: memory.MessageAuthor{
			UserID:      "user-1",
			Username:    "alice",
			GlobalName:  "Alice Global",
			Nick:        "Alice Nick",
			DisplayName: "Alice Nick",
		},
	}
	if !reflect.DeepEqual(record, expected) {
		t.Fatalf("unexpected message record: %#v", record)
	}
}

func TestMessageRecordForDiscordMessageIncludesReferencedMessage(t *testing.T) {
	timestamp := time.Date(2026, 3, 18, 12, 34, 56, 0, time.UTC)
	replyTimestamp := time.Date(2026, 3, 18, 12, 30, 0, 0, time.UTC)
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg-1",
			ChannelID: "channel-1",
			Timestamp: timestamp,
			Author: &discordgo.User{
				ID:         "user-1",
				Username:   "alice",
				GlobalName: "Alice Global",
			},
			ReferencedMessage: &discordgo.Message{
				ID:        "msg-0",
				ChannelID: "channel-1",
				Timestamp: replyTimestamp,
				Content:   "earlier answer",
				Author: &discordgo.User{
					ID:         "bot-1",
					Username:   "helperbot",
					GlobalName: "Helper Bot",
				},
			},
		},
	}

	record := messageRecordForDiscordMessage(message, "follow up", "bot-1")
	if record.ReplyTo == nil {
		t.Fatal("expected reply context to be present")
	}
	if record.ReplyTo.Role != "assistant" {
		t.Fatalf("expected referenced role assistant, got %q", record.ReplyTo.Role)
	}
	if record.ReplyTo.MessageID != "msg-0" || record.ReplyTo.Content != "earlier answer" {
		t.Fatalf("unexpected reply context: %#v", record.ReplyTo)
	}
	if record.ReplyTo.Author.UserID != "bot-1" || record.ReplyTo.Author.Username != "helperbot" {
		t.Fatalf("unexpected reply author: %#v", record.ReplyTo.Author)
	}
}

func TestBuildReplyMessageSendUsesReplyReferenceWithoutMention(t *testing.T) {
	trigger := &discordgo.Message{
		ID:        "msg-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
	}

	send := buildReplyMessageSend(trigger, " hi ")

	if send.Content != "hi" {
		t.Fatalf("expected trimmed content, got %q", send.Content)
	}
	if send.Reference == nil || send.Reference.MessageID != "msg-1" {
		t.Fatalf("expected message reference, got %#v", send.Reference)
	}
	if send.AllowedMentions == nil {
		t.Fatal("expected allowed mentions to be set")
	}
	if send.AllowedMentions.RepliedUser {
		t.Fatal("expected replied user mention to be disabled")
	}
	if len(send.AllowedMentions.Parse) != 0 {
		t.Fatalf("expected no parsed mentions, got %#v", send.AllowedMentions.Parse)
	}
}

func TestBuildPlainMessageSendDisablesMentions(t *testing.T) {
	send := buildPlainMessageSend(" hi ")

	if send.Content != "hi" {
		t.Fatalf("expected trimmed content, got %q", send.Content)
	}
	if send.Reference != nil {
		t.Fatalf("expected no message reference, got %#v", send.Reference)
	}
	if send.AllowedMentions == nil {
		t.Fatal("expected allowed mentions to be set")
	}
	if send.AllowedMentions.RepliedUser {
		t.Fatal("expected replied user mention to be disabled")
	}
	if len(send.AllowedMentions.Parse) != 0 {
		t.Fatalf("expected no parsed mentions, got %#v", send.AllowedMentions.Parse)
	}
}

func TestSendMessageReplyFallsBackWithoutReference(t *testing.T) {
	var requestBodies []string
	session := &discordgo.Session{
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				requestBodies = append(requestBodies, string(body))

				if len(requestBodies) == 1 {
					if !strings.Contains(requestBodies[0], "\"message_reference\"") {
						t.Fatalf("expected first request to include message reference, got %s", requestBodies[0])
					}
					return jsonResponse(http.StatusForbidden, `{"message":"Missing Permissions","code":50013}`), nil
				}

				if strings.Contains(requestBodies[len(requestBodies)-1], "\"message_reference\"") {
					t.Fatalf("expected fallback request to omit message reference, got %s", requestBodies[len(requestBodies)-1])
				}
				return jsonResponse(http.StatusOK, `{"id":"msg-2","channel_id":"channel-1","content":"hi"}`), nil
			}),
		},
		Ratelimiter: discordgo.NewRatelimiter(),
		Token:       "Bot test-token",
	}

	message, err := sendMessageReply(session, &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg-1",
			ChannelID: "channel-1",
			GuildID:   "guild-1",
		},
	}, " hi ")
	if err != nil {
		t.Fatalf("send message reply: %v", err)
	}
	if message == nil || message.ID != "msg-2" {
		t.Fatalf("unexpected response message: %#v", message)
	}
	if len(requestBodies) != 2 {
		t.Fatalf("expected two send attempts, got %d", len(requestBodies))
	}
}

func TestPromptContentForMessageAcceptsImageOnlyReply(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "bot-1"},
			},
		},
	}
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "",
			Attachments: []*discordgo.MessageAttachment{
				{
					URL:         "https://example.com/image.png",
					Filename:    "image.png",
					ContentType: "image/png",
					Width:       320,
					Height:      240,
				},
			},
			ReferencedMessage: &discordgo.Message{
				Author: &discordgo.User{ID: "bot-1"},
			},
		},
	}

	content, ok := promptContentForMessage(session, message)
	if !ok {
		t.Fatal("expected image-only reply to be accepted")
	}
	if content != "" {
		t.Fatalf("expected empty text content, got %q", content)
	}
}

func TestMessageRecordForDiscordMessageIncludesVisualReferences(t *testing.T) {
	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			GuildID: "guild-1",
			Content: "看看这个 <:smile:123456789012345678>",
			Author: &discordgo.User{
				ID:       "user-1",
				Username: "alice",
			},
			Attachments: []*discordgo.MessageAttachment{
				{
					URL:         "https://example.com/pic.png",
					Filename:    "pic.png",
					ContentType: "image/png",
					Width:       320,
					Height:      240,
				},
			},
		},
	}

	record := messageRecordForDiscordMessage(message, "看看这个 <:smile:123456789012345678>", "bot-1")
	if len(record.Images) != 2 {
		t.Fatalf("expected 2 visual references, got %#v", record.Images)
	}
	if record.Images[0].Kind != imageKindCustomEmoji || record.Images[0].EmojiID != "123456789012345678" {
		t.Fatalf("unexpected emoji reference: %#v", record.Images[0])
	}
	if record.Images[1].Kind != imageKindAttachment || record.Images[1].URL != "https://example.com/pic.png" {
		t.Fatalf("unexpected attachment reference: %#v", record.Images[1])
	}
}

func waitForTypingCall(t *testing.T, sendCh <-chan struct{}) {
	t.Helper()

	select {
	case <-sendCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for typing signal")
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
