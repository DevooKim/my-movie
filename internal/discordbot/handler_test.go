package discordbot

import (
	"context"
	"fmt"
	"testing"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/providers"
	"my-movie/internal/subscription"
)

func TestAutocompleteStopsAtDiscordChoiceLimit(t *testing.T) {
	provider := &catalogProvider{id: domain.ProviderMegabox}
	for index := range 30 {
		provider.movies = append(provider.movies, domain.Movie{ID: fmt.Sprintf("m%d", index), Name: fmt.Sprintf("Movie %02d", index)})
	}
	handler := NewHandler(providers.New(provider), &fakeSubscriptions{}, &fakeStore{})

	choices, err := handler.MovieChoices(context.Background(), domain.ProviderMegabox, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 25 {
		t.Fatalf("choices=%d", len(choices))
	}
}

func TestTheaterAutocompleteUsesOnlySelectedProvider(t *testing.T) {
	megabox := &catalogProvider{id: domain.ProviderMegabox, theaters: []domain.Theater{{ID: "mb", Name: "Megabox"}}}
	cgv := &catalogProvider{id: domain.ProviderCGV, theaters: []domain.Theater{{ID: "cgv", Name: "CGV"}}}
	handler := NewHandler(providers.New(megabox, cgv), &fakeSubscriptions{}, &fakeStore{})

	choices, err := handler.TheaterChoices(context.Background(), domain.ProviderMegabox, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 1 || choices[0].Value != "mb" {
		t.Fatalf("choices=%+v", choices)
	}
	if megabox.theaterSearches != 1 || cgv.theaterSearches != 0 {
		t.Fatalf("searches megabox=%d cgv=%d", megabox.theaterSearches, cgv.theaterSearches)
	}
}

func TestDeleteChoicesContainOnlyInvokingUsersSubscriptions(t *testing.T) {
	store := &fakeStore{byUser: map[string][]database.Subscription{
		"u1": {{ID: "s1", Provider: domain.ProviderMegabox, Theater: domain.Theater{Name: "Gangnam"}, Movie: domain.Movie{Name: "Movie"}}},
		"u2": {{ID: "s2", Provider: domain.ProviderMegabox, Theater: domain.Theater{Name: "COEX"}, Movie: domain.Movie{Name: "Other"}}},
	}}
	handler := NewHandler(providers.New(), &fakeSubscriptions{}, store)

	choices, err := handler.DeleteChoices(context.Background(), "u1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 1 || choices[0].Value != "s1" {
		t.Fatalf("choices=%+v", choices)
	}
}

func TestRegisterRejectsArbitraryIDsBeforeWrite(t *testing.T) {
	provider := &catalogProvider{
		id:       domain.ProviderMegabox,
		movies:   []domain.Movie{{ID: "m1", Name: "Movie"}},
		theaters: []domain.Theater{{ID: "t1", Name: "Theater", AreaCode: "10"}},
	}
	subscriptions := &fakeSubscriptions{}
	handler := NewHandler(providers.New(provider), subscriptions, &fakeStore{})

	if _, err := handler.Register(context.Background(), "u1", domain.ProviderMegabox, "forged", "m1"); err == nil {
		t.Fatal("expected unknown theater error")
	}
	if subscriptions.registerCalls != 0 {
		t.Fatalf("register calls=%d", subscriptions.registerCalls)
	}
	if _, err := handler.Register(context.Background(), "u1", domain.ProviderMegabox, "t1", "forged"); err == nil {
		t.Fatal("expected unknown movie error")
	}
	if subscriptions.registerCalls != 0 {
		t.Fatalf("register calls=%d", subscriptions.registerCalls)
	}
}

type catalogProvider struct {
	id              domain.ProviderID
	movies          []domain.Movie
	theaters        []domain.Theater
	theaterSearches int
}

func (p *catalogProvider) ID() domain.ProviderID { return p.id }
func (p *catalogProvider) SearchMovies(context.Context, string) ([]domain.Movie, error) {
	return p.movies, nil
}
func (p *catalogProvider) SearchTheaters(context.Context, string) ([]domain.Theater, error) {
	p.theaterSearches++
	return p.theaters, nil
}
func (p *catalogProvider) FetchShowtimes(context.Context, string, string) ([]domain.Showtime, error) {
	return nil, nil
}
func (p *catalogProvider) BuildBookingLinks(string, string) domain.BookingLinks {
	return domain.BookingLinks{}
}

type fakeSubscriptions struct{ registerCalls int }

func (s *fakeSubscriptions) Register(_ context.Context, input subscription.RegisterInput) (database.Subscription, error) {
	s.registerCalls++
	return database.Subscription{DiscordUserID: input.DiscordUserID, Provider: input.Provider, Theater: input.Theater, Movie: input.Movie}, nil
}

type fakeStore struct {
	byUser map[string][]database.Subscription
}

func (s *fakeStore) ListSubscriptionsByUser(_ context.Context, userID string) ([]database.Subscription, error) {
	return s.byUser[userID], nil
}
func (s *fakeStore) DeleteSubscription(context.Context, string) error { return nil }
func (s *fakeStore) DeleteAllSubscriptionsByUser(context.Context, string) (int64, error) {
	return 0, nil
}
