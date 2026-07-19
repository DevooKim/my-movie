package domain

import "context"

// PreparedBranchPoll reuses a provider-specific prepared resource during one
// scheduled polling burst.
type PreparedBranchPoll interface {
	Fetch(context.Context) ([]Showtime, error)
	Close() error
}

// BranchPreparer optionally prepares a branch before its scheduled burst.
type BranchPreparer interface {
	PrepareBranch(context.Context, Branch) (PreparedBranchPoll, error)
}
