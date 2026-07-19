package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

var ErrCycleRunning = errors.New("poll cycle is already running")

type Store interface {
	ListActivePollingGroups(context.Context) ([]database.PollingGroup, error)
	RecordScan(context.Context, []domain.Showtime, string) error
	RecordPollRun(context.Context, database.PollRun) error
}

type DeliveryService interface{ DeliverPending(context.Context) error }

type Options struct {
	Interval time.Duration
	Jitter   func() time.Duration
	Sleep    func(context.Context, time.Duration) error
	Now      func() time.Time
}

type Scheduler struct {
	store     Store
	delivery  DeliveryService
	providers map[domain.ProviderID]domain.TheaterProvider
	interval  time.Duration
	jitter    func() time.Duration
	sleep     func(context.Context, time.Duration) error
	now       func() time.Time
	running   atomic.Bool
	cancel    context.CancelFunc
	wait      sync.WaitGroup
}

func New(store Store, delivery DeliveryService, providers map[domain.ProviderID]domain.TheaterProvider, options Options) *Scheduler {
	interval := options.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	jitter := options.Jitter
	if jitter == nil {
		jitter = func() time.Duration { return JitterFrom(rand.Float64) }
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{store: store, delivery: delivery, providers: providers, interval: interval, jitter: jitter, sleep: sleep, now: now}
}

func (s *Scheduler) RunOnce(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrCycleRunning
	}
	defer s.running.Store(false)
	groups, err := s.store.ListActivePollingGroups(ctx)
	if err != nil {
		return err
	}
	semaphores := make(map[domain.ProviderID]chan struct{})
	for id := range s.providers {
		semaphores[id] = make(chan struct{}, 2)
	}
	var (
		waitGroup sync.WaitGroup
		mu        sync.Mutex
		cycleErrs []error
	)
	for _, group := range groups {
		group := group
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := s.pollGroup(ctx, group, semaphores[group.Provider]); err != nil {
				mu.Lock()
				cycleErrs = append(cycleErrs, err)
				mu.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	if err := s.delivery.DeliverPending(ctx); err != nil {
		cycleErrs = append(cycleErrs, err)
	}
	return errors.Join(cycleErrs...)
}

func (s *Scheduler) pollGroup(ctx context.Context, group database.PollingGroup, semaphore chan struct{}) error {
	startedAt := s.now()
	if err := s.sleep(ctx, s.jitter()); err != nil {
		return err
	}
	provider, ok := s.providers[group.Provider]
	if !ok {
		err := fmt.Errorf("provider %q is unavailable", group.Provider)
		return errors.Join(err, s.recordRun(ctx, group, startedAt, nil, err))
	}
	select {
	case semaphore <- struct{}{}:
		defer func() { <-semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	showtimes, fetchErr := provider.FetchShowtimes(ctx, group.TheaterID, group.MovieID)
	resultErr := fetchErr
	if fetchErr == nil {
		resultErr = s.store.RecordScan(ctx, showtimes, "")
	}
	recordErr := s.recordRun(ctx, group, startedAt, showtimes, resultErr)
	return errors.Join(resultErr, recordErr)
}

func (s *Scheduler) recordRun(ctx context.Context, group database.PollingGroup, startedAt time.Time, showtimes []domain.Showtime, runErr error) error {
	errorSummary := ""
	if runErr != nil {
		errorSummary = runErr.Error()
	}
	return s.store.RecordPollRun(ctx, database.PollRun{
		Group: group, StartedAt: startedAt, FinishedAt: s.now(), Succeeded: runErr == nil,
		ShowtimeCount: len(showtimes), ErrorSummary: errorSummary,
	})
}

func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		_ = s.RunOnce(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.RunOnce(ctx)
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wait.Wait()
}

func JitterFrom(random func() float64) time.Duration {
	value := random()
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return time.Duration(value * float64(10*time.Second))
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
