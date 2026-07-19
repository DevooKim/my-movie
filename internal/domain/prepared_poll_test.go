package domain

import "context"

type testPreparedPoll struct{}

func (testPreparedPoll) Fetch(context.Context) ([]Showtime, error) { return nil, nil }
func (testPreparedPoll) Close() error                              { return nil }

var _ PreparedBranchPoll = testPreparedPoll{}
