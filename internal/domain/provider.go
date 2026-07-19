package domain

import "context"

type TheaterProvider interface {
	ID() ProviderID
	FetchBranchSnapshot(context.Context, Branch) ([]Showtime, error)
	BuildBookingLinks(AlertTarget, string) BookingLinks
}
