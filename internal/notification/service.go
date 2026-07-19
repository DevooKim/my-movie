package notification

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

var ErrDMUnavailable = errors.New("discord DM unavailable")

type Alert struct {
	Provider    domain.ProviderID
	Theater     domain.Theater
	Movie       domain.Movie
	PlayDate    string
	Times       []string
	Auditoriums []string
	Links       domain.BookingLinks
}

type Notifier interface {
	SendRegistrationConfirmation(context.Context, string, database.Subscription) error
	SendAlert(context.Context, string, Alert) error
}

type Service struct {
	repository *database.Repository
	notifier   Notifier
	providers  map[domain.ProviderID]domain.TheaterProvider
}

func NewService(repository *database.Repository, notifier Notifier, providers map[domain.ProviderID]domain.TheaterProvider) *Service {
	return &Service{repository: repository, notifier: notifier, providers: providers}
}

func (s *Service) DeliverPending(ctx context.Context) error {
	deliveries, err := s.repository.ListPendingDeliveries(ctx)
	if err != nil {
		return err
	}
	groups := groupDeliveries(deliveries)
	var deliveryErrors []error
	for _, group := range groups {
		provider, ok := s.providers[group.subscription.Provider]
		if !ok {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("provider %q is not configured", group.subscription.Provider))
			continue
		}
		alert := Alert{
			Provider: group.subscription.Provider, Theater: group.subscription.Theater,
			Movie: group.subscription.Movie, PlayDate: group.playDate,
			Times: group.times, Auditoriums: group.auditoriums,
			Links: provider.BuildBookingLinks(group.subscription.Theater.ID, group.subscription.Movie.ID),
		}
		err := s.notifier.SendAlert(ctx, group.subscription.DiscordUserID, alert)
		if err == nil {
			if markErr := s.repository.MarkSent(ctx, group.subscription.ID, group.keys); markErr != nil {
				deliveryErrors = append(deliveryErrors, markErr)
			}
			continue
		}
		permanent := errors.Is(err, ErrDMUnavailable)
		if markErr := s.repository.MarkFailedAttempt(ctx, group.subscription.ID, group.keys, err.Error(), permanent); markErr != nil {
			deliveryErrors = append(deliveryErrors, markErr)
		}
		if permanent {
			if disableErr := s.repository.DisableSubscription(ctx, group.subscription.ID); disableErr != nil {
				deliveryErrors = append(deliveryErrors, disableErr)
			}
		}
		deliveryErrors = append(deliveryErrors, err)
	}
	return errors.Join(deliveryErrors...)
}

type deliveryGroup struct {
	subscription database.Subscription
	playDate     string
	keys         []string
	times        []string
	auditoriums  []string
}

func groupDeliveries(deliveries []database.PendingDelivery) []*deliveryGroup {
	groups := make(map[string]*deliveryGroup)
	var order []string
	for _, delivery := range deliveries {
		key := delivery.Subscription.ID + "\x00" + delivery.PlayDate
		group, ok := groups[key]
		if !ok {
			group = &deliveryGroup{subscription: delivery.Subscription, playDate: delivery.PlayDate}
			groups[key] = group
			order = append(order, key)
		}
		group.keys = append(group.keys, delivery.ShowtimeKey)
		group.times = append(group.times, delivery.StartsAt)
		group.auditoriums = append(group.auditoriums, delivery.Auditorium)
	}
	sort.Strings(order)
	result := make([]*deliveryGroup, 0, len(order))
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}
