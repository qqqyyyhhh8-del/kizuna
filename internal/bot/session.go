package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"kizuna/internal/memory"
	"kizuna/pkg/pluginapi"

	"github.com/bwmarrin/discordgo"
)

const typingRefreshInterval = 8 * time.Second

type typingSender func(channelID string) error

type Session struct {
	session        *discordgo.Session
	commandGuildID string
	handler        *Handler
}

func NewSession(token, commandGuildID string, handler *Handler) (*Session, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	wrapper := &Session{
		session:        session,
		commandGuildID: commandGuildID,
		handler:        handler,
	}
	if handler != nil && handler.pluginManager != nil {
		handler.pluginManager.SetSendMessageFn(wrapper.sendPluginMessage)
		handler.pluginManager.SetReplyToMessageFn(wrapper.replyToPluginMessage)
		handler.pluginManager.SetListGuildEmojisFn(wrapper.listGuildEmojis)
		handler.pluginManager.SetRefreshCommandsFn(wrapper.registerCommands)
	}
	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}

		if handler != nil && handler.pluginManager != nil {
			if pluginContent, ok := pluginEventContentForMessage(m); ok {
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
				handler.pluginManager.DispatchMessage(ctx, pluginMessageEventFromDiscord(s, m, pluginContent))
				cancel()
			}
		}

		content, ok := promptContentForMessage(s, m)
		if ok {
			if !handler.AllowsSpeechForMessage(s, m) {
				return
			}
			handleIncomingMessage(s, m, handler, content)
			return
		}

		if handler != nil && handler.pluginManager != nil && handler.pluginManager.CanHandleSlashCommand("proactive") {
			return
		}

		content, ok = proactivePromptContentForMessage(m)
		if !ok || !handler.ShouldProactiveReply() {
			return
		}
		if !handler.AllowsSpeechForMessage(s, m) {
			return
		}
		handleIncomingMessage(s, m, handler, content)
	})
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i == nil {
			return
		}

		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			commandData := i.ApplicationCommandData()
			if commandData.Name == "setup" {
				response, err := handler.SetupPanelCommandResponse(interactionUserID(i), speechLocationForInteraction(s, i))
				if err != nil {
					response = simpleEphemeralInteractionResponse("抱歉，我现在无法打开允许发言范围面板。")
				}
				_ = s.InteractionRespond(i.Interaction, response)
				return
			}
			if commandData.Name == "plugin" {
				response, err := handler.PluginPanelCommandResponse(interactionUserID(i), speechLocationForInteraction(s, i))
				if err != nil {
					response = pluginErrorInteractionResponse("插件面板打开失败", err.Error())
				}
				_ = s.InteractionRespond(i.Interaction, response)
				return
			}
			if handler != nil && handler.pluginManager != nil && handler.pluginManager.CanHandleSlashCommand(commandData.Name) {
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
				defer cancel()

				response, err := handler.pluginManager.HandleSlashCommand(ctx, pluginSlashCommandRequestFromInteraction(s, i, commandData))
				if err != nil {
					response = denyPluginErrorInteractionResponse("抱歉，我现在无法处理这个插件命令。")
				}
				_ = s.InteractionRespond(i.Interaction, discordInteractionResponseFromPlugin(response))
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
			defer cancel()

			response, ephemeral, err := handler.HandleSlashCommand(ctx, interactionUserID(i), commandData)
			if err != nil {
				response = "抱歉，我现在无法处理这个命令。"
				ephemeral = true
			}
			if strings.TrimSpace(response) == "" {
				response = "已完成。"
			}
			_ = respondToInteraction(s, i.Interaction, response, ephemeral)
		case discordgo.InteractionMessageComponent:
			componentData := i.MessageComponentData()
			if isSetupInteractionCustomID(componentData.CustomID) {
				response, err := handler.SetupComponentResponse(interactionUserID(i), speechLocationForInteraction(s, i), componentData)
				if err != nil {
					response = simpleEphemeralInteractionResponse("允许发言范围面板处理失败。")
				}
				_ = s.InteractionRespond(i.Interaction, response)
				return
			}
			if isPluginInteractionCustomID(componentData.CustomID) {
				response, err := handler.PluginComponentResponse(interactionUserID(i), speechLocationForInteraction(s, i), componentData)
				if err != nil {
					response = pluginErrorInteractionResponse("插件面板处理失败", err.Error())
				}
				_ = s.InteractionRespond(i.Interaction, response)
				return
			}
			if handler != nil && handler.pluginManager != nil && handler.pluginManager.CanHandleComponent(componentData.CustomID) {
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
				defer cancel()

				response, err := handler.pluginManager.HandleComponent(ctx, pluginComponentRequestFromInteraction(s, i, componentData))
				if err != nil {
					response = denyPluginErrorInteractionResponse("抱歉，我现在无法处理这个插件交互。")
				}
				_ = s.InteractionRespond(i.Interaction, discordInteractionResponseFromPlugin(response))
				return
			}
		case discordgo.InteractionModalSubmit:
			modalData := i.ModalSubmitData()
			if isPluginInteractionCustomID(modalData.CustomID) {
				_ = s.InteractionRespond(i.Interaction, deferredChannelMessageResponse(true))
				go func(interaction *discordgo.Interaction, userID string, location speechLocation, data discordgo.ModalSubmitInteractionData) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()

					edit, err := handler.PluginModalEdit(ctx, userID, location, data)
					if err != nil {
						edit = pluginErrorEdit("插件面板处理失败", err.Error())
					}
					if edit == nil {
						edit = pluginEmbedsEdit(&discordgo.MessageEmbed{
							Title:       "Plugin Control Center",
							Description: "已完成。",
							Color:       pluginEmbedColorInfo,
						})
					}
					_ = editInteractionResponse(s, interaction, edit)
				}(i.Interaction, interactionUserID(i), speechLocationForInteraction(s, i), modalData)
				return
			}
			if handler != nil && handler.pluginManager != nil && handler.pluginManager.CanHandleModal(modalData.CustomID) {
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
				defer cancel()

				response, err := handler.pluginManager.HandleModal(ctx, pluginModalRequestFromInteraction(s, i, modalData))
				if err != nil {
					response = denyPluginErrorInteractionResponse("抱歉，我现在无法处理这个插件表单。")
				}
				_ = s.InteractionRespond(i.Interaction, discordInteractionResponseFromPlugin(response))
				return
			}
		}
	})
	return wrapper, nil
}

