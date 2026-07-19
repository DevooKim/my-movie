package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

func TestRunOnceFetchesCGVBranchOnceAndSplitsEnabledTargets(t *testing.T) {
	store := &fakeStore{states: []database.TargetState{
		{TargetID: "cgv-yongsan-imax", Enabled: true},
		{TargetID: "cgv-yongsan-4dx", Enabled: true},
		{TargetID: "cgv-yongsan-screenx", Enabled: true},
	}}
	provider := &fakeProvider{id: domain.ProviderCGV, showtimes: []domain.Showtime{
		{TargetID: "cgv-yongsan-imax", ExternalID: "imax"},
		{TargetID: "cgv-yongsan-4dx", ExternalID: "4dx"},
	}}
	delivery := &fakeDelivery{}
	scheduler := New(store, delivery, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
	if len(store.snapshots) != 3 || len(store.snapshots["cgv-yongsan-imax"]) != 1 || len(store.snapshots["cgv-yongsan-4dx"]) != 1 || len(store.snapshots["cgv-yongsan-screenx"]) != 0 {
		t.Fatalf("snapshots=%+v", store.snapshots)
	}
	if delivery.calls != 1 || len(store.runs) != 1 || store.runs[0].Group.TheaterID != "0013" {
		t.Fatalf("delivery=%d runs=%+v", delivery.calls, store.runs)
	}
}

func TestRunOnceFetchesEachEnabledMegaboxBranch(t *testing.T) {
	store := &fakeStore{states: []database.TargetState{
		{TargetID: "megabox-coex-dolby", Enabled: true},
		{TargetID: "megabox-namhyeona-dolby", Enabled: true},
	}}
	provider := &fakeProvider{id: domain.ProviderMegabox}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{domain.ProviderMegabox: provider}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls=%d", provider.calls)
	}
}

func TestFailedBranchDoesNotRecordTargetSnapshots(t *testing.T) {
	store := &fakeStore{states: []database.TargetState{{TargetID: "cgv-yongsan-imax", Enabled: true}}}
	provider := &fakeProvider{id: domain.ProviderCGV, err: errors.New("upstream failed")}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})

	if err := scheduler.RunOnce(context.Background()); err == nil {
		t.Fatal("expected provider error")
	}
	if len(store.snapshots) != 0 || len(store.runs) != 1 || store.runs[0].Succeeded {
		t.Fatalf("snapshots=%v runs=%+v", store.snapshots, store.runs)
	}
}

func TestRunOnceRejectsOverlappingCycle(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	provider := &fakeProvider{id: domain.ProviderMegabox, started: started, release: release}
	store := &fakeStore{states: []database.TargetState{{TargetID: "megabox-coex-dolby", Enabled: true}}}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{domain.ProviderMegabox: provider}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})
	done := make(chan error, 1)
	go func() { done <- scheduler.RunOnce(context.Background()) }()
	<-started
	if err := scheduler.RunOnce(context.Background()); !errors.Is(err, ErrCycleRunning) {
		t.Fatalf("error=%v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestJitterStaysWithinTenSeconds(t *testing.T) {
	if got := JitterFrom(func() float64 { return 0 }); got != 0 {
		t.Fatalf("minimum jitter=%s", got)
	}
	if got := JitterFrom(func() float64 { return 1 }); got != 10*time.Second {
		t.Fatalf("maximum jitter=%s", got)
	}
}

func noSleep(context.Context, time.Duration) error { return nil }

type fakeStore struct {
	mu        sync.Mutex
	states    []database.TargetState
	runs      []database.PollRun
	snapshots map[string][]domain.Showtime
}

func (s *fakeStore) ListTargetStates(context.Context) ([]database.TargetState, error) {
	return s.states, nil
}
func (s *fakeStore) RecordTargetSnapshotForState(_ context.Context, state database.TargetState, showtimes []domain.Showtime) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshots == nil {
		s.snapshots = map[string][]domain.Showtime{}
	}
	s.snapshots[state.TargetID] = append([]domain.Showtime(nil), showtimes...)
	return nil
}
func (s *fakeStore) RecordPollRun(_ context.Context, run database.PollRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, run)
	return nil
}

type fakeDelivery struct{ calls int }

func (d *fakeDelivery) DeliverPending(context.Context) error { d.calls++; return nil }

type fakeProvider struct {
	id        domain.ProviderID
	showtimes []domain.Showtime
	err       error
	started   chan struct{}
	release   chan struct{}
	mu        sync.Mutex
	calls     int
}

func (p *fakeProvider) ID() domain.ProviderID { return p.id }
func (p *fakeProvider) FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.started != nil {
		close(p.started)
		<-p.release
	}
	return p.showtimes, p.err
}
