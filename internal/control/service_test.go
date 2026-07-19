package control

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

func TestInitializeCreatesPrivateChannelTreeAndPersistsOwner(t *testing.T) {
	store := newFakeStore()
	channels := &fakeChannels{}
	service := New(store, channels, nil)

	installation, err := service.Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if channels.categoryName != "영화 예매 알림" || channels.categoryOwner != "owner" {
		t.Fatalf("category name=%q owner=%q", channels.categoryName, channels.categoryOwner)
	}
	wantNames := []string{"제어", "메가박스-코엑스-돌비", "메가박스-남현아-돌비", "cgv-용산-imax", "cgv-용산-4dx", "cgv-용산-screenx"}
	if !reflect.DeepEqual(channels.textNames, wantNames) {
		t.Fatalf("channels=%v want=%v", channels.textNames, wantNames)
	}
	if installation.OwnerUserID != "owner" || installation.ControlMessageID == "" {
		t.Fatalf("installation=%+v", installation)
	}
	if len(store.states) != 5 {
		t.Fatalf("states=%+v", store.states)
	}
	for _, state := range store.states {
		if state.Enabled {
			t.Fatalf("initial target is enabled: %+v", state)
		}
	}
	if len(channels.panels) != 1 || len(channels.panels[0].Targets) != 5 {
		t.Fatalf("panels=%+v", channels.panels)
	}
}

func TestInitializeReusesSavedChannelsAndRejectsAnotherOwner(t *testing.T) {
	store := newFakeStore()
	channels := &fakeChannels{}
	service := New(store, channels, nil)
	first, err := service.Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	channels.resetCalls()
	second, err := service.Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if first.CategoryID != second.CategoryID || len(channels.createdIDs) != 0 {
		t.Fatalf("first=%+v second=%+v newly-created=%v", first, second, channels.createdIDs)
	}
	if _, err := service.Initialize(context.Background(), "guild", "intruder"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("error=%v", err)
	}
}

func TestEnableCapturesCurrentTargetAsBaselineBeforeTurningOn(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner"}
	store.states["cgv-yongsan-imax"] = database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax"}
	provider := &fakeBranchProvider{id: domain.ProviderCGV, showtimes: []domain.Showtime{
		{Provider: domain.ProviderCGV, TargetID: "cgv-yongsan-imax", ExternalID: "existing"},
		{Provider: domain.ProviderCGV, TargetID: "cgv-yongsan-4dx", ExternalID: "other-target"},
	}}
	service := New(store, &fakeChannels{}, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider})

	if err := service.Enable(context.Background(), "owner", "cgv-yongsan-imax"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.baselines["cgv-yongsan-imax"], []string{"existing"}) {
		t.Fatalf("baseline=%v", store.baselines)
	}
	if !store.states["cgv-yongsan-imax"].Enabled || provider.calls != 1 {
		t.Fatalf("state=%+v provider calls=%d", store.states["cgv-yongsan-imax"], provider.calls)
	}
}

func TestEnableFailureAndNonOwnerLeaveTargetOff(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner"}
	store.states["cgv-yongsan-imax"] = database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax"}
	provider := &fakeBranchProvider{id: domain.ProviderCGV, err: errors.New("upstream unavailable")}
	service := New(store, &fakeChannels{}, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider})

	if err := service.Enable(context.Background(), "intruder", "cgv-yongsan-imax"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-owner error=%v", err)
	}
	if err := service.Enable(context.Background(), "owner", "cgv-yongsan-imax"); err == nil {
		t.Fatal("expected provider error")
	}
	if store.states["cgv-yongsan-imax"].Enabled || len(store.baselines) != 0 {
		t.Fatalf("state=%+v baselines=%v", store.states["cgv-yongsan-imax"], store.baselines)
	}
}

func TestDisableUnavailableTurnsTargetOffAndRefreshesPanel(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner", ControlChannelID: "control", ControlMessageID: "panel"}
	store.states["cgv-yongsan-imax"] = database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}
	channels := &fakeChannels{}
	service := New(store, channels, nil)

	if err := service.DisableUnavailable(context.Background(), "cgv-yongsan-imax"); err != nil {
		t.Fatal(err)
	}
	if store.states["cgv-yongsan-imax"].Enabled || len(channels.panels) != 1 {
		t.Fatalf("state=%+v panels=%+v", store.states["cgv-yongsan-imax"], channels.panels)
	}
}

type fakeStore struct {
	installation *database.Installation
	states       map[string]database.TargetState
	baselines    map[string][]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{states: map[string]database.TargetState{}, baselines: map[string][]string{}}
}
func (s *fakeStore) GetInstallation(context.Context) (database.Installation, error) {
	if s.installation == nil {
		return database.Installation{}, sql.ErrNoRows
	}
	return *s.installation, nil
}
func (s *fakeStore) SaveInstallation(_ context.Context, installation database.Installation) error {
	copy := installation
	s.installation = &copy
	return nil
}
func (s *fakeStore) SaveTargetState(_ context.Context, state database.TargetState) error {
	s.states[state.TargetID] = state
	return nil
}
func (s *fakeStore) ListTargetStates(context.Context) ([]database.TargetState, error) {
	states := make([]database.TargetState, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}
	return states, nil
}
func (s *fakeStore) ReplaceBaseline(_ context.Context, targetID string, ids []string) error {
	s.baselines[targetID] = append([]string(nil), ids...)
	return nil
}

type fakeChannels struct {
	categoryName  string
	categoryOwner string
	textNames     []string
	createdIDs    []string
	panels        []Panel
}

func (c *fakeChannels) EnsurePrivateCategory(_ context.Context, _ string, existingID, name, ownerID string) (string, error) {
	c.categoryName, c.categoryOwner = name, ownerID
	if existingID != "" {
		return existingID, nil
	}
	id := "category"
	c.createdIDs = append(c.createdIDs, id)
	return id, nil
}
func (c *fakeChannels) EnsurePrivateTextChannel(_ context.Context, _ string, _ string, existingID, name, _ string) (string, error) {
	c.textNames = append(c.textNames, name)
	if existingID != "" {
		return existingID, nil
	}
	id := "channel-" + name
	c.createdIDs = append(c.createdIDs, id)
	return id, nil
}
func (c *fakeChannels) UpsertPanel(_ context.Context, _ string, existingID string, panel Panel) (string, error) {
	c.panels = append(c.panels, panel)
	if existingID != "" {
		return existingID, nil
	}
	return "panel", nil
}
func (c *fakeChannels) resetCalls() {
	c.textNames = nil
	c.createdIDs = nil
	c.panels = nil
}

type fakeBranchProvider struct {
	id        domain.ProviderID
	showtimes []domain.Showtime
	err       error
	calls     int
}

func (p *fakeBranchProvider) ID() domain.ProviderID { return p.id }
func (p *fakeBranchProvider) FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error) {
	p.calls++
	return p.showtimes, p.err
}
