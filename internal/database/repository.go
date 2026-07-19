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
