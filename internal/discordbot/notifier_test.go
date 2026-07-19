package discordbot

import (
	"context"
	"errors"
	"testing"

	"github.com/bwmarrin/discordgo"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/notification"
)

func TestNotifierBuildsAlertWithTwoHTTPSButtons(t *testing.T) {
	session := &fakeDiscordSession{}
	notifier := NewNotifier(session)
	alert := notification.Alert{
		Provider: domain.ProviderMegabox, Theater: domain.Theater{Name: "Gangnam"}, Movie: domain.Movie{Name: "Movie"},
		PlayDate: "2026-07-20", Times: []string{"14:00", "18:30"},
		Links: domain.BookingLinks{App: "https://m.megabox.co.kr/booking/", Web: "https://www.megabox.co.kr/booking"},
	}

	if err := notifier.SendAlert(context.Background(), "u1", alert); err != nil {
		t.Fatal(err)
	}
	if len(session.sent.Components) != 1 {
		t.Fatalf("components=%+v", session.sent.Components)
	}
	row := session.sent.Components[0].(discordgo.ActionsRow)
	if len(row.Components) != 2 {
		t.Fatalf("buttons=%+v", row.Components)
	}
}

func TestNotifierMapsDiscordCannotSendDM(t *testing.T) {
	session := &fakeDiscordSession{sendErr: &discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: 50007}}}
	notifier := NewNotifier(session)

	err := notifier.SendRegistrationConfirmation(context.Background(), "u1", database.Subscription{})
	if !errors.Is(err, notification.ErrDMUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

type fakeDiscordSession struct {
	sent    *discordgo.MessageSend
	sendErr error
}

func (s *fakeDiscordSession) UserChannelCreate(string, ...discordgo.RequestOption) (*discordgo.Channel, error) {
	return &discordgo.Channel{ID: "dm1"}, nil
}
func (s *fakeDiscordSession) ChannelMessageSendComplex(_ string, message *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	s.sent = message
	return nil, s.sendErr
}