func promptContentForMessage(s *discordgo.Session, m *discordgo.MessageCreate) (string, bool) {
	if m == nil || m.Message == nil {
		return "", false
	}

	content := strings.TrimSpace(m.Content)
	hasVisualInput := discordMessageHasVisualInput(m.Message)
	if content == "" {
		if !hasVisualInput {
			return "", false
		}
	}

	if m.GuildID == "" {
		return content, true
	}

	if s.State == nil || s.State.User == nil {
		return "", false
	}

	selfID := s.State.User.ID
	triggeredByMention := mentionsUser(m.Mentions, selfID)
	triggeredByReply := repliesToUser(m.Message, selfID)
	if !triggeredByMention && !triggeredByReply {
		return "", false
	}

	if triggeredByMention {
		content = strings.ReplaceAll(content, "<@"+selfID+">", "")
		content = strings.ReplaceAll(content, "<@!"+selfID+">", "")
	}
	content = strings.TrimSpace(content)
	if content == "" && !hasVisualInput {
		return "", false
	}
	return content, true
}

func proactivePromptContentForMessage(m *discordgo.MessageCreate) (string, bool) {
	if m == nil || m.Message == nil {
		return "", false
	}
	if strings.TrimSpace(m.GuildID) == "" {
		return "", false
	}

	content := strings.TrimSpace(m.Content)
	hasVisualInput := discordMessageHasVisualInput(m.Message)
	if content == "" && !hasVisualInput {
		return "", false
	}
	return content, true
}

func pluginEventContentForMessage(m *discordgo.MessageCreate) (string, bool) {
	if m == nil || m.Message == nil {
		return "", false
	}

	content := strings.TrimSpace(m.Content)
	hasVisualInput := discordMessageHasVisualInput(m.Message)
	if content == "" && !hasVisualInput {
		return "", false
	}
	return content, true
}

func mentionsUser(users []*discordgo.User, userID string) bool {
	for _, user := range users {
		if user != nil && user.ID == userID {
			return true
		}
	}
	return false
}

func repliesToUser(message *discordgo.Message, userID string) bool {
	if message == nil || message.ReferencedMessage == nil || message.ReferencedMessage.Author == nil {
		return false
	}
	return message.ReferencedMessage.Author.ID == userID
}

