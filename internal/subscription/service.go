package subscription

import (
	"context"
	"errors"
	"fmt"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/notification"
)

var ErrAlreadySubscribed = errors.New("already subscribed")

type RegisterInput = database.CreateSubscriptionInput

type Service struct {
	repository *database.Repository
	notifier   notification.Notifier
	providers  map[domain.ProviderID]domain.TheaterProvider
}

func NewService(repository *database.Repository, notifier notification.Notifier, providers map[domain.ProviderID]domain.TheaterProvider) *Service {
	return &Service{repository: repository, notifier: notifier, providers: providers}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (registered database.Subscription, err error) {
	provider, ok := s.providers[input.Provider]
	if !ok {
		return database.Subscription{}, fmt.Errorf("provider %q is not configured", input.Provider)
	}
	subscription, err := s.repository.CreateInitializingSubscription(ctx, input)
	if err != nil {
		var sqliteError interface{ Code() int }
		if errors.As(err, &sqliteError) && sqliteError.Code() == 2067 {
			return database.Subscription{}, fmt.Errorf("%w: %v", ErrAlreadySubscribed, err)
		}
		return database.Subscription{}, fmt.Errorf("create subscription: %w", err)
	}
	finished := false
	defer func() {
		if !finished {
			if cleanupErr := s.repository.DeleteSubscription(context.Background(), subscription.ID); cleanupErr != nil {
				err = fmt.Errorf("%w; clean up subscription: %v", err, cleanupErr)
			}
		}
	}()

	showtimes, err := provider.FetchShowtimes(ctx, input.Theater.ID, input.Movie.ID)
	if err != nil {
		return database.Subscription{}, fmt.Errorf("fetch initial showtimes: %w", err)
	}
	if err := s.repository.RecordScan(ctx, showtimes, subscription.ID); err != nil {
		return database.Subscription{}, fmt.Errorf("record initial showtimes: %w", err)
	}
	if err := s.notifier.SendRegistrationConfirmation(ctx, subscription.DiscordUserID, subscription); err != nil {
		return database.Subscription{}, fmt.Errorf("send registration confirmation: %w", err)
	}
	if err := s.repository.ActivateSubscription(ctx, subscription.ID); err != nil {
		return database.Subscription{}, fmt.Errorf("activate subscription: %w", err)
	}
	finished = true
	subscription.Status = database.StatusActive
	return subscription, nil
}
