package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"my-movie/internal/domain"
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
		DiscordUserID: userID,
		Provider:      domain.ProviderMegabox,
		Theater:       domain.Theater{ID: "1372", Name: "강남", AreaCode: "10"},
		Movie:         domain.Movie{ID: "m1", Name: "영화"},
	}
}

func sampleShowtime(externalID string) domain.Showtime {
	return domain.Showtime{
		Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1",
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
	if len(groups) != 1 || groups[0] != (PollingGroup{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1"}) {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestRecordPollRunDrivesLatestProviderSuccess(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	finishedAt := fixedNow.Add(2 * time.Minute)
	err := repository.RecordPollRun(ctx, PollRun{
		Group:     PollingGroup{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1"},
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
