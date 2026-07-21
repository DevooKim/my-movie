package pollalert

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

func TestReportSendsFailureOnceAndRecoveryOnce(t *testing.T) {
	store := &fakeStore{installation: database.Installation{ControlChannelID: "control", StatusChannelID: "status"}}
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
	want := []sentMessage{
		{channelID: "control", content: "⚠️ CGV · 용산아이파크몰 조회 실패\nupstream failed"},
		{channelID: "status", content: "⚠️ CGV · 용산아이파크몰 조회가 원활하지 않습니다"},
		{channelID: "control", content: "✅ CGV · 용산아이파크몰 조회가 정상화되었습니다"},
		{channelID: "status", content: "✅ CGV · 용산아이파크몰 조회가 정상화되었습니다"},
	}
	if !reflect.DeepEqual(messenger.messages, want) {
		t.Fatalf("messages=%v", messenger.messages)
	}
}

func TestReportKeepsControlAlertForInstallationWithoutStatusChannel(t *testing.T) {
	store := &fakeStore{installation: database.Installation{ControlChannelID: "control"}}
	messenger := &fakeMessenger{}
	service := New(store, messenger)
	group := database.PollingGroup{Provider: domain.ProviderMegabox, TheaterID: "1351"}
	if err := service.Report(context.Background(), group, "코엑스", errors.New("connection reset")); err != nil {
		t.Fatal(err)
	}
	want := []sentMessage{{channelID: "control", content: "⚠️ 메가박스 · 코엑스 조회 실패\nconnection reset"}}
	if !reflect.DeepEqual(messenger.messages, want) {
		t.Fatalf("messages=%v", messenger.messages)
	}
	state := store.states[string(domain.ProviderMegabox)+"1351"]
	if !state.ControlDelivered || state.StatusDelivered {
		t.Fatalf("delivery state without status channel=%+v", state)
	}
	store.installation.StatusChannelID = "status"
	if err := service.Report(context.Background(), group, "코엑스", errors.New("connection reset")); err != nil {
		t.Fatal(err)
	}
	if len(messenger.messages) != 2 || messenger.messages[1].channelID != "status" || messenger.messages[1].content != "⚠️ 메가박스 · 코엑스 조회가 원활하지 않습니다" {
		t.Fatalf("messages after status channel creation=%v", messenger.messages)
	}
}

func TestReportDoesNotRepeatSuccessfulChannelWhenOtherChannelFails(t *testing.T) {
	store := &fakeStore{installation: database.Installation{ControlChannelID: "control", StatusChannelID: "status"}}
	messenger := &fakeMessenger{failChannels: map[string]error{"status": errors.New("status unavailable")}}
	service := New(store, messenger)
	group := database.PollingGroup{Provider: domain.ProviderCGV, TheaterID: "0013"}
	if err := service.Report(context.Background(), group, "용산아이파크몰", errors.New("upstream failed")); err == nil {
		t.Fatal("expected status channel error")
	}
	state := store.states[string(domain.ProviderCGV)+"0013"]
	if !state.ControlDelivered || state.StatusDelivered {
		t.Fatalf("delivery state=%+v", state)
	}
	delete(messenger.failChannels, "status")
	if err := service.Report(context.Background(), group, "용산아이파크몰", errors.New("upstream failed")); err != nil {
		t.Fatalf("retry failed status channel: %v", err)
	}
	if len(messenger.messages) != 3 || messenger.messages[2].channelID != "status" {
		t.Fatalf("messages=%v", messenger.messages)
	}
	if err := service.Report(context.Background(), group, "용산아이파크몰", errors.New("upstream failed")); err != nil {
		t.Fatalf("completed transition: %v", err)
	}
	if len(messenger.messages) != 3 {
		t.Fatalf("duplicate messages=%v", messenger.messages)
	}
}

func TestRecoveryOnlySentWhereFailureWasDelivered(t *testing.T) {
	store := &fakeStore{installation: database.Installation{ControlChannelID: "control", StatusChannelID: "status"}}
	messenger := &fakeMessenger{failChannels: map[string]error{"status": errors.New("status unavailable")}}
	service := New(store, messenger)
	group := database.PollingGroup{Provider: domain.ProviderCGV, TheaterID: "0013"}
	if err := service.Report(context.Background(), group, "용산아이파크몰", errors.New("upstream failed")); err == nil {
		t.Fatal("expected status channel error")
	}
	delete(messenger.failChannels, "status")
	if err := service.Report(context.Background(), group, "용산아이파크몰", nil); err != nil {
		t.Fatal(err)
	}
	if len(messenger.messages) != 3 || messenger.messages[2].channelID != "control" || messenger.messages[2].content != "✅ CGV · 용산아이파크몰 조회가 정상화되었습니다" {
		t.Fatalf("messages=%v", messenger.messages)
	}
}

type fakeStore struct {
	installation database.Installation
	states       map[string]database.PollAlertState
}

func (s *fakeStore) GetInstallation(context.Context) (database.Installation, error) {
	return s.installation, nil
}
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

type sentMessage struct {
	channelID string
	content   string
}
type fakeMessenger struct {
	messages     []sentMessage
	failChannels map[string]error
}

func (m *fakeMessenger) SendControlMessage(_ context.Context, channelID, message string) error {
	m.messages = append(m.messages, sentMessage{channelID: channelID, content: message})
	return m.failChannels[channelID]
}
