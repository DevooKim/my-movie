package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"my-movie/internal/domain"

	_ "modernc.org/sqlite"
)

var fixedNow = time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

func newTestRepository(t *testing.T) (*Repository, func()) {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	return NewRepository(database, func() time.Time { return fixedNow }), func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}
}

func subscriptionInput(userID string) CreateSubscriptionInput {
	return CreateSubscriptionInput{
		DiscordUserID:  userID,
		Provider:       domain.ProviderMegabox,
		TargetID:       "megabox-coex-dolby",
		AuditoriumName: "Dolby Cinema",
		Theater:        domain.Theater{ID: "1372", Name: "강남", AreaCode: "10"},
		Movie:          domain.Movie{ID: "m1", Name: "영화"},
	}
}

func sampleShowtime(externalID string) domain.Showtime {
	return domain.Showtime{
		Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", TheaterID: "1372", MovieID: "m1",
		ExternalID: externalID, PlayDate: "2026-07-19", StartsAt: "14:00", Auditorium: "6관",
	}
}

func TestCreateInitializingSubscriptionRejectsDuplicate(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()

	if _, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("u1")); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("u1")); err == nil {
		t.Fatal("expected duplicate subscription error")
	}
}

func TestCreateInitializingSubscriptionAllowsSameMovieAcrossTargets(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()

	for _, targetID := range []string{"cgv-yongsan-imax", "cgv-yongsan-4dx", "cgv-yongsan-screenx"} {
		input := CreateSubscriptionInput{
			DiscordUserID: "u1", Provider: domain.ProviderCGV,
			TargetID: targetID, AuditoriumName: targetID,
			Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"},
			Movie:   domain.Movie{ID: "30001297", Name: "호프"},
		}
		if _, err := repository.CreateInitializingSubscription(ctx, input); err != nil {
			t.Fatalf("create %s: %v", targetID, err)
		}
	}

	duplicate := CreateSubscriptionInput{
		DiscordUserID: "u1", Provider: domain.ProviderCGV,
		TargetID: "cgv-yongsan-imax", AuditoriumName: "IMAX",
		Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"},
		Movie:   domain.Movie{ID: "30001297", Name: "호프"},
	}
	if _, err := repository.CreateInitializingSubscription(ctx, duplicate); err == nil {
		t.Fatal("expected duplicate target subscription error")
	}
}

func TestRecordScanKeepsBaselinePerSubscription(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()

	oldSubscription, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("old-user"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ActivateSubscription(ctx, oldSubscription.ID); err != nil {
		t.Fatal(err)
	}
	newSubscription, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("new-user"))
	if err != nil {
		t.Fatal(err)
	}

	if err := repository.RecordScan(ctx, []domain.Showtime{sampleShowtime("s1")}, newSubscription.ID); err != nil {
		t.Fatal(err)
	}
	assertDeliveryStatus(t, repository, oldSubscription.ID, "megabox:s1", DeliveryPending)
	assertDeliveryStatus(t, repository, newSubscription.ID, "megabox:s1", DeliveryBaseline)
}

func TestRecordScanDoesNotOverwriteSentDelivery(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()

	subscription, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("u1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ActivateSubscription(ctx, subscription.ID); err != nil {
		t.Fatal(err)
	}
	showtime := sampleShowtime("s1")
	if err := repository.RecordScan(ctx, []domain.Showtime{showtime}, ""); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkSent(ctx, subscription.ID, []string{"megabox:s1"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordScan(ctx, []domain.Showtime{showtime}, ""); err != nil {
		t.Fatal(err)
	}
	assertDeliveryStatus(t, repository, subscription.ID, "megabox:s1", DeliverySent)
}

func TestMarkSentRollsBackWholeGroupWhenAnyKeyIsMissing(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	subscription, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("u1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ActivateSubscription(ctx, subscription.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordScan(ctx, []domain.Showtime{sampleShowtime("s1")}, ""); err != nil {
		t.Fatal(err)
	}

	if err := repository.MarkSent(ctx, subscription.ID, []string{"megabox:s1", "megabox:missing"}); err == nil {
		t.Fatal("expected grouped update error")
	}
	assertDeliveryStatus(t, repository, subscription.ID, "megabox:s1", DeliveryPending)
}

func TestOpenPersistsSubscriptionAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.sqlite")
	ctx := context.Background()
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(database, func() time.Time { return fixedNow })
	if _, err := repository.CreateInitializingSubscription(ctx, subscriptionInput("u1")); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository = NewRepository(database, func() time.Time { return fixedNow })
	subscriptions, err := repository.ListSubscriptionsByUser(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != 1 || subscriptions[0].Movie.ID != "m1" {
		t.Fatalf("subscriptions=%+v", subscriptions)
	}
}

func TestMigrationDisablesLegacySubscriptionAndClearsDeliveries(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initial, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations VALUES(1, datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions VALUES('s1','u1','megabox','1351','코엑스','10','m1','영화','active',datetime('now'),datetime('now'));
		INSERT INTO showtimes VALUES('megabox:x','megabox','1351','m1','x','2026-07-19','10:00','1관',datetime('now'),datetime('now'));
		INSERT INTO notification_deliveries VALUES('s1','megabox:x','sent',1,datetime('now'),NULL);`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	var status, targetID string
	if err := db.QueryRowContext(ctx, `SELECT status, target_id FROM subscriptions WHERE id='s1'`).Scan(&status, &targetID); err != nil {
		t.Fatal(err)
	}
	if status != string(StatusDisabled) || targetID != "" {
		t.Fatalf("status=%q targetID=%q", status, targetID)
	}
	var deliveries int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("deliveries=%d", deliveries)
	}
}

func TestListActivePollingGroupsDeduplicatesMatchingSubscriptions(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	for _, userID := range []string{"u1", "u2"} {
		subscription, err := repository.CreateInitializingSubscription(ctx, subscriptionInput(userID))
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.ActivateSubscription(ctx, subscription.ID); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := repository.ListActivePollingGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0] != (PollingGroup{Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", MovieID: "m1"}) {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestRecordPollRunDrivesLatestProviderSuccess(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	finishedAt := fixedNow.Add(2 * time.Minute)
	err := repository.RecordPollRun(ctx, PollRun{
		Group:     PollingGroup{Provider: domain.ProviderMegabox, TargetID: "megabox-coex-dolby", MovieID: "m1"},
		StartedAt: fixedNow, FinishedAt: finishedAt, Succeeded: true, ShowtimeCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	latest, err := repository.LatestSuccessfulPoll(ctx, domain.ProviderMegabox)
	if err != nil {
		t.Fatal(err)
	}
	if !latest.Equal(finishedAt) {
		t.Fatalf("latest=%s want=%s", latest, finishedAt)
	}
}

func assertDeliveryStatus(t *testing.T, repository *Repository, subscriptionID, showtimeKey string, want DeliveryStatus) {
	t.Helper()
	got, err := repository.GetDeliveryStatus(context.Background(), subscriptionID, showtimeKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("status=%q want=%q", got, want)
	}
}
