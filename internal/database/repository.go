package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

type DeliveryStatus string

const (
	DeliveryPending DeliveryStatus = "pending"
	DeliverySent    DeliveryStatus = "sent"
	DeliveryFailed  DeliveryStatus = "failed"
)

type PollingGroup struct {
	Provider  domain.ProviderID
	TargetID  string
	TheaterID string
	MovieID   string
}

type PollRun struct {
	Group         PollingGroup
	StartedAt     time.Time
	FinishedAt    time.Time
	Succeeded     bool
	ShowtimeCount int
	ErrorSummary  string
}

type PollAlertState struct {
	Provider         domain.ProviderID
	TheaterID        string
	Failed           bool
	ErrorSummary     string
	ControlDelivered bool
	StatusDelivered  bool
	UpdatedAt        time.Time
}

type Installation struct {
	GuildID             string
	OwnerUserID         string
	ViewerRoleID        string
	NoticeChannelID     string
	GuideChannelID      string
	GuideImageMessageID string
	GuideMessageID      string
	CategoryID          string
	ControlChannelID    string
	ControlMessageID    string
	StatusChannelID     string
}

type TargetState struct {
	TargetID   string
	ChannelID  string
	Enabled    bool
	UpdatedAt  time.Time
	Generation int64
}

type PendingChannelDelivery struct {
	TargetID     string
	ChannelID    string
	Showtime     domain.Showtime
	AttemptCount int
}

type ChannelDelivery struct {
	TargetID     string
	ShowtimeID   string
	Status       DeliveryStatus
	AttemptCount int
}

type Repository struct {
	database *sql.DB
	now      func() time.Time
}

func NewRepository(database *sql.DB, now func() time.Time) *Repository {
	return &Repository{database: database, now: now}
}

func (r *Repository) SaveInstallation(ctx context.Context, installation Installation) error {
	_, err := r.database.ExecContext(ctx, `
		INSERT INTO installations(guild_id, owner_user_id, viewer_role_id, notice_channel_id, guide_channel_id, guide_image_message_id, guide_message_id, category_id, control_channel_id, control_message_id, status_channel_id, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(guild_id) DO UPDATE SET
		  owner_user_id=excluded.owner_user_id,
		  viewer_role_id=excluded.viewer_role_id,
		  notice_channel_id=excluded.notice_channel_id,
		  guide_channel_id=excluded.guide_channel_id,
		  guide_image_message_id=excluded.guide_image_message_id,
		  guide_message_id=excluded.guide_message_id,
		  category_id=excluded.category_id,
		  control_channel_id=excluded.control_channel_id,
		  control_message_id=excluded.control_message_id,
		  status_channel_id=excluded.status_channel_id,
		  updated_at=excluded.updated_at`,
		installation.GuildID, installation.OwnerUserID, installation.ViewerRoleID,
		installation.NoticeChannelID, installation.GuideChannelID, installation.GuideImageMessageID, installation.GuideMessageID, installation.CategoryID,
		installation.ControlChannelID, installation.ControlMessageID, installation.StatusChannelID, formatTime(r.now()),
	)
	return err
}

func (r *Repository) GetInstallation(ctx context.Context) (Installation, error) {
	var installation Installation
	err := r.database.QueryRowContext(ctx, `
		SELECT guild_id, owner_user_id, viewer_role_id, notice_channel_id, guide_channel_id, guide_image_message_id, guide_message_id, category_id, control_channel_id, control_message_id, status_channel_id
		FROM installations ORDER BY updated_at DESC LIMIT 1`).Scan(
		&installation.GuildID, &installation.OwnerUserID, &installation.ViewerRoleID,
		&installation.NoticeChannelID, &installation.GuideChannelID, &installation.GuideImageMessageID, &installation.GuideMessageID, &installation.CategoryID,
		&installation.ControlChannelID, &installation.ControlMessageID, &installation.StatusChannelID,
	)
	return installation, err
}