func messageRecordForDiscordMessage(m *discordgo.MessageCreate, content, botUserID string) memory.MessageRecord {
	record := memory.MessageRecord{
		Role:    "user",
		Content: strings.TrimSpace(content),
	}
	if m == nil || m.Message == nil {
		return record
	}

	record.GuildID = strings.TrimSpace(m.GuildID)
	record.Time = m.Timestamp
	record.Author = authorFromDiscord(m.Author, m.Member)
	record.Images = collectVisualReferences(m.Message, content)
	if reply := replyRecordFromDiscord(m.ReferencedMessage, botUserID); reply != nil {
		record.ReplyTo = reply
	}
	return record
}

func botUserIDFromSession(s *discordgo.Session) string {
	if s == nil || s.State == nil || s.State.User == nil {
		return ""
	}
	return strings.TrimSpace(s.State.User.ID)
}

func authorFromDiscord(user *discordgo.User, member *discordgo.Member) memory.MessageAuthor {
	author := memory.MessageAuthor{}
	if user != nil {
		author.UserID = strings.TrimSpace(user.ID)
		author.Username = strings.TrimSpace(user.Username)
		author.GlobalName = strings.TrimSpace(user.GlobalName)
	}
	if member != nil {
		author.Nick = strings.TrimSpace(member.Nick)
	}
	if author.DisplayName == "" {
		switch {
		case author.Nick != "":
			author.DisplayName = author.Nick
		case author.GlobalName != "":
			author.DisplayName = author.GlobalName
		case author.Username != "":
			author.DisplayName = author.Username
		}
	}
	return author
}

func replyRecordFromDiscord(message *discordgo.Message, botUserID string) *memory.ReplyRecord {
	if message == nil {
		return nil
	}

	role := "user"
	if message.Author != nil && strings.TrimSpace(message.Author.ID) != "" && strings.TrimSpace(message.Author.ID) == strings.TrimSpace(botUserID) {
		role = "assistant"
	}

	return &memory.ReplyRecord{
		MessageID: strings.TrimSpace(message.ID),
		Role:      role,
		Content:   strings.TrimSpace(message.Content),
		Time:      message.Timestamp,
		Author:    authorFromDiscord(message.Author, message.Member),
	}
}

func sendMessageReply(s *discordgo.Session, trigger *discordgo.MessageCreate, content string) (*discordgo.Message, error) {
	if s == nil || trigger == nil || trigger.Message == nil {
		return nil, fmt.Errorf("message reply context is missing")
	}

	replySend := buildReplyMessageSend(trigger.Message, content)
	message, err := s.ChannelMessageSendComplex(trigger.ChannelID, replySend)
	if err == nil {
		return message, nil
	}

	log.Printf("reply send failed, retrying without message reference: guild=%s channel=%s trigger=%s err=%v", strings.TrimSpace(trigger.GuildID), strings.TrimSpace(trigger.ChannelID), strings.TrimSpace(trigger.ID), err)
	return s.ChannelMessageSendComplex(trigger.ChannelID, buildPlainMessageSend(content))
}

func handleIncomingMessage(s *discordgo.Session, m *discordgo.MessageCreate, handler *Handler, content string) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
	defer cancel()
	location := speechLocationForDiscordMessage(s, m)

	stopTyping := startTypingLoop(ctx, m.ChannelID, func(channelID string) error {
		return s.ChannelTyping(channelID)
	}, typingRefreshInterval)
	defer stopTyping()

	response, err := handler.HandleMessageAtLocation(ctx, location, messageRecordForDiscordMessage(m, content, botUserIDFromSession(s)))
	if err != nil {
		if _, sendErr := sendMessageReply(s, m, "抱歉，我现在无法回应。"); sendErr != nil {
			log.Printf("failed to send fallback error reply: guild=%s channel=%s trigger=%s err=%v", strings.TrimSpace(m.GuildID), strings.TrimSpace(m.ChannelID), strings.TrimSpace(m.ID), sendErr)
		}
		return
	}
	if strings.TrimSpace(response) == "" {
		return
	}
	replyMessage, err := sendMessageReply(s, m, response)
	if err != nil {
		log.Printf("failed to send response message: guild=%s channel=%s trigger=%s err=%v", strings.TrimSpace(m.GuildID), strings.TrimSpace(m.ChannelID), strings.TrimSpace(m.ID), err)
		return
	}
	if handler != nil && handler.pluginManager != nil {
		handler.pluginManager.DispatchReplyCommitted(ctx, pluginapi.ReplyCommittedRequest{
			TriggerMessage: pluginMessageEventFromDiscord(s, m, content).Message,
			ReplyMessage:   pluginMessageContextFromDiscordMessage(s, replyMessage, m.GuildID),
		})
	}
}

