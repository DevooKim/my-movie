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

func TestRunOncePollsEachGroupAndDeliversAfterScans(t *testing.T) {
	store := &fakeStore{groups: []database.PollingGroup{
		{Provider: domain.ProviderMegabox, TheaterID: "t1", MovieID: "m1"},
		{Provider: domain.ProviderCGV, TheaterID: "t2", MovieID: "m2"},
	}}
	megabox := &fakeProvider{id: domain.ProviderMegabox}
	cgv := &fakeProvider{id: domain.ProviderCGV, err: errors.New("upstream failed")}
	delivery := &fakeDelivery{}
	scheduler := New(store, delivery, map[domain.ProviderID]domain.TheaterProvider{
		domain.ProviderMegabox: megabox, domain.ProviderCGV: cgv,
	}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})

	if err := scheduler.RunOnce(context.Background()); err == nil {
		t.Fatal("expected aggregated provider error")
	}
	if megabox.calls != 1 || cgv.calls != 1 {
		t.Fatalf("calls megabox=%d cgv=%d", megabox.calls, cgv.calls)
	}
	if len(store.runs) != 2 || delivery.calls != 1 {
		t.Fatalf("runs=%d deliveries=%d", len(store.runs), delivery.calls)
	}
	if len(store.scans) != 1 {
		t.Fatalf("successful scans=%d", len(store.scans))
	}
}

func TestRunOnceLimitsEachProviderToTwoConcurrentRequests(t *testing.T) {
	store := &fakeStore{groups: []database.PollingGroup{
		{Provider: domain.ProviderMegabox, TheaterID: "t1", MovieID: "m1"},
		{Provider: domain.ProviderMegabox, TheaterID: "t2", MovieID: "m2"},
		{Provider: domain.ProviderMegabox, TheaterID: "t3", MovieID: "m3"},
	}}
	provider := &fakeProvider{id: domain.ProviderMegabox, delay: 10 * time.Millisecond}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]domain.TheaterProvider{
		domain.ProviderMegabox: provider,
	}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})

	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.maxConcurrent != 2 {
		t.Fatalf("max concurrency=%d", provider.maxConcurrent)
	}
}

func TestRunOnceRejectsOverlappingCycle(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	provider := &fakeProvider{id: domain.ProviderMegabox, started: started, release: release}
	store := &fakeStore{groups: []database.PollingGroup{{Provider: domain.ProviderMegabox, TheaterID: "t1", MovieID: "m1"}}}
	scheduler := New(store, &fakeDelivery{}, map[domain.ProviderID]domain.TheaterProvider{domain.ProviderMegabox: provider}, Options{Jitter: func() time.Duration { return 0 }, Sleep: noSleep, Now: time.Now})
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
	mu     sync.Mutex
	groups []database.PollingGroup
	runs   []database.PollRun
	scans  [][]domain.Showtime
}

func (s *fakeStore) ListActivePollingGroups(context.Context) ([]database.PollingGroup, error) {
	return s.groups, nil
}
func (s *fakeStore) RecordScan(_ context.Context, showtimes []domain.Showtime, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scans = append(s.scans, showtimes)
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
	id            domain.ProviderID
	err           error
	delay         time.Duration
	started       chan struct{}
	release       chan struct{}
	mu            sync.Mutex
	calls         int
	concurrent    int
	maxConcurrent int
}

func (p *fakeProvider) ID() domain.ProviderID                                        { return p.id }
func (p *fakeProvider) SearchMovies(context.Context, string) ([]domain.Movie, error) { return nil, nil }
func (p *fakeProvider) SearchTheaters(context.Context, string) ([]domain.Theater, error) {
	return nil, nil
}
func (p *fakeProvider) FetchShowtimes(_ context.Context, theaterID, movieID string) ([]domain.Showtime, error) {
	p.mu.Lock()
	p.calls++
	p.concurrent++
	if p.concurrent > p.maxConcurrent {
		p.maxConcurrent = p.concurrent
	}
	p.mu.Unlock()
	if p.started != nil {
		close(p.started)
		<-p.release
	}
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	p.mu.Lock()
	p.concurrent--
	p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	return []domain.Showtime{{Provider: p.id, TheaterID: theaterID, MovieID: movieID, ExternalID: theaterID + movieID}}, nil
}
func (p *fakeProvider) BuildBookingLinks(string, string) domain.BookingLinks {
	return domain.BookingLinks{}
}
