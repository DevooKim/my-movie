package notification

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

func TestDeliverPendingGroupsMovieDateAndSendsToTargetChannel(t *testing.T) {
	service, repository, notifier, closeDatabase := newNotificationTestService(t)
	defer closeDatabase()
	showtimes := []domain.Showtime{
		{Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", TheaterID: "1351", TheaterName: "코엑스", MovieID: "m1", MovieName: "호프", ExternalID: "late", PlayDate: "2026-07-20", StartsAt: "18:30", EndsAt: "20:30", Auditorium: "Dolby Cinema", RemainingSeats: 20, TotalSeats: 144, SeatCountKnown: true},
		{Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", TheaterID: "1351", TheaterName: "코엑스", MovieID: "m1", MovieName: "호프", ExternalID: "early", PlayDate: "2026-07-20", StartsAt: "14:00", EndsAt: "16:00", Auditorium: "Dolby Cinema", RemainingSeats: 57, TotalSeats: 144, SeatCountKnown: true},
	}
	if err := repository.RecordTargetSnapshot(context.Background(), "megabox-coex-dolby", showtimes); err != nil {
		t.Fatal(err)
	}

	if err := service.DeliverPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.alerts) != 1 || notifier.channels[0] != "dolby-channel" {
		t.Fatalf("channels=%v alerts=%+v", notifier.channels, notifier.alerts)
	}
	alert := notifier.alerts[0]
	if alert.MovieName != "호프" || len(alert.Sessions) != 2 || alert.Sessions[0].StartsAt != "14:00" || alert.Sessions[1].StartsAt != "18:30" {
		t.Fatalf("alert=%+v", alert)
	}
	if alert.Links.App != "https://app.example/1351/m1" || alert.Links.Web != "https://web.example/1351/m1" {
		t.Fatalf("links=%+v", alert.Links)
	}
	for _, id := range []string{"early", "late"} {
		delivery, err := repository.GetChannelDelivery(context.Background(), "megabox-coex-dolby", id)
		if err != nil || delivery.Status != database.DeliverySent || delivery.AttemptCount != 1 {
			t.Fatalf("delivery %s=%+v err=%v", id, delivery, err)
		}
	}
}

func TestDeliverPendingFailsAfterThreeTransientAttempts(t *testing.T) {
	service, repository, notifier, closeDatabase := newNotificationTestService(t)
	defer closeDatabase()
	notifier.err = errors.New("temporary discord error")
	showtime := domain.Showtime{Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", TheaterID: "1351", TheaterName: "코엑스", MovieID: "m1", MovieName: "호프", ExternalID: "s1", PlayDate: "2026-07-20", StartsAt: "14:00"}
	if err := repository.RecordTargetSnapshot(context.Background(), "megabox-coex-dolby", []domain.Showtime{showtime}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err := service.DeliverPending(context.Background()); err == nil {
			t.Fatalf("attempt %d: expected delivery error", attempt)
		}
	}
	delivery, err := repository.GetChannelDelivery(context.Background(), "megabox-coex-dolby", "s1")
	if err != nil || delivery.Status != database.DeliveryFailed || delivery.AttemptCount != 3 {
		t.Fatalf("delivery=%+v err=%v", delivery, err)
	}
}

func TestUnavailableChannelTurnsTargetOff(t *testing.T) {
	service, repository, notifier, closeDatabase := newNotificationTestService(t)
	defer closeDatabase()
	notifier.err = ErrChannelUnavailable
	showtime := domain.Showtime{Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", TheaterID: "1351", TheaterName: "코엑스", MovieID: "m1", MovieName: "호프", ExternalID: "s1", PlayDate: "2026-07-20", StartsAt: "14:00"}
	if err := repository.RecordTargetSnapshot(context.Background(), "megabox-coex-dolby", []domain.Showtime{showtime}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeliverPending(context.Background()); !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("error=%v", err)
	}
	if service.disabler.(*recordingDisabler).targetID != "megabox-coex-dolby" {
		t.Fatalf("disabled target=%q", service.disabler.(*recordingDisabler).targetID)
	}
	states, err := repository.ListTargetStates(context.Background())
	if err != nil || len(states) != 1 || states[0].Enabled {
		t.Fatalf("states=%+v err=%v", states, err)
	}
}

type recordingNotifier struct {
	channels []string
	alerts   []Alert
	err      error
}

type recordingDisabler struct {
	repository *database.Repository
	targetID   string
}

func (d *recordingDisabler) DisableUnavailable(ctx context.Context, targetID string) error {
	d.targetID = targetID
	return d.repository.DisableTarget(ctx, targetID)
}

func (n *recordingNotifier) SendAlert(_ context.Context, channelID string, alert Alert) error {
	n.channels = append(n.channels, channelID)
	n.alerts = append(n.alerts, alert)
	return n.err
}

type linkProvider struct{}

func (linkProvider) BuildBookingLinks(target domain.AlertTarget, movieID string) domain.BookingLinks {
	return domain.BookingLinks{App: "https://app.example/" + target.Theater.ID + "/" + movieID, Web: "https://web.example/" + target.Theater.ID + "/" + movieID}
}

func newNotificationTestService(t *testing.T) (*Service, *database.Repository, *recordingNotifier, func()) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	repository := database.NewRepository(db, func() time.Time { return time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC) })
	if err := repository.SaveTargetState(context.Background(), database.TargetState{TargetID: "megabox-coex-dolby", ChannelID: "dolby-channel", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	providers := map[domain.ProviderID]LinkProvider{domain.ProviderMegabox: linkProvider{}}
	disabler := &recordingDisabler{repository: repository}
	return NewService(repository, notifier, providers, disabler), repository, notifier, func() { _ = db.Close() }
}

func TestTargetCatalogUsedByNotificationStillContainsCoex(t *testing.T) {
	if _, ok := targets.Find("megabox-coex-dolby"); !ok {
		t.Fatal("missing coex target")
	}
}
