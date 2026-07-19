package discordbot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/domain"
	"my-movie/internal/notification"
)

type MessageSession interface {
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
}

type Notifier struct{ session MessageSession }

func NewNotifier(session MessageSession) *Notifier { return &Notifier{session: session} }

func (n *Notifier) SendAlert(ctx context.Context, channelID string, alert notification.Alert) error {
	if err := requireHTTPS(alert.Links.App, alert.Links.Web); err != nil {
		return err
	}
	message := alertMessage(alert)
	_, err := n.session.ChannelMessageSendComplex(channelID, message, discordgo.WithContext(ctx))
	return classifyDiscordError(err)
}

func alertMessage(alert notification.Alert) *discordgo.MessageSend {
	var content strings.Builder
	content.WriteString("🎬 새 예매 회차 오픈\n\n")
	fmt.Fprintf(&content, "**%s**\n", alert.MovieName)
	fmt.Fprintf(&content, "📅 **%s**\n", koreanDate(alert.PlayDate))
	for _, session := range alert.Sessions {
		timeRange := session.StartsAt
		if session.EndsAt != "" {
			timeRange += " – " + session.EndsAt
		}
		fmt.Fprintf(&content, "⏰ **%s**\n", timeRange)
		if session.SeatCountKnown {
			fmt.Fprintf(&content, "💺 잔여 %d / %d석\n", session.RemainingSeats, session.TotalSeats)
		}
	}
	fmt.Fprintf(&content, "\n%s %s · %s", providerDisplayName(alert.Provider), alert.TheaterName, alert.AuditoriumName)
	return &discordgo.MessageSend{
		Content: content.String(),
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "앱에서 예매", Style: discordgo.LinkButton, URL: alert.Links.App},
			discordgo.Button{Label: "웹에서 예매", Style: discordgo.LinkButton, URL: alert.Links.Web},
		}}},
	}
}

func koreanDate(raw string) string {
	date, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%d년 %d월 %d일", date.Year(), date.Month(), date.Day())
}

func providerDisplayName(provider domain.ProviderID) string {
	if provider == domain.ProviderMegabox {
		return "메가박스"
	}
	if provider == domain.ProviderCGV {
		return "CGV"
	}
	return string(provider)
}

func classifyDiscordError(err error) error {
	if err == nil {
		return nil
	}
	var restError *discordgo.RESTError
	if errors.As(err, &restError) && restError.Message != nil {
		switch restError.Message.Code {
		case 10003, 50001, 50013:
			return fmt.Errorf("%w: %v", notification.ErrChannelUnavailable, err)
		}
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
