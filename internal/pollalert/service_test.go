package pollalert

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

func TestReportSendsFailureOnceAndRecoveryOnce(t *testing.T) {
	store := &fakeStore{installation: database.Installation{ControlChannelID: "control"}}
	messenger := &fakeMessenger{}
	service := New(store, messenger)
	group := database.PollingGroup{Provider: domain.ProviderCGV, TheaterID: "0013"}
	if err := service.Report(context.Background(), group, "용산아이파크몰", errors.New("upstream failed")); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), group, "용산아이파크몰", errors.New("upstream failed")); err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), group, "용산아이파크몰", nil); err != nil {
		t.Fatal(err)
	}
	if len(messenger.messages) != 2 {
		t.Fatalf("messages=%v", messenger.messages)
	}
}

type fakeStore struct {
	installation database.Installation
	states       map[string]database.PollAlertState
}
func (s *fakeStore) GetInstallation(context.Context) (database.Installation, error) { return s.installation, nil }
func (s *fakeStore) GetPollAlertState(_ context.Context, provider domain.ProviderID, theaterID string) (database.PollAlertState, error) {
	state, ok := s.states[string(provider)+theaterID]
	if !ok {
		return database.PollAlertState{}, sql.ErrNoRows
	}
	return state, nil
}
func (s *fakeStore) SavePollAlertState(_ context.Context, state database.PollAlertState) error {
	if s.states == nil {
		s.states = map[string]database.PollAlertState{}
	}
	s.states[string(state.Provider)+state.TheaterID] = state
	return nil
}
type fakeMessenger struct{ messages []string }
func (m *fakeMessenger) SendControlMessage(_ context.Context, _ string, message string) error { m.messages = append(m.messages, message); return nil }
