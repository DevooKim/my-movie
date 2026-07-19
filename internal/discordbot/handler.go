package discordbot

import (
	"context"
	"fmt"
	"strings"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/providers"
	"my-movie/internal/subscription"
)

const maxChoices = 25

type Choice struct {
	Name  string
	Value string
}

type SubscriptionManager interface {
	Register(context.Context, subscription.RegisterInput) (database.Subscription, error)
}

type SubscriptionStore interface {
	ListSubscriptionsByUser(context.Context, string) ([]database.Subscription, error)
	DeleteSubscription(context.Context, string) error
	DeleteAllSubscriptionsByUser(context.Context, string) (int64, error)
}

type Handler struct {
	registry      *providers.Registry
	subscriptions SubscriptionManager
	store         SubscriptionStore
}

func NewHandler(registry *providers.Registry, subscriptions SubscriptionManager, store SubscriptionStore) *Handler {
	return &Handler{registry: registry, subscriptions: subscriptions, store: store}
}

func (h *Handler) MovieChoices(ctx context.Context, providerID domain.ProviderID, query string) ([]Choice, error) {
	provider, ok := h.registry.Get(providerID)
	if !ok {
		return nil, fmt.Errorf("provider %q is unavailable", providerID)
	}
	movies, err := provider.SearchMovies(ctx, query)
	if err != nil {
		return nil, err
	}
	choices := make([]Choice, 0, min(len(movies), maxChoices))
	for _, movie := range movies {
		choices = append(choices, Choice{Name: movie.Name, Value: movie.ID})
		if len(choices) == maxChoices {
			break
		}
	}
	return choices, nil
}

func (h *Handler) TheaterChoices(ctx context.Context, providerID domain.ProviderID, query string) ([]Choice, error) {
	provider, ok := h.registry.Get(providerID)
	if !ok {
		return nil, fmt.Errorf("provider %q is unavailable", providerID)
	}
	theaters, err := provider.SearchTheaters(ctx, query)
	if err != nil {
		return nil, err
	}
	choices := make([]Choice, 0, min(len(theaters), maxChoices))
	for _, theater := range theaters {
		choices = append(choices, Choice{Name: theater.Name, Value: theater.ID})
		if len(choices) == maxChoices {
			break
		}
	}
	return choices, nil
}

func (h *Handler) DeleteChoices(ctx context.Context, userID, query string) ([]Choice, error) {
	subscriptions, err := h.store.ListSubscriptionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	choices := make([]Choice, 0, min(len(subscriptions), maxChoices))
	for _, item := range subscriptions {
		name := fmt.Sprintf("%s · %s · %s", providerName(item.Provider), item.Theater.Name, item.Movie.Name)
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		choices = append(choices, Choice{Name: truncate(name, 100), Value: item.ID})
		if len(choices) == maxChoices {
			break
		}
	}
	return choices, nil
}

func (h *Handler) Register(ctx context.Context, userID string, providerID domain.ProviderID, theaterID, movieID string) (database.Subscription, error) {
	provider, ok := h.registry.Get(providerID)
	if !ok {
		return database.Subscription{}, fmt.Errorf("provider %q is unavailable", providerID)
	}
	theaters, err := provider.SearchTheaters(ctx, "")
	if err != nil {
		return database.Subscription{}, err
	}
	theater, ok := findTheater(theaters, theaterID)
	if !ok {
		return database.Subscription{}, fmt.Errorf("unknown theater %q", theaterID)
	}
	movies, err := provider.SearchMovies(ctx, "")
	if err != nil {
		return database.Subscription{}, err
	}
	movie, ok := findMovie(movies, movieID)
	if !ok {
		return database.Subscription{}, fmt.Errorf("unknown movie %q", movieID)
	}
	return h.subscriptions.Register(ctx, subscription.RegisterInput{
		DiscordUserID: userID, Provider: providerID, Theater: theater, Movie: movie,
	})
}

func (h *Handler) List(ctx context.Context, userID string) ([]database.Subscription, error) {
	return h.store.ListSubscriptionsByUser(ctx, userID)
}

func (h *Handler) Delete(ctx context.Context, userID, subscriptionID string) error {
	items, err := h.store.ListSubscriptionsByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == subscriptionID {
			return h.store.DeleteSubscription(ctx, subscriptionID)
		}
	}
	return fmt.Errorf("subscription %q does not belong to this user", subscriptionID)
}

func (h *Handler) DeleteAll(ctx context.Context, userID string) (int64, error) {
	return h.store.DeleteAllSubscriptionsByUser(ctx, userID)
}

func findMovie(movies []domain.Movie, id string) (domain.Movie, bool) {
	for _, movie := range movies {
		if movie.ID == id {
			return movie, true
		}
	}
	return domain.Movie{}, false
}

func findTheater(theaters []domain.Theater, id string) (domain.Theater, bool) {
	for _, theater := range theaters {
		if theater.ID == id {
			return theater, true
		}
	}
	return domain.Theater{}, false
}

func providerName(id domain.ProviderID) string {
	if id == domain.ProviderMegabox {
		return "Megabox"
	}
	if id == domain.ProviderCGV {
		return "CGV"
	}
	return string(id)
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