func (r *Repository) GetPollAlertState(ctx context.Context, provider domain.ProviderID, theaterID string) (PollAlertState, error) {
	var state PollAlertState
	var updatedAt string
	err := r.database.QueryRowContext(ctx, `
		SELECT provider, theater_id, failed, error_summary, control_delivered, status_delivered, updated_at
		FROM poll_alert_states WHERE provider = ? AND theater_id = ?`, provider, theaterID,
	).Scan(&state.Provider, &state.TheaterID, &state.Failed, &state.ErrorSummary, &state.ControlDelivered, &state.StatusDelivered, &updatedAt)
	if err != nil {
		return PollAlertState{}, err
	}
	state.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return state, err
}

func (r *Repository) SavePollAlertState(ctx context.Context, state PollAlertState) error {
	_, err := r.database.ExecContext(ctx, `
		INSERT INTO poll_alert_states(provider, theater_id, failed, error_summary, control_delivered, status_delivered, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, theater_id) DO UPDATE SET
		  failed=excluded.failed,
		  error_summary=excluded.error_summary,
		  control_delivered=excluded.control_delivered,
		  status_delivered=excluded.status_delivered,
		  updated_at=excluded.updated_at`,
		state.Provider, state.TheaterID, state.Failed, state.ErrorSummary,
		state.ControlDelivered, state.StatusDelivered, formatTime(r.now()))
	return err
}

func (r *Repository) SaveTargetState(ctx context.Context, state TargetState) error {
	_, err := r.database.ExecContext(ctx, `
		INSERT INTO target_states(target_id, channel_id, enabled, updated_at, generation)
		VALUES(?, ?, ?, ?, 1)
		ON CONFLICT(target_id) DO UPDATE SET
		  channel_id=excluded.channel_id,
		  enabled=excluded.enabled,
		  updated_at=excluded.updated_at,
		  generation=target_states.generation + 1`,
		state.TargetID, state.ChannelID, state.Enabled, formatTime(r.now()),
	)
	return err
}

