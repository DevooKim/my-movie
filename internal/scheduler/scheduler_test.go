package scheduler

import (
	"context"
	"errors"
	"runtime"
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
	scheduler := New(store, delivery, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider}, Options{Sleep: noSleep, Now: time.Now})

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
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{domain.ProviderMegabox: provider}, Options{Sleep: noSleep, Now: time.Now})

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
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider}, Options{Sleep: noSleep, Now: time.Now})

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
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{domain.ProviderMegabox: provider}, Options{Sleep: noSleep, Now: time.Now})
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

func TestBurstOffsetsCoverBoundaryThroughThirtySeconds(t *testing.T) {
	want := []time.Duration{0}
	got := burstOffsets()
	if len(got) != len(want) {
		t.Fatalf("offsets=%v", got)
	}
	for index, offset := range want {
		if got[index] != offset {
			t.Fatalf("offset[%d]=%s want=%s", index, got[index], offset)
		}
	}
}

func TestBurstBoundaryUsesOnlyExactCurrentBoundary(t *testing.T) {
	boundary := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if got := burstBoundary(boundary, 30*time.Second); !got.Equal(boundary) {
		t.Fatalf("boundary=%s", got)
	}
	if got := burstBoundary(boundary.Add(time.Second), 30*time.Second); !got.Equal(boundary.Add(30 * time.Second)) {
		t.Fatalf("boundary=%s", got)
	}
}

func TestRunBurstPreparesCGVAndPollsOnceAtBoundary(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 19, 11, 59, 50, 0, time.UTC)}
	cgv := &fakePreparingProvider{id: domain.ProviderCGV}
	megabox := &fakeProvider{id: domain.ProviderMegabox}
	store := &fakeStore{states: []database.TargetState{
		{TargetID: "cgv-yongsan-imax", Enabled: true},
		{TargetID: "megabox-coex-dolby", Enabled: true},
	}}
	delivery := &fakeDelivery{}
	scheduler := New(store, delivery, map[domain.ProviderID]BranchProvider{
		domain.ProviderCGV:     cgv,
		domain.ProviderMegabox: megabox,
	}, Options{Sleep: clock.Sleep, Now: clock.Now})

	boundary := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if err := scheduler.runBurst(context.Background(), boundary); err != nil {
		t.Fatal(err)
	}
	if cgv.prepareCalls != 1 || cgv.poll.fetchCalls != 1 || cgv.poll.closeCalls != 1 {
		t.Fatalf("cgv prepare=%d fetch=%d close=%d", cgv.prepareCalls, cgv.poll.fetchCalls, cgv.poll.closeCalls)
	}
	if megabox.calls != 1 {
		t.Fatalf("megabox calls=%d", megabox.calls)
	}
	if delivery.calls != 2 {
		t.Fatalf("delivery calls=%d", delivery.calls)
	}
	wantSleeps := []time.Duration{10 * time.Second}
	if len(clock.sleeps) != len(wantSleeps) {
		t.Fatalf("sleeps=%v", clock.sleeps)
	}
	for index, want := range wantSleeps {
		if clock.sleeps[index] != want {
			t.Fatalf("sleep[%d]=%s want=%s", index, clock.sleeps[index], want)
		}
	}
}

func TestRunBurstDoesNotBlockMegaboxWhileCGVPrepares(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 19, 11, 59, 50, 0, time.UTC)}
	cgv := &blockingPreparingProvider{id: domain.ProviderCGV, started: make(chan struct{})}
	megabox := &fakeProvider{id: domain.ProviderMegabox}
	store := &fakeStore{states: []database.TargetState{
		{TargetID: "cgv-yongsan-imax", Enabled: true},
		{TargetID: "megabox-coex-dolby", Enabled: true},
	}}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{
		domain.ProviderCGV:     cgv,
		domain.ProviderMegabox: megabox,
	}, Options{Sleep: clock.Sleep, Now: clock.Now})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- scheduler.runBurst(ctx, time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	}()
	<-cgv.started

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("Megabox polling waited for CGV preparation")
	}
	if megabox.calls != 1 {
		t.Fatalf("megabox calls=%d", megabox.calls)
	}
	if cgv.normalFetchCalls != 0 {
		t.Fatalf("CGV fallback calls=%d", cgv.normalFetchCalls)
	}
}

func TestRunBurstDoesNotWaitForUnresponsivePreparationDuringCleanup(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 7, 19, 11, 59, 50, 0, time.UTC)}
	cgv := &unresponsivePreparingProvider{id: domain.ProviderCGV, started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	megabox := &fakeProvider{id: domain.ProviderMegabox}
	store := &fakeStore{states: []database.TargetState{
		{TargetID: "cgv-yongsan-imax", Enabled: true},
		{TargetID: "megabox-coex-dolby", Enabled: true},
	}}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]BranchProvider{
		domain.ProviderCGV:     cgv,
		domain.ProviderMegabox: megabox,
	}, Options{Sleep: clock.Sleep, Now: clock.Now})
	done := make(chan error, 1)
	go func() {
		done <- scheduler.runBurst(context.Background(), time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	}()
	<-cgv.started

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		close(cgv.release)
		<-done
		t.Fatal("burst cleanup waited for unresponsive preparation")
	}
	if err := scheduler.RunOnce(context.Background()); !errors.Is(err, ErrCycleRunning) {
		t.Fatalf("overlapping run error=%v", err)
	}
	close(cgv.release)
	<-cgv.finished
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

type fakeDelivery struct {
	mu    sync.Mutex
	calls int
}

func (d *fakeDelivery) DeliverPending(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return nil
}

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

type fakePreparedPoll struct {
	mu         sync.Mutex
	fetchCalls int
	closeCalls int
	showtimes  []domain.Showtime
}

func (p *fakePreparedPoll) Fetch(context.Context) ([]domain.Showtime, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fetchCalls++
	return p.showtimes, nil
}
func (p *fakePreparedPoll) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
	return nil
}

type fakePreparingProvider struct {
	id           domain.ProviderID
	prepareCalls int
	poll         fakePreparedPoll
}

func (p *fakePreparingProvider) ID() domain.ProviderID { return p.id }
func (p *fakePreparingProvider) FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error) {
	return p.poll.showtimes, nil
}
func (p *fakePreparingProvider) PrepareBranch(context.Context, domain.Branch) (domain.PreparedBranchPoll, error) {
	p.prepareCalls++
	return &p.poll, nil
}

type blockingPreparingProvider struct {
	id               domain.ProviderID
	started          chan struct{}
	normalFetchCalls int
}

func (p *blockingPreparingProvider) ID() domain.ProviderID { return p.id }
func (p *blockingPreparingProvider) FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error) {
	p.normalFetchCalls++
	return nil, nil
}
func (p *blockingPreparingProvider) PrepareBranch(ctx context.Context, _ domain.Branch) (domain.PreparedBranchPoll, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

type unresponsivePreparingProvider struct {
	id       domain.ProviderID
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (p *unresponsivePreparingProvider) ID() domain.ProviderID { return p.id }
func (p *unresponsivePreparingProvider) FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error) {
	return nil, nil
}
func (p *unresponsivePreparingProvider) PrepareBranch(context.Context, domain.Branch) (domain.PreparedBranchPoll, error) {
	close(p.started)
	<-p.release
	close(p.finished)
	return nil, errors.New("Lightpanda stopped responding")
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) Sleep(_ context.Context, duration time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, duration)
	c.now = c.now.Add(duration)
	c.mu.Unlock()
	runtime.Gosched()
	return nil
}
