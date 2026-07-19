package domain

import "context"

type TheaterProvider interface {
	ID() ProviderID
	SearchMovies(context.Context, string) ([]Movie, error)
	FetchShowtimes(context.Context, AlertTarget, string) ([]Showtime, error)
	BuildBookingLinks(AlertTarget, string) BookingLinks
}
