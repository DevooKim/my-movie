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

type Notifier struct {
	session          MessageSession
	appLaunchBaseURL string
}

func NewNotifier(session MessageSession, appLaunchBaseURL ...string) *Notifier {
	var baseURL string
	if len(appLaunchBaseURL) > 0 {
		baseURL = appLaunchBaseURL[0]
	}
	return &Notifier{session: session, appLaunchBaseURL: baseURL}
}

func (n *Notifier) SendAlert(ctx context.Context, channelID string, alert notification.Alert) error {
	appButtonLabel := "앱에서 예매"
	if n.appLaunchBaseURL != "" {
		launchURL, err := url.JoinPath(n.appLaunchBaseURL, string(alert.Provider))
		if err != nil {
			return fmt.Errorf("build app launch URL: %w", err)
		}
		alert.Links.App = launchURL
		appButtonLabel = providerDisplayName(alert.Provider) + " 앱 열기"
	}
	if err := requireHTTPS(alert.Links.App); err != nil {
		return err
	}
	message := alertMessage(alert, appButtonLabel)
	_, err := n.session.ChannelMessageSendComplex(channelID, message, discordgo.WithContext(ctx))
	return classifyDiscordError(err)
}

func (n *Notifier) SendControlMessage(ctx context.Context, channelID, content string) error {
	_, err := n.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: content}, discordgo.WithContext(ctx))
	return classifyDiscordError(err)
}

func alertMessage(alert notification.Alert, appButtonLabel string) *discordgo.MessageSend {
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
			discordgo.Button{Label: appButtonLabel, Style: discordgo.LinkButton, URL: alert.Links.App},
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
