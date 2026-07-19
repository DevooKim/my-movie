package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

var ErrCycleRunning = errors.New("poll cycle is already running")

type Store interface {
	ListTargetStates(context.Context) ([]database.TargetState, error)
	RecordTargetSnapshotForState(context.Context, database.TargetState, []domain.Showtime) error
	RecordPollRun(context.Context, database.PollRun) error
}

type BranchProvider interface {
	ID() domain.ProviderID
	FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error)
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
	providers map[domain.ProviderID]BranchProvider
	interval  time.Duration
	jitter    func() time.Duration
	sleep     func(context.Context, time.Duration) error
	now       func() time.Time
	running   atomic.Bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func New(store Store, delivery DeliveryService, providers map[domain.ProviderID]BranchProvider, options Options) *Scheduler {
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

type branchGroup struct {
	branch  domain.Branch
	targets []enabledTarget
}

type enabledTarget struct {
	target domain.AlertTarget
	state  database.TargetState
}

func (s *Scheduler) RunOnce(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrCycleRunning
	}
	defer s.running.Store(false)
	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return err
	}
	groups, err := enabledBranchGroups(states)
	if err != nil {
		return err
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
			if err := s.pollBranch(ctx, group); err != nil {
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

func enabledBranchGroups(states []database.TargetState) ([]branchGroup, error) {
	byKey := map[string]*branchGroup{}
	for _, state := range states {
		if !state.Enabled {
			continue
		}
		target, ok := targets.Find(state.TargetID)
		if !ok {
			return nil, fmt.Errorf("target %q is unavailable", state.TargetID)
		}
		key := string(target.Provider) + ":" + target.Theater.ID
		group, ok := byKey[key]
		if !ok {
			group = &branchGroup{branch: domain.Branch{
				Provider: target.Provider, TheaterID: target.Theater.ID,
				TheaterName: target.Theater.Name, AreaCode: target.Theater.AreaCode,
			}}
			byKey[key] = group
		}
		group.targets = append(group.targets, enabledTarget{target: target, state: state})
	}
	groups := make([]branchGroup, 0, len(byKey))
	for _, group := range byKey {
		groups = append(groups, *group)
	}
	return groups, nil
}

func (s *Scheduler) pollBranch(ctx context.Context, group branchGroup) error {
	startedAt := s.now()
	if err := s.sleep(ctx, s.jitter()); err != nil {
		return err
	}
	provider, ok := s.providers[group.branch.Provider]
	if !ok {
		err := fmt.Errorf("provider %q is unavailable", group.branch.Provider)
		return errors.Join(err, s.recordRun(ctx, group.branch, startedAt, nil, err))
	}
	showtimes, fetchErr := provider.FetchBranchSnapshot(ctx, group.branch)
	resultErr := fetchErr
	if fetchErr == nil {
		for _, enabled := range group.targets {
			filtered := filterTarget(showtimes, enabled.target.ID)
			if err := s.store.RecordTargetSnapshotForState(ctx, enabled.state, filtered); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}
	recordErr := s.recordRun(ctx, group.branch, startedAt, showtimes, resultErr)
	return errors.Join(resultErr, recordErr)
}

func filterTarget(showtimes []domain.Showtime, targetID string) []domain.Showtime {
	filtered := make([]domain.Showtime, 0)
	for _, showtime := range showtimes {
		if showtime.TargetID == targetID {
			filtered = append(filtered, showtime)
		}
	}
	return filtered
}

func (s *Scheduler) recordRun(ctx context.Context, branch domain.Branch, startedAt time.Time, showtimes []domain.Showtime, runErr error) error {
	errorSummary := ""
	if runErr != nil {
		errorSummary = runErr.Error()
	}
	return s.store.RecordPollRun(ctx, database.PollRun{
		Group:     database.PollingGroup{Provider: branch.Provider, TheaterID: branch.TheaterID},
		StartedAt: startedAt, FinishedAt: s.now(), Succeeded: runErr == nil,
		ShowtimeCount: len(showtimes), ErrorSummary: errorSummary,
	})
}

func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("poll cycle failed", "error", err)
		}
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("poll cycle failed", "error", err)
				}
			}
		}
	}()
}

func (s *Scheduler) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.done == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
