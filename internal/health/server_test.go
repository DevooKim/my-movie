package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"my-movie/internal/domain"
)

var healthNow = time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

func TestHealthIsOKWithNoActiveProviders(t *testing.T) {
	response := serveHealth(t, &fakeHealthStore{}, 5*time.Minute)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHealthIsOKWhenProviderSucceededWithinTwoIntervals(t *testing.T) {
	store := &fakeHealthStore{
		providers: []domain.ProviderID{domain.ProviderMegabox},
		latest:    map[domain.ProviderID]time.Time{domain.ProviderMegabox: healthNow.Add(-9 * time.Minute)},
	}
	response := serveHealth(t, store, 5*time.Minute)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHealthIsUnavailableForStaleProviderOrDatabase(t *testing.T) {
	for name, store := range map[string]*fakeHealthStore{
		"stale":    {providers: []domain.ProviderID{domain.ProviderMegabox}, latest: map[domain.ProviderID]time.Time{domain.ProviderMegabox: healthNow.Add(-11 * time.Minute)}},
		"database": {pingErr: errors.New("database unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			response := serveHealth(t, store, 5*time.Minute)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHealthResponseDoesNotExposeSubscriptionData(t *testing.T) {
	store := &fakeHealthStore{providers: []domain.ProviderID{domain.ProviderMegabox}, latest: map[domain.ProviderID]time.Time{domain.ProviderMegabox: healthNow}}
	response := serveHealth(t, store, 5*time.Minute)
	body := response.Body.String()
	for _, secret := range []string{"discord-user-123", "movie-id", "theater-id", "payload"} {
		if strings.Contains(body, secret) {
			t.Fatalf("body exposes %q: %s", secret, body)
		}
	}
}

func serveHealth(t *testing.T, store *fakeHealthStore, interval time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewHandler(store, interval, func() time.Time { return healthNow })
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type fakeHealthStore struct {
	pingErr   error
	providers []domain.ProviderID
	latest    map[domain.ProviderID]time.Time
}

func (s *fakeHealthStore) PingContext(context.Context) error { return s.pingErr }
func (s *fakeHealthStore) ListActiveProviderIDs(context.Context) ([]domain.ProviderID, error) {
	return s.providers, nil
}
func (s *fakeHealthStore) LatestSuccessfulPoll(_ context.Context, provider domain.ProviderID) (time.Time, error) {
	return s.latest[provider], nil
}
