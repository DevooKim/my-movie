package domain

import "context"

type TheaterProvider interface {
	ID() ProviderID
	SearchMovies(context.Context, string) ([]Movie, error)
	SearchTheaters(context.Context, string) ([]Theater, error)
	FetchShowtimes(context.Context, string, string) ([]Showtime, error)
	BuildBookingLinks(string, string) BookingLinks
}
