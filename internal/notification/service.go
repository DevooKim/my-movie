package notification

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

var ErrChannelUnavailable = errors.New("discord channel unavailable")

type Session struct {
	StartsAt       string
	EndsAt         string
	Auditorium     string
	RemainingSeats int
	TotalSeats     int
	SeatCountKnown bool
}

type Alert struct {
	Provider       domain.ProviderID
	TargetID       string
	TheaterName    string
	AuditoriumName string
	MovieID        string
	MovieName      string
	PlayDate       string
	Sessions       []Session
	Links          domain.BookingLinks
}

type Notifier interface {
	SendAlert(context.Context, string, Alert) error
}

type LinkProvider interface {
	BuildBookingLinks(domain.AlertTarget, string) domain.BookingLinks
}

type TargetDisabler interface {
	DisableUnavailable(context.Context, string) error
}

type Service struct {
	repository *database.Repository
	notifier   Notifier
	providers  map[domain.ProviderID]LinkProvider
	disabler   TargetDisabler
}

func NewService(repository *database.Repository, notifier Notifier, providers map[domain.ProviderID]LinkProvider, disabler TargetDisabler) *Service {
	return &Service{repository: repository, notifier: notifier, providers: providers, disabler: disabler}
}

func (s *Service) DeliverPending(ctx context.Context) error {
	deliveries, err := s.repository.ListPendingChannelDeliveries(ctx)
	if err != nil {
		return err
	}
	groups := groupDeliveries(deliveries)
	var deliveryErrors []error
	for _, group := range groups {
		enabled, err := s.repository.IsTargetEnabled(ctx, group.targetID)
		if err != nil {
			deliveryErrors = append(deliveryErrors, err)
			continue
		}
		if !enabled {
			continue
		}
		provider, ok := s.providers[group.provider]
		if !ok {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("provider %q is not configured", group.provider))
			continue
		}
		target, found := targets.Find(group.targetID)
		if !found {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("target %q is unavailable", group.targetID))
			continue
		}
		alert := Alert{
			Provider: group.provider, TargetID: group.targetID,
			TheaterName: group.theaterName, AuditoriumName: target.AuditoriumName,
			MovieID: group.movieID, MovieName: group.movieName, PlayDate: group.playDate,
			Sessions: group.sessions, Links: provider.BuildBookingLinks(target, group.movieID),
		}
		err = s.notifier.SendAlert(ctx, group.channelID, alert)
		if err == nil {
			if markErr := s.repository.MarkChannelSent(ctx, group.targetID, group.showtimeIDs); markErr != nil {
				deliveryErrors = append(deliveryErrors, markErr)
			}
			continue
		}
		permanent := errors.Is(err, ErrChannelUnavailable)
		if markErr := s.repository.MarkChannelFailedAttempt(ctx, group.targetID, group.showtimeIDs, err.Error(), permanent); markErr != nil {
			deliveryErrors = append(deliveryErrors, markErr)
		}
		if permanent {
			if disableErr := s.disabler.DisableUnavailable(ctx, group.targetID); disableErr != nil {
				deliveryErrors = append(deliveryErrors, disableErr)
			}
		}
		deliveryErrors = append(deliveryErrors, err)
	}
	return errors.Join(deliveryErrors...)
}

type deliveryGroup struct {
	targetID    string
	channelID   string
	provider    domain.ProviderID
	theaterName string
	movieID     string
	movieName   string
	playDate    string
	showtimeIDs []string
	sessions    []Session
}

func groupDeliveries(deliveries []database.PendingChannelDelivery) []*deliveryGroup {
	groups := make(map[string]*deliveryGroup)
	var order []string
	for _, delivery := range deliveries {
		showtime := delivery.Showtime
		key := delivery.TargetID + "\x00" + showtime.MovieID + "\x00" + showtime.PlayDate
		group, ok := groups[key]
		if !ok {
			group = &deliveryGroup{
				targetID: delivery.TargetID, channelID: delivery.ChannelID,
				provider: showtime.Provider, theaterName: showtime.TheaterName,
				movieID: showtime.MovieID, movieName: showtime.MovieName, playDate: showtime.PlayDate,
			}
			groups[key] = group
			order = append(order, key)
		}
		group.showtimeIDs = append(group.showtimeIDs, showtime.ExternalID)
		group.sessions = append(group.sessions, Session{
			StartsAt: showtime.StartsAt, EndsAt: showtime.EndsAt, Auditorium: showtime.Auditorium,
			RemainingSeats: showtime.RemainingSeats, TotalSeats: showtime.TotalSeats, SeatCountKnown: showtime.SeatCountKnown,
		})
	}
	sort.Strings(order)
	result := make([]*deliveryGroup, 0, len(order))
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}
