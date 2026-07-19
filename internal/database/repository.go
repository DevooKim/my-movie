package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"my-movie/internal/domain"
)

type SubscriptionStatus string

const (
	StatusInitializing SubscriptionStatus = "initializing"
	StatusActive       SubscriptionStatus = "active"
	StatusDisabled     SubscriptionStatus = "disabled"
)

type DeliveryStatus string

const (
	DeliveryBaseline DeliveryStatus = "baseline"
	DeliveryPending  DeliveryStatus = "pending"
	DeliverySent     DeliveryStatus = "sent"
	DeliveryFailed   DeliveryStatus = "failed"
)

type Subscription struct {
	ID            string
	DiscordUserID string
	Provider      domain.ProviderID
	Theater       domain.Theater
	Movie         domain.Movie
	Status        SubscriptionStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Delivery struct {
	SubscriptionID string
	ShowtimeKey    string
	Status         DeliveryStatus
	AttemptCount   int
}

type PendingDelivery struct {
	Subscription Subscription
	ShowtimeKey  string
	PlayDate     string
	StartsAt     string
	Auditorium   string
	AttemptCount int
}

type PollingGroup struct {
	Provider  domain.ProviderID
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

type CreateSubscriptionInput struct {
	DiscordUserID string
	Provider      domain.ProviderID
	Theater       domain.Theater
	Movie         domain.Movie
}

type Repository struct {
	database *sql.DB
	now      func() time.Time
}

func NewRepository(database *sql.DB, now func() time.Time) *Repository {
	return &Repository{database: database, now: now}
}

func (r *Repository) CreateInitializingSubscription(ctx context.Context, input CreateSubscriptionInput) (Subscription, error) {
	now := r.now().UTC()
	subscription := Subscription{
		ID: newID(), DiscordUserID: input.DiscordUserID, Provider: input.Provider,
		Theater: input.Theater, Movie: input.Movie, Status: StatusInitializing,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.database.ExecContext(ctx, `
		INSERT INTO subscriptions(
			id, discord_user_id, provider, theater_id, theater_name, theater_area_code,
			movie_id, movie_name, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		subscription.ID, subscription.DiscordUserID, subscription.Provider,
		subscription.Theater.ID, subscription.Theater.Name, subscription.Theater.AreaCode,
		subscription.Movie.ID, subscription.Movie.Name, subscription.Status,
		formatTime(now), formatTime(now),
	)
	if err != nil {
		return Subscription{}, err
	}
	return subscription, nil
}

func (r *Repository) ActivateSubscription(ctx context.Context, id string) error {
	result, err := r.database.ExecContext(ctx,
		"UPDATE subscriptions SET status = ?, updated_at = ? WHERE id = ? AND status = ?",
		StatusActive, formatTime(r.now()), id, StatusInitializing,
	)
	if err != nil {
		return err
	}
	return requireChanged(result, "activate subscription")
}

func (r *Repository) DeleteSubscription(ctx context.Context, id string) error {
	_, err := r.database.ExecContext(ctx, "DELETE FROM subscriptions WHERE id = ?", id)
	return err
}

func (r *Repository) DeleteAllSubscriptionsByUser(ctx context.Context, userID string) (int64, error) {
	result, err := r.database.ExecContext(ctx, "DELETE FROM subscriptions WHERE discord_user_id = ?", userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *Repository) ListSubscriptionsByUser(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := r.database.QueryContext(ctx, subscriptionSelect+" WHERE discord_user_id = ? ORDER BY created_at", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subscriptions []Subscription
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, rows.Err()
}

func (r *Repository) ListActivePollingGroups(ctx context.Context) ([]PollingGroup, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT DISTINCT provider, theater_id, movie_id
		FROM subscriptions WHERE status = ?
		ORDER BY provider, theater_id, movie_id`, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []PollingGroup
	for rows.Next() {
		var group PollingGroup
		if err := rows.Scan(&group.Provider, &group.TheaterID, &group.MovieID); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r *Repository) ListActiveProviderIDs(ctx context.Context) ([]domain.ProviderID, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT DISTINCT provider FROM subscriptions WHERE status = ? ORDER BY provider`, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var providers []domain.ProviderID
	for rows.Next() {
		var provider domain.ProviderID
		if err := rows.Scan(&provider); err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
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
			succeeded, showtime_count, error_summary
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID(), run.Group.Provider, run.Group.TheaterID, run.Group.MovieID,
		formatTime(run.StartedAt), formatTime(run.FinishedAt), run.Succeeded,
		run.ShowtimeCount, run.ErrorSummary,
	)
	return err
}

func (r *Repository) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	return scanSubscription(r.database.QueryRowContext(ctx, subscriptionSelect+" WHERE id = ?", id))
}

func (r *Repository) DisableSubscription(ctx context.Context, id string) error {
	result, err := r.database.ExecContext(ctx,
		"UPDATE subscriptions SET status = ?, updated_at = ? WHERE id = ?",
		StatusDisabled, formatTime(r.now()), id,
	)
	if err != nil {
		return err
	}
	return requireChanged(result, "disable subscription")
}

func (r *Repository) RecordScan(ctx context.Context, showtimes []domain.Showtime, baselineSubscriptionID string) error {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()

	for _, showtime := range showtimes {
		key := domain.ShowtimeKey(showtime)
		now := formatTime(r.now())
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO showtimes(key, provider, theater_id, movie_id, external_id, play_date, starts_at, auditorium, first_seen_at, last_seen_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
			key, showtime.Provider, showtime.TheaterID, showtime.MovieID, showtime.ExternalID,
			showtime.PlayDate, showtime.StartsAt, showtime.Auditorium, now, now,
		); err != nil {
			return err
		}

		subscriptions, err := matchingSubscriptions(ctx, transaction, showtime, baselineSubscriptionID)
		if err != nil {
			return err
		}
		for _, subscription := range subscriptions {
			status := DeliveryPending
			if subscription.id == baselineSubscriptionID {
				status = DeliveryBaseline
			}
			if _, err := transaction.ExecContext(ctx, `
				INSERT OR IGNORE INTO notification_deliveries(subscription_id, showtime_key, status)
				VALUES(?, ?, ?)`, subscription.id, key, status); err != nil {
				return err
			}
		}
	}
	return transaction.Commit()
}

func (r *Repository) MarkSent(ctx context.Context, subscriptionID string, showtimeKeys []string) error {
	for _, key := range showtimeKeys {
		if _, err := r.database.ExecContext(ctx, `
			UPDATE notification_deliveries
			SET status = ?, attempt_count = attempt_count + 1, last_attempt_at = ?, last_error = NULL
			WHERE subscription_id = ? AND showtime_key = ?`,
			DeliverySent, formatTime(r.now()), subscriptionID, key,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) MarkFailedAttempt(ctx context.Context, subscriptionID string, showtimeKeys []string, message string, permanent bool) error {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, key := range showtimeKeys {
		failed := permanent
		if !failed {
			var attempts int
			if err := transaction.QueryRowContext(ctx, `
				SELECT attempt_count FROM notification_deliveries
				WHERE subscription_id = ? AND showtime_key = ? AND status = ?`,
				subscriptionID, key, DeliveryPending,
			).Scan(&attempts); err != nil {
				return err
			}
			failed = attempts+1 >= 3
		}
		status := DeliveryPending
		if failed {
			status = DeliveryFailed
		}
		if _, err := transaction.ExecContext(ctx, `
			UPDATE notification_deliveries
			SET status = ?, attempt_count = attempt_count + 1, last_attempt_at = ?, last_error = ?
			WHERE subscription_id = ? AND showtime_key = ? AND status = ?`,
			status, formatTime(r.now()), message, subscriptionID, key, DeliveryPending,
		); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (r *Repository) ListPendingDeliveries(ctx context.Context) ([]PendingDelivery, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT s.id, s.discord_user_id, s.provider, s.theater_id, s.theater_name, s.theater_area_code,
		       s.movie_id, s.movie_name, s.status, s.created_at, s.updated_at,
		       d.showtime_key, st.play_date, st.starts_at, st.auditorium, d.attempt_count
		FROM notification_deliveries d
		JOIN subscriptions s ON s.id = d.subscription_id
		JOIN showtimes st ON st.key = d.showtime_key
		WHERE d.status = ? AND s.status = ?
		ORDER BY s.id, st.play_date, st.starts_at, d.showtime_key`, DeliveryPending, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []PendingDelivery
	for rows.Next() {
		var delivery PendingDelivery
		var createdAt, updatedAt string
		if err := rows.Scan(
			&delivery.Subscription.ID, &delivery.Subscription.DiscordUserID, &delivery.Subscription.Provider,
			&delivery.Subscription.Theater.ID, &delivery.Subscription.Theater.Name, &delivery.Subscription.Theater.AreaCode,
			&delivery.Subscription.Movie.ID, &delivery.Subscription.Movie.Name, &delivery.Subscription.Status,
			&createdAt, &updatedAt, &delivery.ShowtimeKey, &delivery.PlayDate, &delivery.StartsAt,
			&delivery.Auditorium, &delivery.AttemptCount,
		); err != nil {
			return nil, err
		}
		delivery.Subscription.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		delivery.Subscription.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (r *Repository) GetDelivery(ctx context.Context, subscriptionID, showtimeKey string) (Delivery, error) {
	var delivery Delivery
	err := r.database.QueryRowContext(ctx, `
		SELECT subscription_id, showtime_key, status, attempt_count
		FROM notification_deliveries WHERE subscription_id = ? AND showtime_key = ?`,
		subscriptionID, showtimeKey,
	).Scan(&delivery.SubscriptionID, &delivery.ShowtimeKey, &delivery.Status, &delivery.AttemptCount)
	return delivery, err
}

func (r *Repository) GetDeliveryStatus(ctx context.Context, subscriptionID, showtimeKey string) (DeliveryStatus, error) {
	var status DeliveryStatus
	err := r.database.QueryRowContext(ctx,
		"SELECT status FROM notification_deliveries WHERE subscription_id = ? AND showtime_key = ?",
		subscriptionID, showtimeKey,
	).Scan(&status)
	return status, err
}

type subscriptionMatch struct{ id string }

func matchingSubscriptions(ctx context.Context, transaction *sql.Tx, showtime domain.Showtime, baselineID string) ([]subscriptionMatch, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT id FROM subscriptions
		WHERE provider = ? AND theater_id = ? AND movie_id = ?
		AND (status = ? OR id = ?)`,
		showtime.Provider, showtime.TheaterID, showtime.MovieID, StatusActive, baselineID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var matches []subscriptionMatch
	for rows.Next() {
		var match subscriptionMatch
		if err := rows.Scan(&match.id); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

const subscriptionSelect = `
	SELECT id, discord_user_id, provider, theater_id, theater_name, theater_area_code,
	       movie_id, movie_name, status, created_at, updated_at
	FROM subscriptions`

type scanner interface{ Scan(...any) error }

func scanSubscription(row scanner) (Subscription, error) {
	var subscription Subscription
	var createdAt, updatedAt string
	err := row.Scan(
		&subscription.ID, &subscription.DiscordUserID, &subscription.Provider,
		&subscription.Theater.ID, &subscription.Theater.Name, &subscription.Theater.AreaCode,
		&subscription.Movie.ID, &subscription.Movie.Name, &subscription.Status, &createdAt, &updatedAt,
	)
	if err != nil {
		return Subscription{}, err
	}
	subscription.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Subscription{}, err
	}
	subscription.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return subscription, err
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
