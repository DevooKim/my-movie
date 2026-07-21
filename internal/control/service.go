package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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
	EnsureViewerRole(context.Context, string, string, string) (string, bool, error)
	EnsureRestrictedCategory(context.Context, string, string, string, string) (string, bool, error)
	EnsurePrivateTextChannel(context.Context, string, string, string, string, string) (string, bool, error)
	EnsurePublicTextChannel(context.Context, string, string, string, string) (string, bool, error)
	EnsureRestrictedTextChannel(context.Context, string, string, string, string, string) (string, bool, error)
	UpsertGuideImage(context.Context, string, string) (string, bool, error)
	UpsertGuide(context.Context, string, string, string) (string, bool, error)
	UpsertPanel(context.Context, string, string, Panel) (string, bool, error)
	AddMemberRole(context.Context, string, string, string) error
	SendAnnouncement(context.Context, string, string) error
	DeleteRole(context.Context, string, string) error
	DeleteChannel(context.Context, string) error
	DeleteMessage(context.Context, string, string) error
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
	previousCategoryID := installation.CategoryID
	controlSecured := false
	var created bool
	if previousCategoryID != "" && installation.ControlChannelID != "" {
		installation.ControlChannelID, _, err = s.channels.EnsurePrivateTextChannel(ctx, guildID, previousCategoryID, installation.ControlChannelID, "제어", ownerID)
		if err != nil {
			return database.Installation{}, fmt.Errorf("secure control channel before publishing category: %w", err)
		}
		controlSecured = true
	}
	previousViewerRoleID := installation.ViewerRoleID
	installation.ViewerRoleID, created, err = s.channels.EnsureViewerRole(ctx, guildID, previousViewerRoleID, "영화 알림")
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure alert viewer role: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteRole(cleanupCtx, guildID, installation.ViewerRoleID)
			})
		}
		return database.Installation{}, err
	}
	previousNoticeChannelID := installation.NoticeChannelID
	installation.NoticeChannelID, created, err = s.channels.EnsurePublicTextChannel(ctx, guildID, "", previousNoticeChannelID, "공지")
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure notice channel: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteChannel(cleanupCtx, installation.NoticeChannelID)
			})
		}
		return database.Installation{}, err
	}
	previousGuideChannelID := installation.GuideChannelID
	installation.GuideChannelID, created, err = s.channels.EnsurePublicTextChannel(ctx, guildID, "", previousGuideChannelID, "안내")
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure guide channel: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteChannel(cleanupCtx, installation.GuideChannelID)
			})
		}
		return database.Installation{}, err
	}
	previousGuideImageMessageID := installation.GuideImageMessageID
	installation.GuideImageMessageID, created, err = s.channels.UpsertGuideImage(ctx, installation.GuideChannelID, previousGuideImageMessageID)
	if err != nil {
		return database.Installation{}, fmt.Errorf("upsert guide image message: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteMessage(cleanupCtx, installation.GuideChannelID, installation.GuideImageMessageID)
			})
		}
		return database.Installation{}, err
	}
	previousGuideMessageID := installation.GuideMessageID
	installation.GuideMessageID, created, err = s.channels.UpsertGuide(ctx, installation.GuideChannelID, previousGuideMessageID, installation.GuideImageMessageID)
	if err != nil {
		return database.Installation{}, fmt.Errorf("upsert guide message: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteMessage(cleanupCtx, installation.GuideChannelID, installation.GuideMessageID)
			})
		}
		return database.Installation{}, err
	}
	previousCategoryID = installation.CategoryID
	installation.CategoryID, created, err = s.channels.EnsureRestrictedCategory(ctx, guildID, previousCategoryID, categoryName, installation.ViewerRoleID)
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure restricted alert category: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteChannel(cleanupCtx, installation.CategoryID)
			})
		}
		return database.Installation{}, err
	}
	if !controlSecured || installation.CategoryID != previousCategoryID {
		installation.ControlChannelID, created, err = s.channels.EnsurePrivateTextChannel(ctx, guildID, installation.CategoryID, installation.ControlChannelID, "제어", ownerID)
		if err != nil {
			return database.Installation{}, fmt.Errorf("ensure control channel: %w", err)
		}
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteChannel(cleanupCtx, installation.ControlChannelID)
			})
		}
		return database.Installation{}, err
	}
	previousStatusChannelID := installation.StatusChannelID
	installation.StatusChannelID, created, err = s.channels.EnsureRestrictedTextChannel(ctx, guildID, installation.CategoryID, previousStatusChannelID, "서버-상태", installation.ViewerRoleID)
	if err != nil {
		return database.Installation{}, fmt.Errorf("ensure status channel: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteChannel(cleanupCtx, installation.StatusChannelID)
			})
		}
		return database.Installation{}, err
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
		state.ChannelID, created, err = s.channels.EnsureRestrictedTextChannel(ctx, guildID, installation.CategoryID, state.ChannelID, channelName(target.ID), installation.ViewerRoleID)
		if err != nil {
			return database.Installation{}, fmt.Errorf("ensure target channel %s: %w", target.ID, err)
		}
		if err := s.store.SaveTargetState(ctx, state); err != nil {
			if created {
				err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error { return s.channels.DeleteChannel(cleanupCtx, state.ChannelID) })
			}
			return database.Installation{}, err
		}
		byID[target.ID] = state
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		return database.Installation{}, err
	}
	panel := panelFromStates(byID, "")
	previousControlMessageID := installation.ControlMessageID
	installation.ControlMessageID, created, err = s.channels.UpsertPanel(ctx, installation.ControlChannelID, previousControlMessageID, panel)
	if err != nil {
		return database.Installation{}, fmt.Errorf("upsert control panel: %w", err)
	}
	if err := s.store.SaveInstallation(ctx, installation); err != nil {
		if created {
			err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
				return s.channels.DeleteMessage(cleanupCtx, installation.ControlChannelID, installation.ControlMessageID)
			})
		}
		return database.Installation{}, err
	}
	return installation, nil
}

func cleanupFailure(ctx context.Context, primary error, cleanup func(context.Context) error) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return errors.Join(primary, cleanup(cleanupCtx))
}

func (s *Service) JoinAlerts(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation, err := s.store.GetInstallation(ctx)
	if err != nil {
		return err
	}
	if installation.GuildID == "" || installation.ViewerRoleID == "" {
		return errors.New("alert channels are not initialized")
	}
	return s.channels.AddMemberRole(ctx, installation.GuildID, userID, installation.ViewerRoleID)
}

func (s *Service) Announce(ctx context.Context, ownerID, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation, err := s.authorize(ctx, ownerID)
	if err != nil {
		return err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("announcement content is empty")
	}
	if installation.NoticeChannelID == "" {
		return errors.New("notice channel is not initialized")
	}
	return s.channels.SendAnnouncement(ctx, installation.NoticeChannelID, "📢 **공지**\n"+content)
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
	messageID, created, err := s.channels.UpsertPanel(ctx, installation.ControlChannelID, installation.ControlMessageID, panelFromStates(byID, selectedTargetID))
	if err != nil {
		return err
	}
	if messageID != installation.ControlMessageID {
		installation.ControlMessageID = messageID
		if err := s.store.SaveInstallation(ctx, installation); err != nil {
			if created {
				err = cleanupFailure(ctx, err, func(cleanupCtx context.Context) error {
					return s.channels.DeleteMessage(cleanupCtx, installation.ControlChannelID, messageID)
				})
			}
			return err
		}
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
