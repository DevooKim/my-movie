package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

var ErrNotOwner = errors.New("only the alert owner can perform this action")

const categoryName = "영화 예매 알림"

type Store interface {
	GetInstallation(context.Context) (database.Installation, error)
	SaveInstallation(context.Context, database.Installation) error
	SaveTargetState(context.Context, database.TargetState) error
	ListTargetStates(context.Context) ([]database.TargetState, error)
	ReplaceBaseline(context.Context, string, []string) error
}

type Channels interface {
	EnsurePrivateCategory(context.Context, string, string, string, string) (string, error)
	EnsurePrivateTextChannel(context.Context, string, string, string, string, string) (string, error)
	UpsertPanel(context.Context, string, string, Panel) (string, error)
}

type BranchProvider interface {
	ID() domain.ProviderID
	FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error)
}

type TargetStatus struct {
	ID      string
	Name    string
	Enabled bool
}

type Panel struct {
	Targets          []TargetStatus
	SelectedTargetID string
}

type Service struct {
	store     Store
	channels  Channels
	providers map[domain.ProviderID]BranchProvider
	mu        sync.Mutex
}

func New(store Store, channels Channels, providers map[domain.ProviderID]BranchProvider) *Service {
	return &Service{store: store, channels: channels, providers: providers}
}

func (s *Service) Initialize(ctx context.Context, guildID, ownerID string) (database.Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	installation, err := s.store.GetInstallation(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return database.Installation{}, err
	}
	if err == nil && (installation.GuildID != guildID || installation.OwnerUserID != ownerID) {
		return database.Installation{}, ErrNotOwner
	}
	installation.GuildID = guildID
	installation.OwnerUserID = ownerID
	installation.CategoryID, err = s.channels.EnsurePrivateCategory(ctx, guildID, installation.CategoryID, categoryName, ownerID)
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure alert category: %w", err)
	}
	installation.ControlChannelID, err = s.channels.EnsurePrivateTextChannel(ctx, guildID, installation.CategoryID, installation.ControlChannelID, "제어", ownerID)
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure control channel: %w", err)
	}

	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return database.Installation{}, err
	}
	byID := make(map[string]database.TargetState, len(states))
	for _, state := range states {
		byID[state.TargetID] = state
	}
	for _, target := range targets.All() {
		state := byID[target.ID]
		state.TargetID = target.ID
		state.ChannelID, err = s.channels.EnsurePrivateTextChannel(ctx, guildID, installation.CategoryID, state.ChannelID, channelName(target.ID), ownerID)
		if err != nil {
			return database.Installation{}, fmt.Errorf("ensure target channel %s: %w", target.ID, err)
		}
		if err := s.store.SaveTargetState(ctx, state); err != nil {
			return database.Installation{}, err
		}
		byID[target.ID] = state
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		return database.Installation{}, err
	}
	panel := panelFromStates(byID, "")
	installation.ControlMessageID, err = s.channels.UpsertPanel(ctx, installation.ControlChannelID, installation.ControlMessageID, panel)
	if err != nil {
		return database.Installation{}, fmt.Errorf("upsert control panel: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		return database.Installation{}, err
	}
	return installation, nil
}

func (s *Service) Enable(ctx context.Context, ownerID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation, err := s.authorize(ctx, ownerID)
	if err != nil {
		return err
	}
	target, ok := targets.Find(targetID)
	if !ok {
		return fmt.Errorf("unknown target %q", targetID)
	}
	provider, ok := s.providers[target.Provider]
	if !ok {
		return fmt.Errorf("provider %q is unavailable", target.Provider)
	}
	showtimes, err := provider.FetchBranchSnapshot(ctx, domain.Branch{
		Provider: target.Provider, TheaterID: target.Theater.ID,
		TheaterName: target.Theater.Name, AreaCode: target.Theater.AreaCode,
	})
	if err != nil {
		return fmt.Errorf("fetch current %s schedule: %w", target.DisplayName(), err)
	}
	ids := make([]string, 0, len(showtimes))
	for _, showtime := range showtimes {
		if showtime.TargetID == targetID {
			ids = append(ids, showtime.ExternalID)
		}
	}
	if err := s.store.ReplaceBaseline(ctx, targetID, ids); err != nil {
		return err
	}
	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return err
	}
	state, ok := findState(states, targetID)
	if !ok {
		return fmt.Errorf("target %q is not initialized", targetID)
	}
	state.Enabled = true
	if err := s.store.SaveTargetState(ctx, state); err != nil {
		return err
	}
	return s.refreshPanel(ctx, installation, targetID)
}

