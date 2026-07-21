package pollalert

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

type Store interface {
	GetInstallation(context.Context) (database.Installation, error)
	GetPollAlertState(context.Context, domain.ProviderID, string) (database.PollAlertState, error)
	SavePollAlertState(context.Context, database.PollAlertState) error
}

type Messenger interface {
	SendControlMessage(context.Context, string, string) error
}

type Service struct {
	store     Store
	messenger Messenger
}

func New(store Store, messenger Messenger) *Service {
	return &Service{store: store, messenger: messenger}
}

func (s *Service) Report(ctx context.Context, group database.PollingGroup, theaterName string, fetchErr error) error {
	installation, err := s.store.GetInstallation(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	state, err := s.store.GetPollAlertState(ctx, group.Provider, group.TheaterID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	stateMissing := errors.Is(err, sql.ErrNoRows)
	failed := fetchErr != nil
	if !failed && stateMissing {
		return nil
	}
	sameTransition := !stateMissing && state.Failed == failed
	if sameTransition && state.ControlDelivered && (state.StatusDelivered || installation.StatusChannelID == "") {
		return nil
	}
	provider := string(group.Provider)
	if group.Provider == "cgv" {
		provider = "CGV"
	}
	if group.Provider == "megabox" {
		provider = "메가박스"
	}
	summary := ""
	if failed {
		if sameTransition && state.ErrorSummary != "" {
			summary = state.ErrorSummary
		} else {
			summary = truncate(fetchErr.Error(), 500)
		}
	}
	message := fmt.Sprintf("✅ %s · %s 조회가 정상화되었습니다", provider, theaterName)
	statusMessage := message
	if failed {
		message = fmt.Sprintf("⚠️ %s · %s 조회 실패\n%s", provider, theaterName, summary)
		statusMessage = fmt.Sprintf("⚠️ %s · %s 조회가 원활하지 않습니다", provider, theaterName)
	}
	controlDelivered := false
	statusDelivered := installation.StatusChannelID == "" && !failed
	if sameTransition {
		controlDelivered = state.ControlDelivered
		statusDelivered = state.StatusDelivered
	} else if !stateMissing && !failed {
		controlDelivered = !state.ControlDelivered
		statusDelivered = installation.StatusChannelID == "" || !state.StatusDelivered
	}
	var sendErrors []error
	if !controlDelivered {
		if err := s.messenger.SendControlMessage(ctx, installation.ControlChannelID, message); err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("send control alert: %w", err))
		} else {
			controlDelivered = true
		}
	}
	if !statusDelivered && installation.StatusChannelID != "" {
		if err := s.messenger.SendControlMessage(ctx, installation.StatusChannelID, statusMessage); err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("send public status alert: %w", err))
		} else {
			statusDelivered = true
		}
	}
	if err := s.store.SavePollAlertState(ctx, database.PollAlertState{
		Provider: group.Provider, TheaterID: group.TheaterID, Failed: failed, ErrorSummary: summary,
		ControlDelivered: controlDelivered, StatusDelivered: statusDelivered,
	}); err != nil {
		sendErrors = append(sendErrors, err)
	}
	return errors.Join(sendErrors...)
}

func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum] + "…"
}
