package discordbot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/domain"
	"my-movie/internal/notification"
)

func TestNotifierSendsPlainMarkdownToTargetChannel(t *testing.T) {
	session := &fakeDiscordSession{}
	notifier := NewNotifier(session)
	alert := notification.Alert{
		Provider: domain.ProviderCGV, TheaterName: "용산아이파크몰", AuditoriumName: "IMAX",
		MovieName: "호프", PlayDate: "2026-07-19",
		Sessions: []notification.Session{
			{StartsAt: "19:10", EndsAt: "21:56", RemainingSeats: 57, TotalSeats: 144, SeatCountKnown: true},
			{StartsAt: "22:30", EndsAt: "00:50"},
		},
		Links: domain.BookingLinks{App: "https://m.cgv.co.kr/booking", Web: "https://cgv.co.kr/ticket"},
	}

	if err := notifier.SendAlert(context.Background(), "imax-channel", alert); err != nil {
		t.Fatal(err)
	}
	if session.channelID != "imax-channel" || len(session.sent.Embeds) != 0 {
		t.Fatalf("channel=%q message=%+v", session.channelID, session.sent)
	}
	wantParts := []string{"**호프**", "📅 **2026년 7월 19일**", "⏰ **19:10 – 21:56**", "💺 잔여 57 / 144석", "⏰ **22:30 – 00:50**", "CGV 용산아이파크몰 · IMAX"}
	for _, part := range wantParts {
		if !strings.Contains(session.sent.Content, part) {
			t.Fatalf("missing %q in %q", part, session.sent.Content)
		}
	}
	if strings.Contains(session.sent.Content, "**CGV") || len(session.sent.Components) != 1 {
		t.Fatalf("unexpected formatting: %+v", session.sent)
	}
}

func TestNotifierMapsMissingOrForbiddenChannel(t *testing.T) {
	for _, code := range []int{10003, 50001, 50013} {
		session := &fakeDiscordSession{sendErr: &discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: code}}}
		notifier := NewNotifier(session)
		err := notifier.SendAlert(context.Background(), "missing", notification.Alert{Links: domain.BookingLinks{App: "https://app.example", Web: "https://web.example"}})
		if !errors.Is(err, notification.ErrChannelUnavailable) {
			t.Fatalf("code=%d error=%v", code, err)
		}
	}
}

type fakeDiscordSession struct {
	channelID string
	sent      *discordgo.MessageSend
	sendErr   error
}

func (s *fakeDiscordSession) ChannelMessageSendComplex(channelID string, message *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	s.channelID = channelID
	s.sent = message
	return nil, s.sendErr
}