func (s *Service) Disable(ctx context.Context, ownerID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation, err := s.authorize(ctx, ownerID)
	if err != nil {
		return err
	}
	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return err
	}
	state, ok := findState(states, targetID)
	if !ok {
		return fmt.Errorf("target %q is not initialized", targetID)
	}
	state.Enabled = false
	if err := s.store.SaveTargetState(ctx, state); err != nil {
		return err
	}
	return s.refreshPanel(ctx, installation, targetID)
}

func (s *Service) DisableUnavailable(ctx context.Context, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation, err := s.store.GetInstallation(ctx)
	if err != nil {
		return err
	}
	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return err
	}
	state, ok := findState(states, targetID)
	if !ok {
		return fmt.Errorf("target %q is not initialized", targetID)
	}
	state.Enabled = false
	if err := s.store.SaveTargetState(ctx, state); err != nil {
		return err
	}
	return s.refreshPanel(ctx, installation, targetID)
}

func (s *Service) SelectTarget(ctx context.Context, ownerID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation, err := s.authorize(ctx, ownerID)
	if err != nil {
		return err
	}
	if _, ok := targets.Find(targetID); !ok {
		return fmt.Errorf("unknown target %q", targetID)
	}
	return s.refreshPanel(ctx, installation, targetID)
}

func (s *Service) authorize(ctx context.Context, ownerID string) (database.Installation, error) {
	installation, err := s.store.GetInstallation(ctx)
	if err != nil {
		return database.Installation{}, err
	}
	if installation.OwnerUserID != ownerID {
		return database.Installation{}, ErrNotOwner
	}
	return installation, nil
}

func (s *Service) refreshPanel(ctx context.Context, installation database.Installation, selectedTargetID string) error {
	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]database.TargetState, len(states))
	for _, state := range states {
		byID[state.TargetID] = state
	}
	messageID, err := s.channels.UpsertPanel(ctx, installation.ControlChannelID, installation.ControlMessageID, panelFromStates(byID, selectedTargetID))
	if err != nil {
		return err
	}
	if messageID != installation.ControlMessageID {
		installation.ControlMessageID = messageID
		return s.store.SaveInstallation(ctx, installation)
	}
	return nil
}

func panelFromStates(states map[string]database.TargetState, selectedTargetID string) Panel {
	panel := Panel{SelectedTargetID: selectedTargetID, Targets: make([]TargetStatus, 0, len(targets.All()))}
	for _, target := range targets.All() {
		panel.Targets = append(panel.Targets, TargetStatus{ID: target.ID, Name: target.DisplayName(), Enabled: states[target.ID].Enabled})
	}
	return panel
}

func findState(states []database.TargetState, targetID string) (database.TargetState, bool) {
	for _, state := range states {
		if state.TargetID == targetID {
			return state, true
		}
	}
	return database.TargetState{}, false
}

func channelName(targetID string) string {
	switch targetID {
	case "megabox-coex-dolby":
		return "메가박스-코엑스-돌비"
	case "megabox-namhyeona-dolby":
		return "메가박스-남현아-돌비"
	case "cgv-yongsan-imax":
		return "cgv-용산-imax"
	case "cgv-yongsan-4dx":
		return "cgv-용산-4dx"
	case "cgv-yongsan-screenx":
		return "cgv-용산-screenx"
	default:
		panic("unknown target: " + targetID)
	}
}
