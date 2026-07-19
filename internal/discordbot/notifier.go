package discordbot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/database"
	"my-movie/internal/notification"
)

type MessageSession interface {
	UserChannelCreate(string, ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
}

type Notifier struct{ session MessageSession }

func NewNotifier(session MessageSession) *Notifier { return &Notifier{session: session} }

func (n *Notifier) SendRegistrationConfirmation(_ context.Context, userID string, item database.Subscription) error {
	message := &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title:       "오픈 알림 등록 완료",
		Description: fmt.Sprintf("%s · %s\n%s", providerName(item.Provider), item.Theater.Name, item.Movie.Name),
		Color:       0x2ecc71,
	}}}
	return n.send(userID, message)
}

func (n *Notifier) SendAlert(_ context.Context, userID string, alert notification.Alert) error {
	if err := requireHTTPS(alert.Links.App, alert.Links.Web); err != nil {
		return err
	}
	lines := make([]string, 0, len(alert.Times))
	for index, start := range alert.Times {
		line := start
		if index < len(alert.Auditoriums) && alert.Auditoriums[index] != "" {
			line += " · " + alert.Auditoriums[index]
		}
		lines = append(lines, line)
	}
	message := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       "새 예매 회차가 열렸어요",
			Description: fmt.Sprintf("**%s**\n%s · %s\n\n%s", alert.Movie.Name, providerName(alert.Provider), alert.Theater.Name, strings.Join(lines, "\n")),
			Color:       0xe74c3c,
			Fields:      []*discordgo.MessageEmbedField{{Name: "상영일", Value: alert.PlayDate}},
		}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "앱에서 예매", Style: discordgo.LinkButton, URL: alert.Links.App},
			discordgo.Button{Label: "웹에서 예매", Style: discordgo.LinkButton, URL: alert.Links.Web},
		}}},
	}
	return n.send(userID, message)
}

func (n *Notifier) send(userID string, message *discordgo.MessageSend) error {
	channel, err := n.session.UserChannelCreate(userID)
	if err != nil {
		return classifyDiscordError(err)
	}
	_, err = n.session.ChannelMessageSendComplex(channel.ID, message)
	return classifyDiscordError(err)
}

func classifyDiscordError(err error) error {
	if err == nil {
		return nil
	}
	var restError *discordgo.RESTError
	if errors.As(err, &restError) && restError.Message != nil && restError.Message.Code == 50007 {
		return fmt.Errorf("%w: %v", notification.ErrDMUnavailable, err)
	}
	return err
}

func requireHTTPS(values ...string) error {
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("booking URL must use HTTPS: %q", value)
		}
	}
	return nil
}
