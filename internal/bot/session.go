package bot

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

type Session struct {
	session *discordgo.Session
}

func NewSession(token string, handler *Handler) (*Session, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages
	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout())
		defer cancel()
		response, err := handler.HandleMessage(ctx, m.ChannelID, m.Content)
		if err != nil {
			_, _ = s.ChannelMessageSend(m.ChannelID, "抱歉，我现在无法回应。")
			return
		}
		_, _ = s.ChannelMessageSend(m.ChannelID, response)
	})
	return &Session{session: session}, nil
}

func (s *Session) Open() error {
	return s.session.Open()
}

func (s *Session) Close() error {
	return s.session.Close()
}

func (s *Session) CloseWithContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- s.session.Close()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}
