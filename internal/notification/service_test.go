package notification

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

func TestDeliverPendingGroupsAndSortsShowtimes(t *testing.T) {
	service, repository, notifier, subscription, closeDatabase := newNotificationTestService(t)
	defer closeDatabase()
	showtimes := []domain.Showtime{
		{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "late", PlayDate: "2026-07-20", StartsAt: "18:30", Auditorium: "2관"},
		{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "early", PlayDate: "2026-07-20", StartsAt: "14:00", Auditorium: "1관"},
	}
	if err := repository.RecordScan(context.Background(), showtimes, ""); err != nil {
		t.Fatal(err)
	}

	if err := service.DeliverPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.alerts) != 1 {
		t.Fatalf("alerts=%d", len(notifier.alerts))
	}
	alert := notifier.alerts[0]
	if got := alert.Times; len(got) != 2 || got[0] != "14:00" || got[1] != "18:30" {
		t.Fatalf("times=%v", got)
	}
	if alert.Links.App != "https://app.example/1372/m1" || alert.Links.Web != "https://web.example/1372/m1" {
		t.Fatalf("links=%+v", alert.Links)
	}
	assertDelivery(t, repository, subscription.ID, "megabox:early", database.DeliverySent, 1)
	assertDelivery(t, repository, subscription.ID, "megabox:late", database.DeliverySent, 1)
}

func TestDeliverPendingFailsAfterThreeTransientAttempts(t *testing.T) {
	service, repository, notifier, subscription, closeDatabase := newNotificationTestService(t)
	defer closeDatabase()
	notifier.alertErr = errors.New("temporary discord error")
	showtime := domain.Showtime{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "s1", PlayDate: "2026-07-20", StartsAt: "14:00"}
	if err := repository.RecordScan(context.Background(), []domain.Showtime{showtime}, ""); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		if err := service.DeliverPending(context.Background()); err == nil {
			t.Fatalf("attempt %d: expected delivery error", attempt)
		}
	}
	assertDelivery(t, repository, subscription.ID, "megabox:s1", database.DeliveryFailed, 3)
}

func TestDeliverPendingDisablesSubscriptionWhenDMIsUnavailable(t *testing.T) {
	service, repository, notifier, subscription, closeDatabase := newNotificationTestService(t)
	defer closeDatabase()
	notifier.alertErr = ErrDMUnavailable
	showtime := domain.Showtime{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "s1", PlayDate: "2026-07-20", StartsAt: "14:00"}
	if err := repository.RecordScan(context.Background(), []domain.Showtime{showtime}, ""); err != nil {
		t.Fatal(err)
	}

	if err := service.DeliverPending(context.Background()); !errors.Is(err, ErrDMUnavailable) {
		t.Fatalf("error=%v", err)
	}
	got, err := repository.GetSubscription(context.Background(), subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != database.StatusDisabled {
		t.Fatalf("status=%q", got.Status)
	}
	assertDelivery(t, repository, subscription.ID, "megabox:s1", database.DeliveryFailed, 1)
}

func TestDeliveryStateSurvivesRestartWithoutDuplicateAlert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.sqlite")
	notifier := &recordingNotifier{}
	providers := map[domain.ProviderID]domain.TheaterProvider{domain.ProviderMegabox: linkProvider{}}
	ctx := context.Background()

	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	repository := database.NewRepository(db, time.Now)
	subscription, err := repository.CreateInitializingSubscription(ctx, database.CreateSubscriptionInput{
		DiscordUserID: "u1", Provider: domain.ProviderMegabox,
		Theater: domain.Theater{ID: "1372", Name: "Gangnam", AreaCode: "10"}, Movie: domain.Movie{ID: "m1", Name: "Movie"},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline := domain.Showtime{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "baseline", PlayDate: "2026-07-20", StartsAt: "10:00"}
	if err := repository.RecordScan(ctx, []domain.Showtime{baseline}, subscription.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ActivateSubscription(ctx, subscription.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	repository = database.NewRepository(db, time.Now)
	newShowtime := domain.Showtime{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "new", PlayDate: "2026-07-20", StartsAt: "14:00"}
	if err := repository.RecordScan(ctx, []domain.Showtime{baseline, newShowtime}, ""); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository, notifier, providers).DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if len(notifier.alerts) != 1 || len(notifier.alerts[0].Times) != 1 || notifier.alerts[0].Times[0] != "14:00" {
		t.Fatalf("alerts=%+v", notifier.alerts)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository = database.NewRepository(db, time.Now)
	if err := repository.RecordScan(ctx, []domain.Showtime{baseline, newShowtime}, ""); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository, notifier, providers).DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if len(notifier.alerts) != 1 {
		t.Fatalf("duplicate alerts=%d", len(notifier.alerts))
	}
}

type recordingNotifier struct {
	alerts   []Alert
	alertErr error
}

func (n *recordingNotifier) SendRegistrationConfirmation(context.Context, string, database.Subscription) error {
	return nil
}
func (n *recordingNotifier) SendAlert(_ context.Context, _ string, alert Alert) error {
	n.alerts = append(n.alerts, alert)
	return n.alertErr
}

type linkProvider struct{}

func (linkProvider) ID() domain.ProviderID                                        { return domain.ProviderMegabox }
func (linkProvider) SearchMovies(context.Context, string) ([]domain.Movie, error) { return nil, nil }
func (linkProvider) SearchTheaters(context.Context, string) ([]domain.Theater, error) {
	return nil, nil
}
func (linkProvider) FetchShowtimes(context.Context, string, string) ([]domain.Showtime, error) {
	return nil, nil
}
func (linkProvider) BuildBookingLinks(theaterID, movieID string) domain.BookingLinks {
	return domain.BookingLinks{App: "https://app.example/" + theaterID + "/" + movieID, Web: "https://web.example/" + theaterID + "/" + movieID}
}

func newNotificationTestService(t *testing.T) (*Service, *database.Repository, *recordingNotifier, database.Subscription, func()) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	repository := database.NewRepository(db, time.Now)
	subscription, err := repository.CreateInitializingSubscription(context.Background(), database.CreateSubscriptionInput{
		DiscordUserID: "u1", Provider: domain.ProviderMegabox,
		Theater: domain.Theater{ID: "1372", Name: "강남", AreaCode: "10"},
		Movie:   domain.Movie{ID: "m1", Name: "영화"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ActivateSubscription(context.Background(), subscription.ID); err != nil {
		t.Fatal(err)
	}
	subscription.Status = database.StatusActive
	notifier := &recordingNotifier{}
	providers := map[domain.ProviderID]domain.TheaterProvider{domain.ProviderMegabox: linkProvider{}}
	return NewService(repository, notifier, providers), repository, notifier, subscription, func() { _ = db.Close() }
}

func assertDelivery(t *testing.T, repository *database.Repository, subscriptionID, key string, status database.DeliveryStatus, attempts int) {
	t.Helper()
	delivery, err := repository.GetDelivery(context.Background(), subscriptionID, key)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != status || delivery.AttemptCount != attempts {
		t.Fatalf("delivery=%+v want status=%q attempts=%d", delivery, status, attempts)
	}
}
