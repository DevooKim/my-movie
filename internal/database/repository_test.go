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
	db, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	return NewRepository(db, func() time.Time { return fixedNow }), func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}
}

func TestInstallationAndTargetStatePersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel-targets.sqlite")
	ctx := context.Background()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db, func() time.Time { return fixedNow })
	installation := Installation{
		GuildID: "g1", OwnerUserID: "u1", CategoryID: "cat",
		ControlChannelID: "control", ControlMessageID: "message",
	}
	if err := repository.SaveInstallation(ctx, installation); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveTargetState(ctx, TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository = NewRepository(db, func() time.Time { return fixedNow })
	gotInstallation, err := repository.GetInstallation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotInstallation != installation {
		t.Fatalf("installation=%+v want=%+v", gotInstallation, installation)
	}
	states, err := repository.ListTargetStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].TargetID != "cgv-yongsan-imax" || !states[0].Enabled {
		t.Fatalf("states=%+v", states)
	}
}

func TestReplaceBaselineAndRecordSnapshotQueueOnlyNewShowtimes(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	state := TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}
	if err := repository.SaveTargetState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceBaseline(ctx, state.TargetID, []string{"old", "gone"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceBaseline(ctx, state.TargetID, []string{"old"}); err != nil {
		t.Fatal(err)
	}
	showtimes := []domain.Showtime{
		{Provider: domain.ProviderCGV, TargetID: state.TargetID, TheaterID: "0013", TheaterName: "용산아이파크몰", MovieID: "m1", MovieName: "호프", ExternalID: "old", PlayDate: "2026-07-19", StartsAt: "19:10", EndsAt: "21:56", Auditorium: "IMAX관", Format: "IMAX", RemainingSeats: 57, TotalSeats: 144, SeatCountKnown: true},
		{Provider: domain.ProviderCGV, TargetID: state.TargetID, TheaterID: "0013", TheaterName: "용산아이파크몰", MovieID: "m1", MovieName: "호프", ExternalID: "new", PlayDate: "2026-07-19", StartsAt: "22:00", EndsAt: "00:46", Auditorium: "IMAX관", Format: "IMAX"},
	}
	if err := repository.RecordTargetSnapshot(ctx, state.TargetID, showtimes); err != nil {
		t.Fatal(err)
	}
	pending, err := repository.ListPendingChannelDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Showtime.ExternalID != "new" || pending[0].ChannelID != "imax" || pending[0].Showtime.MovieName != "호프" {
		t.Fatalf("pending=%+v", pending)
	}
}

func TestRecordSnapshotRefreshesPresentationMetadataForPendingDelivery(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	targetID := "cgv-yongsan-imax"
	if err := repository.SaveTargetState(ctx, TargetState{TargetID: targetID, ChannelID: "imax", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	showtime := domain.Showtime{
		Provider: domain.ProviderCGV, TargetID: targetID, TheaterID: "0013", TheaterName: "용산아이파크몰",
		MovieID: "m1", MovieName: "오디세이", ExternalID: "same-session", PlayDate: "2026-08-10",
		StartsAt: "00:00", EndsAt: "02:30", Auditorium: "IMAX관", Format: "IMAX",
	}
	if err := repository.RecordTargetSnapshot(ctx, targetID, []domain.Showtime{showtime}); err != nil {
		t.Fatal(err)
	}
	showtime.PlayDate = "2026-08-09"
	showtime.StartsAt = "24:00"
	showtime.EndsAt = "26:30"
	if err := repository.RecordTargetSnapshot(ctx, targetID, []domain.Showtime{showtime}); err != nil {
		t.Fatal(err)
	}

	pending, err := repository.ListPendingChannelDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Showtime.PlayDate != "2026-08-09" || pending[0].Showtime.StartsAt != "24:00" || pending[0].Showtime.EndsAt != "26:30" {
		t.Fatalf("pending=%+v", pending)
	}
}

func TestReplaceBaselineDiscardsPendingAlertsFromBeforeReenable(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	state := TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}
	if err := repository.SaveTargetState(ctx, state); err != nil {
		t.Fatal(err)
	}
	showtime := domain.Showtime{
		Provider: domain.ProviderCGV, TargetID: state.TargetID, TheaterID: "0013",
		MovieID: "m1", MovieName: "호프", ExternalID: "opened-before-off",
		PlayDate: "2026-07-19", StartsAt: "19:10",
	}
	if err := repository.RecordTargetSnapshot(ctx, state.TargetID, []domain.Showtime{showtime}); err != nil {
		t.Fatal(err)
	}
	if err := repository.DisableTarget(ctx, state.TargetID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceBaseline(ctx, state.TargetID, []string{showtime.ExternalID}); err != nil {
		t.Fatal(err)
	}
	state.Enabled = true
	if err := repository.SaveTargetState(ctx, state); err != nil {
		t.Fatal(err)
	}
	pending, err := repository.ListPendingChannelDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("stale pending alerts=%+v", pending)
	}
}

func TestStalePollingGenerationCannotQueueAfterTargetWasToggled(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	state := TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}
	if err := repository.SaveTargetState(ctx, state); err != nil {
		t.Fatal(err)
	}
	states, err := repository.ListTargetStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stale := states[0]
	state.Enabled = false
	if err := repository.SaveTargetState(ctx, state); err != nil {
		t.Fatal(err)
	}
	state.Enabled = true
	if err := repository.SaveTargetState(ctx, state); err != nil {
		t.Fatal(err)
	}
	showtime := domain.Showtime{Provider: domain.ProviderCGV, TargetID: state.TargetID, MovieID: "m1", MovieName: "호프", ExternalID: "stale", PlayDate: "2026-07-19", StartsAt: "19:10"}
	if err := repository.RecordTargetSnapshotForState(ctx, stale, []domain.Showtime{showtime}); err != nil {
		t.Fatal(err)
	}
	pending, err := repository.ListPendingChannelDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("stale poll queued deliveries=%+v", pending)
	}
}

func TestEnabledTargetsDriveActiveProviders(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	for _, state := range []TargetState{
		{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true},
		{TargetID: "megabox-coex-dolby", ChannelID: "dolby", Enabled: false},
	} {
		if err := repository.SaveTargetState(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	providers, err := repository.ListActiveProviderIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0] != domain.ProviderCGV {
		t.Fatalf("providers=%v", providers)
	}
}

func TestRecordPollRunDrivesLatestProviderSuccess(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	finishedAt := fixedNow.Add(2 * time.Minute)
	err := repository.RecordPollRun(ctx, PollRun{
		Group:     PollingGroup{Provider: domain.ProviderMegabox, TheaterID: "1351"},
		StartedAt: fixedNow, FinishedAt: finishedAt, Succeeded: true, ShowtimeCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := repository.LatestSuccessfulPoll(ctx, domain.ProviderMegabox)
	if err != nil || !latest.Equal(finishedAt) {
		t.Fatalf("latest=%s err=%v", latest, err)
	}
}

func TestMigrationThreePreservesDisabledLegacyRows(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `
		INSERT INTO schema_migrations VALUES(1, datetime('now'));
		INSERT INTO subscriptions VALUES('s1','u1','megabox','1351','코엑스','10','m1','영화','active',datetime('now'),datetime('now'));`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE id='s1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "disabled" {
		t.Fatalf("legacy status=%q", status)
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 5 {
		t.Fatalf("version=%d err=%v", version, err)
	}
}