func (r *Repository) ListTargetStates(ctx context.Context) ([]TargetState, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT target_id, channel_id, enabled, updated_at, generation
		FROM target_states ORDER BY target_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []TargetState
	for rows.Next() {
		var state TargetState
		var updatedAt string
		if err := rows.Scan(&state.TargetID, &state.ChannelID, &state.Enabled, &updatedAt, &state.Generation); err != nil {
			return nil, err
		}
		state.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *Repository) ReplaceBaseline(ctx context.Context, targetID string, showtimeIDs []string) error {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM channel_deliveries WHERE target_id = ? AND status = ?`, targetID, DeliveryPending); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM target_baselines WHERE target_id = ?`, targetID); err != nil {
		return err
	}
	for _, showtimeID := range showtimeIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO target_baselines(target_id, showtime_id, first_seen_at)
			VALUES(?, ?, ?)`, targetID, showtimeID, formatTime(r.now())); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (r *Repository) RecordTargetSnapshot(ctx context.Context, targetID string, showtimes []domain.Showtime) error {
	return r.recordTargetSnapshot(ctx, TargetState{TargetID: targetID}, showtimes, false)
}

func (r *Repository) RecordTargetSnapshotForState(ctx context.Context, state TargetState, showtimes []domain.Showtime) error {
	return r.recordTargetSnapshot(ctx, state, showtimes, true)
}

func (r *Repository) recordTargetSnapshot(ctx context.Context, state TargetState, showtimes []domain.Showtime, guarded bool) error {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if guarded {
		var current bool
		if err := transaction.QueryRowContext(ctx, `
			SELECT EXISTS(
			  SELECT 1 FROM target_states
			  WHERE target_id = ? AND enabled = 1 AND generation = ?
			)`, state.TargetID, state.Generation).Scan(&current); err != nil {
			return err
		}
		if !current {
			return nil
		}
	}
	targetID := state.TargetID
	now := formatTime(r.now())
	for _, showtime := range showtimes {
		var exists bool
		if err := transaction.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM target_baselines WHERE target_id = ? AND showtime_id = ?)`,
			targetID, showtime.ExternalID,
		).Scan(&exists); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO target_showtimes(
			  target_id, showtime_id, provider, theater_id, theater_name,
			  movie_id, movie_name, movie_english_name, play_date, starts_at, ends_at,
			  auditorium, format, rating, remaining_seats, total_seats,
			  seat_count_known, poster_url, first_seen_at, last_seen_at
			) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(target_id, showtime_id) DO UPDATE SET
			  play_date=excluded.play_date,
			  starts_at=excluded.starts_at,
			  ends_at=excluded.ends_at,
			  remaining_seats=excluded.remaining_seats,
			  total_seats=excluded.total_seats,
			  seat_count_known=excluded.seat_count_known,
			  last_seen_at=excluded.last_seen_at`,
			targetID, showtime.ExternalID, showtime.Provider, showtime.TheaterID, showtime.TheaterName,
			showtime.MovieID, showtime.MovieName, showtime.MovieEnglishName, showtime.PlayDate,
			showtime.StartsAt, showtime.EndsAt, showtime.Auditorium, showtime.Format,
			showtime.Rating, showtime.RemainingSeats, showtime.TotalSeats, showtime.SeatCountKnown,
			showtime.PosterURL, now, now,
		); err != nil {
			return err
		}
		if !exists {
			if _, err := transaction.ExecContext(ctx, `
				INSERT OR IGNORE INTO channel_deliveries(target_id, showtime_id, status)
				VALUES(?, ?, ?)`, targetID, showtime.ExternalID, DeliveryPending); err != nil {
				return err
			}
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT OR IGNORE INTO target_baselines(target_id, showtime_id, first_seen_at)
			VALUES(?, ?, ?)`, targetID, showtime.ExternalID, now); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (r *Repository) ListPendingChannelDeliveries(ctx context.Context) ([]PendingChannelDelivery, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT d.target_id, ts.channel_id, d.attempt_count,
		       st.provider, st.theater_id, st.theater_name, st.movie_id, st.movie_name,
		       st.movie_english_name, st.showtime_id, st.play_date, st.starts_at, st.ends_at,
		       st.auditorium, st.format, st.rating, st.remaining_seats, st.total_seats,
		       st.seat_count_known, st.poster_url
		FROM channel_deliveries d
		JOIN target_states ts ON ts.target_id = d.target_id
		JOIN target_showtimes st ON st.target_id = d.target_id AND st.showtime_id = d.showtime_id
		WHERE d.status = ? AND ts.enabled = 1
		ORDER BY d.target_id, st.play_date, st.starts_at, st.showtime_id`, DeliveryPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []PendingChannelDelivery
	for rows.Next() {
		var delivery PendingChannelDelivery
		delivery.Showtime.TargetID = ""
		if err := rows.Scan(
			&delivery.TargetID, &delivery.ChannelID, &delivery.AttemptCount,
			&delivery.Showtime.Provider, &delivery.Showtime.TheaterID, &delivery.Showtime.TheaterName,
			&delivery.Showtime.MovieID, &delivery.Showtime.MovieName, &delivery.Showtime.MovieEnglishName,
			&delivery.Showtime.ExternalID, &delivery.Showtime.PlayDate, &delivery.Showtime.StartsAt,
			&delivery.Showtime.EndsAt, &delivery.Showtime.Auditorium, &delivery.Showtime.Format,
			&delivery.Showtime.Rating, &delivery.Showtime.RemainingSeats, &delivery.Showtime.TotalSeats,
			&delivery.Showtime.SeatCountKnown, &delivery.Showtime.PosterURL,
		); err != nil {
			return nil, err
		}
		delivery.Showtime.TargetID = delivery.TargetID
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (r *Repository) MarkChannelSent(ctx context.Context, targetID string, showtimeIDs []string) error {
	return r.updateChannelDeliveries(ctx, targetID, showtimeIDs, "", false, true)
}

func (r *Repository) MarkChannelFailedAttempt(ctx context.Context, targetID string, showtimeIDs []string, message string, permanent bool) error {
	return r.updateChannelDeliveries(ctx, targetID, showtimeIDs, message, permanent, false)
}

func (r *Repository) updateChannelDeliveries(ctx context.Context, targetID string, showtimeIDs []string, message string, permanent, sent bool) error {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, showtimeID := range showtimeIDs {
		status := DeliveryPending
		if sent {
			status = DeliverySent
		} else if permanent {
			status = DeliveryFailed
		} else {
			var attempts int
			if err := transaction.QueryRowContext(ctx, `
				SELECT attempt_count FROM channel_deliveries
				WHERE target_id = ? AND showtime_id = ? AND status = ?`,
				targetID, showtimeID, DeliveryPending,
			).Scan(&attempts); err != nil {
				return err
			}
			if attempts+1 >= 3 {
				status = DeliveryFailed
			}
		}
		result, err := transaction.ExecContext(ctx, `
			UPDATE channel_deliveries
			SET status = ?, attempt_count = attempt_count + 1,
			    last_attempt_at = ?, last_error = ?
			WHERE target_id = ? AND showtime_id = ? AND status = ?`,
			status, formatTime(r.now()), nullableError(message), targetID, showtimeID, DeliveryPending,
		)
		if err != nil {
			return err
		}
		if err := requireChanged(result, "update channel delivery"); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (r *Repository) GetChannelDelivery(ctx context.Context, targetID, showtimeID string) (ChannelDelivery, error) {
	var delivery ChannelDelivery
	err := r.database.QueryRowContext(ctx, `
		SELECT target_id, showtime_id, status, attempt_count
		FROM channel_deliveries WHERE target_id = ? AND showtime_id = ?`, targetID, showtimeID,
	).Scan(&delivery.TargetID, &delivery.ShowtimeID, &delivery.Status, &delivery.AttemptCount)
	return delivery, err
}

func (r *Repository) DisableTarget(ctx context.Context, targetID string) error {
	result, err := r.database.ExecContext(ctx, `
		UPDATE target_states SET enabled = 0, updated_at = ? WHERE target_id = ?`, formatTime(r.now()), targetID)
	if err != nil {
		return err
	}
	return requireChanged(result, "disable target")
}

func (r *Repository) IsTargetEnabled(ctx context.Context, targetID string) (bool, error) {
	var enabled bool
	err := r.database.QueryRowContext(ctx, `SELECT enabled FROM target_states WHERE target_id = ?`, targetID).Scan(&enabled)
	return enabled, err
}

func nullableError(message string) any {
	if message == "" {
		return nil
	}
	return message
}

func (r *Repository) ListActiveProviderIDs(ctx context.Context) ([]domain.ProviderID, error) {
	states, err := r.ListTargetStates(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[domain.ProviderID]bool)
	for _, state := range states {
		if !state.Enabled {
			continue
		}
		target, ok := targets.Find(state.TargetID)
		if ok {
			seen[target.Provider] = true
		}
	}
	providers := make([]domain.ProviderID, 0, len(seen))
	for _, provider := range []domain.ProviderID{domain.ProviderCGV, domain.ProviderMegabox} {
		if seen[provider] {
			providers = append(providers, provider)
		}
	}
	return providers, nil
}

func (r *Repository) PingContext(ctx context.Context) error { return r.database.PingContext(ctx) }

func (r *Repository) LatestSuccessfulPoll(ctx context.Context, provider domain.ProviderID) (time.Time, error) {
	var value string
	err := r.database.QueryRowContext(ctx, `
		SELECT finished_at FROM poll_runs
		WHERE provider = ? AND succeeded = 1
		ORDER BY finished_at DESC LIMIT 1`, provider).Scan(&value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, value)
}

func (r *Repository) RecordPollRun(ctx context.Context, run PollRun) error {
	_, err := r.database.ExecContext(ctx, `
		INSERT INTO poll_runs(
			id, provider, theater_id, movie_id, started_at, finished_at,
			succeeded, showtime_count, error_summary, target_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID(), run.Group.Provider, run.Group.TheaterID, run.Group.MovieID,
		formatTime(run.StartedAt), formatTime(run.FinishedAt), run.Succeeded,
		run.ShowtimeCount, run.ErrorSummary, run.Group.TargetID,
	)
	return err
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return hex.EncodeToString(bytes)
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func requireChanged(result sql.Result, action string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%s: expected one row, changed %d", action, count)
	}
	return nil
}