func buildReplyMessageSend(trigger *discordgo.Message, content string) *discordgo.MessageSend {
	send := buildPlainMessageSend(content)
	if trigger != nil {
		send.Reference = trigger.Reference()
	}
	return send
}

func buildPlainMessageSend(content string) *discordgo.MessageSend {
	content = strings.TrimSpace(content)
	return &discordgo.MessageSend{
		Content: content,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse:       []discordgo.AllowedMentionType{},
			RepliedUser: false,
		},
	}
}

func (s *Session) sendPluginMessage(ctx context.Context, request pluginapi.SendMessageRequest) error {
	if s == nil || s.session == nil {
		return fmt.Errorf("discord session is unavailable")
	}

	channelID := strings.TrimSpace(request.ThreadID)
	if channelID == "" {
		channelID = strings.TrimSpace(request.ChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("plugin message channel is required")
	}

	send := buildPlainMessageSend(request.Content)
	if replyID := strings.TrimSpace(request.ReplyToMessageID); replyID != "" {
		send.Reference = &discordgo.MessageReference{
			MessageID: replyID,
			ChannelID: channelID,
			GuildID:   strings.TrimSpace(request.GuildID),
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.session.ChannelMessageSendComplex(channelID, send)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (s *Session) replyToPluginMessage(ctx context.Context, message pluginapi.MessageContext) error {
	if s == nil || s.session == nil || s.handler == nil {
		return fmt.Errorf("plugin core reply is unavailable")
	}

	channelID := strings.TrimSpace(message.Channel.ThreadID)
	if channelID == "" {
		channelID = strings.TrimSpace(message.Channel.ID)
	}
	if channelID == "" {
		return fmt.Errorf("plugin reply target channel is required")
	}

	record := messageRecordFromPluginMessage(message)
	stopTyping := startTypingLoop(ctx, channelID, func(channelID string) error {
		return s.session.ChannelTyping(channelID)
	}, typingRefreshInterval)
	defer stopTyping()

	response, err := s.handler.HandleMessageRecord(ctx, channelID, record)
	if err != nil {
		return err
	}
	if strings.TrimSpace(response) == "" {
		return nil
	}

	send := buildPlainMessageSend(response)
	if strings.TrimSpace(message.MessageID) != "" {
		send.Reference = &discordgo.MessageReference{
			MessageID: strings.TrimSpace(message.MessageID),
			ChannelID: channelID,
			GuildID:   strings.TrimSpace(message.Guild.ID),
		}
	}

	type result struct {
		message *discordgo.Message
		err     error
	}
	done := make(chan result, 1)
	go func() {
		sent, err := s.session.ChannelMessageSendComplex(channelID, send)
		if err == nil || send.Reference == nil {
			done <- result{message: sent, err: err}
			return
		}

		log.Printf("plugin reply send failed, retrying without message reference: guild=%s channel=%s trigger=%s err=%v", strings.TrimSpace(message.Guild.ID), channelID, strings.TrimSpace(message.MessageID), err)
		sent, retryErr := s.session.ChannelMessageSendComplex(channelID, buildPlainMessageSend(response))
		done <- result{message: sent, err: retryErr}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case outcome := <-done:
		if outcome.err != nil {
			return outcome.err
		}
		if s.handler != nil && s.handler.pluginManager != nil {
			s.handler.pluginManager.DispatchReplyCommitted(ctx, pluginapi.ReplyCommittedRequest{
				TriggerMessage: message,
				ReplyMessage:   pluginMessageContextFromDiscordMessage(s.session, outcome.message, message.Guild.ID),
			})
		}
		return nil
	}
}

func (s *Session) listGuildEmojis(ctx context.Context, guildID string) ([]pluginapi.GuildEmoji, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("discord session is unavailable")
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, fmt.Errorf("guild id is required")
	}

	type result struct {
		emojis []*discordgo.Emoji
		err    error
	}
	done := make(chan result, 1)
	go func() {
		emojis, err := s.session.GuildEmojis(guildID)
		done <- result{emojis: emojis, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case outcome := <-done:
		if outcome.err != nil {
			return nil, outcome.err
		}
		converted := make([]pluginapi.GuildEmoji, 0, len(outcome.emojis))
		for _, emoji := range outcome.emojis {
			if emoji == nil {
				continue
			}
			converted = append(converted, pluginapi.GuildEmoji{
				ID:       strings.TrimSpace(emoji.ID),
				Name:     strings.TrimSpace(emoji.Name),
				Animated: emoji.Animated,
				URL:      emojiCDNURL(emoji.ID),
				Syntax:   emojiSyntax(emoji.Name, emoji.ID, emoji.Animated),
			})
		}
		return converted, nil
	}
}

func startTypingLoop(ctx context.Context, channelID string, send typingSender, interval time.Duration) func() {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" || send == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = typingRefreshInterval
	}

	typingCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if typingCtx.Err() != nil {
			return
		}

		_ = send(channelID)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				_ = send(channelID)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func (s *Session) Open() error {
	if err := s.session.Open(); err != nil {
		return err
	}
	if appID, err := s.applicationID(); err == nil {
		if s.handler != nil && s.handler.pluginManager != nil {
			s.handler.pluginManager.SetBotUserID(appID)
		}
	}
	if err := s.registerCommands(); err != nil {
		_ = s.session.Close()
		return err
	}
	if s.handler != nil && s.handler.pluginManager != nil {
		if err := s.handler.pluginManager.Start(); err != nil {
			_ = s.session.Close()
			return err
		}
	}
	return nil
}

func (s *Session) Close() error {
	if s.handler != nil && s.handler.pluginManager != nil {
		_ = s.handler.pluginManager.Close()
	}
	return s.session.Close()
}

func (s *Session) CloseWithContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		if s.handler != nil && s.handler.pluginManager != nil {
			_ = s.handler.pluginManager.Close()
		}
		done <- s.session.Close()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (s *Session) registerCommands() error {
	appID, err := s.applicationID()
	if err != nil {
		return err
	}

	pluginCommands := []*discordgo.ApplicationCommand{}
	if s.handler != nil && s.handler.pluginManager != nil {
		pluginCommands, err = s.handler.pluginManager.ApplicationCommands()
		if err != nil {
			return err
		}
	}

	commands := slashCommands(pluginCommands)
	return syncApplicationCommands(func(scope string, commands []*discordgo.ApplicationCommand) error {
		_, err := s.session.ApplicationCommandBulkOverwrite(appID, scope, commands)
		return err
	}, s.commandGuildID, commands)
}

func syncApplicationCommands(overwrite func(scope string, commands []*discordgo.ApplicationCommand) error, commandGuildID string, commands []*discordgo.ApplicationCommand) error {
	targetScope := strings.TrimSpace(commandGuildID)
	if err := overwrite(targetScope, commands); err != nil {
		return err
	}
	if targetScope == "" {
		return nil
	}
	if err := overwrite("", []*discordgo.ApplicationCommand{}); err != nil {
		log.Printf("cleanup stale global commands failed: %v", err)
	}
	return nil
}

func (s *Session) applicationID() (string, error) {
	if s.session.State != nil && s.session.State.User != nil && s.session.State.User.ID != "" {
		return s.session.State.User.ID, nil
	}

	user, err := s.session.User("@me")
	if err != nil {
		return "", err
	}
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("failed to resolve bot application ID")
	}
	if s.session.State != nil {
		s.session.State.User = user
	}
	return user.ID, nil
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i == nil {
		return ""
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func respondToInteraction(s *discordgo.Session, interaction *discordgo.Interaction, content string, ephemeral bool) error {
	data := &discordgo.InteractionResponseData{
		Content: content,
	}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	return s.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	})
}

func simpleEphemeralInteractionResponse(content string) *discordgo.InteractionResponse {
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: strings.TrimSpace(content),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}
}

func deferredMessageUpdateResponse() *discordgo.InteractionResponse {
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}
}

func deferredChannelMessageResponse(ephemeral bool) *discordgo.InteractionResponse {
	data := &discordgo.InteractionResponseData{}
	if ephemeral {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: data,
	}
}

func editInteractionResponse(s *discordgo.Session, interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) error {
	if s == nil || interaction == nil || edit == nil {
		return fmt.Errorf("interaction edit context is missing")
	}
	_, err := s.InteractionResponseEdit(interaction, edit)
	return err
}

func guildNameFromSession(s *discordgo.Session, guildID string) string {
	if s == nil || s.State == nil || strings.TrimSpace(guildID) == "" {
		return ""
	}
	guild, err := s.State.Guild(guildID)
	if err != nil || guild == nil {
		return ""
	}
	return strings.TrimSpace(guild.Name)
}
