package subscription

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/notification"
)

func TestRegisterBaselinesBeforeConfirmationAndActivation(t *testing.T) {
	service, repository, notifier, closeDatabase := newTestService(t)
	defer closeDatabase()

	subscription, err := service.Register(context.Background(), RegisterInput{
		DiscordUserID: "u1",
		Provider:      domain.ProviderMegabox,
		Theater:       domain.Theater{ID: "1372", Name: "강남", AreaCode: "10"},
		Movie:         domain.Movie{ID: "m1", Name: "영화"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Status != database.StatusActive {
		t.Fatalf("status=%q", subscription.Status)
	}
	if notifier.confirmations != 1 {
		t.Fatalf("confirmations=%d", notifier.confirmations)
	}
	status, err := repository.GetDeliveryStatus(context.Background(), subscription.ID, "megabox:s1")
	if err != nil {
		t.Fatal(err)
	}
	if status != database.DeliveryBaseline {
		t.Fatalf("delivery status=%q", status)
	}
}

func TestRegisterDeletesInitializingSubscriptionWhenFetchFails(t *testing.T) {
	service, repository, _, closeDatabase := newTestService(t)
	defer closeDatabase()
	service.providers[domain.ProviderMegabox].(*fakeProvider).fetchErr = errors.New("upstream unavailable")

	_, err := service.Register(context.Background(), sampleInput())
	if err == nil {
		t.Fatal("expected registration error")
	}
	assertNoSubscriptions(t, repository, "u1")
}

func TestRegisterDeletesInitializingSubscriptionWhenConfirmationFails(t *testing.T) {
	service, repository, notifier, closeDatabase := newTestService(t)
	defer closeDatabase()
	notifier.confirmErr = errors.New("cannot open DM")

	_, err := service.Register(context.Background(), sampleInput())
	if err == nil {
		t.Fatal("expected registration error")
	}
	assertNoSubscriptions(t, repository, "u1")
}

type fakeProvider struct {
	showtimes []domain.Showtime
	fetchErr  error
}

func (p *fakeProvider) ID() domain.ProviderID { return domain.ProviderMegabox }
func (p *fakeProvider) SearchMovies(context.Context, string) ([]domain.Movie, error) {
	return nil, nil
}
func (p *fakeProvider) SearchTheaters(context.Context, string) ([]domain.Theater, error) {
	return nil, nil
}
func (p *fakeProvider) FetchShowtimes(context.Context, string, string) ([]domain.Showtime, error) {
	return p.showtimes, p.fetchErr
}
func (p *fakeProvider) BuildBookingLinks(string, string) domain.BookingLinks {
	return domain.BookingLinks{App: "https://app.example", Web: "https://web.example"}
}

type fakeNotifier struct {
	confirmations int
	confirmErr    error
}

func (n *fakeNotifier) SendRegistrationConfirmation(context.Context, string, database.Subscription) error {
	n.confirmations++
	return n.confirmErr
}
func (n *fakeNotifier) SendAlert(context.Context, string, notification.Alert) error { return nil }

func newTestService(t *testing.T) (*Service, *database.Repository, *fakeNotifier, func()) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	repository := database.NewRepository(db, func() time.Time {
		return time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	})
	provider := &fakeProvider{showtimes: []domain.Showtime{{
		Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1",
		ExternalID: "s1", PlayDate: "2026-07-20", StartsAt: "14:00", Auditorium: "1관",
	}}}
	notifier := &fakeNotifier{}
	providers := map[domain.ProviderID]domain.TheaterProvider{domain.ProviderMegabox: provider}
	return NewService(repository, notifier, providers), repository, notifier, func() { _ = db.Close() }
}

func sampleInput() RegisterInput {
	return RegisterInput{
		DiscordUserID: "u1", Provider: domain.ProviderMegabox,
		Theater: domain.Theater{ID: "1372", Name: "강남", AreaCode: "10"},
		Movie:   domain.Movie{ID: "m1", Name: "영화"},
	}
}

func assertNoSubscriptions(t *testing.T, repository *database.Repository, userID string) {
	t.Helper()
	subscriptions, err := repository.ListSubscriptionsByUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 0 {
		t.Fatalf("subscriptions=%+v", subscriptions)
	}
}
