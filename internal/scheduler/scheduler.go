package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"my-movie/internal/database"
	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

var ErrCycleRunning = errors.New("poll cycle is already running")

const (
	prewarmLead = 5 * time.Second
	burstStep   = 5 * time.Second
	burstWindow = 0
)

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
type PollReporter interface { Report(context.Context, database.PollingGroup, string, error) error }

type Options struct {
	Interval time.Duration
	Sleep    func(context.Context, time.Duration) error
	Now      func() time.Time
	Reporter PollReporter
}

type Scheduler struct {
	store     Store
	delivery  DeliveryService
	providers map[domain.ProviderID]BranchProvider
	interval  time.Duration
	sleep     func(context.Context, time.Duration) error
	now       func() time.Time
	reporter  PollReporter
	running   atomic.Bool
	cancel    context.CancelFunc
	done      chan struct{}
}

func New(store Store, delivery DeliveryService, providers map[domain.ProviderID]BranchProvider, options Options) *Scheduler {
	interval := options.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{store: store, delivery: delivery, providers: providers, interval: interval, sleep: sleep, now: now, reporter: options.Reporter}
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

type preparedGroup struct {
	group    branchGroup
	provider BranchProvider
	poll     domain.PreparedBranchPoll
	err      error
	ready    <-chan preparationResult
	done     <-chan struct{}
}

type preparationResult struct {
	poll domain.PreparedBranchPoll
	err  error
}

func (s *Scheduler) runBurst(ctx context.Context, boundary time.Time) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrCycleRunning
	}
	releaseRunning := true
	defer func() {
		if releaseRunning {
			s.running.Store(false)
		}
	}()

	states, err := s.store.ListTargetStates(ctx)
	if err != nil {
		return err
	}
	groups, err := enabledBranchGroups(states)
	if err != nil {
		return err
	}
	prepared, preparationErr := s.prepareGroups(ctx, groups)
	defer func() {
		closePreparedPolls(prepared)
	}()

	cycleErr := preparationErr
	for _, offset := range burstOffsets() {
		due := boundary.Add(offset)
		now := s.now()
		if due.Before(now) {
			continue
		}
		if wait := due.Sub(now); wait > 0 {
			if err := s.sleep(ctx, wait); err != nil {
				return errors.Join(cycleErr, err)
			}
		}
		cycleErr = errors.Join(cycleErr, s.runBurstAttempt(ctx, prepared))
	}
	return cycleErr
}

func closePreparedPolls(groups []preparedGroup) {
	for _, group := range groups {
		if group.poll != nil {
			_ = group.poll.Close()
		}
	}
}

func (s *Scheduler) prepareGroups(ctx context.Context, groups []branchGroup) ([]preparedGroup, error) {
	prepared := make([]preparedGroup, 0, len(groups))
	var preparationErr error
	for _, group := range groups {
		provider, ok := s.providers[group.branch.Provider]
		if !ok {
			err := fmt.Errorf("provider %q is unavailable", group.branch.Provider)
			prepared = append(prepared, preparedGroup{group: group, err: err})
			preparationErr = errors.Join(preparationErr, err, s.recordRun(ctx, group.branch, s.now(), nil, err))
			continue
		}
		item := preparedGroup{group: group, provider: provider}
		preparer, ok := provider.(domain.BranchPreparer)
		if ok {
			ready := make(chan preparationResult, 1)
			item.ready = ready
			go func(preparer domain.BranchPreparer, branch domain.Branch, ready chan<- preparationResult) {
				poll, err := preparer.PrepareBranch(ctx, branch)
				ready <- preparationResult{poll: poll, err: err}
			}(preparer, group.branch, ready)
		}
		prepared = append(prepared, item)
	}
	return prepared, preparationErr
}

func (s *Scheduler) runBurstAttempt(ctx context.Context, groups []preparedGroup) error {
	cycleErr := s.collectPreparedGroups(ctx, groups)
	completed := make(chan error, len(groups))
	count := 0
	for _, group := range groups {
		if group.err != nil || group.ready != nil {
			continue
		}
		group := group
		count++
		go func() { completed <- s.pollPreparedBranch(ctx, group) }()
	}
	for range count {
		cycleErr = errors.Join(cycleErr, <-completed)
		cycleErr = errors.Join(cycleErr, s.delivery.DeliverPending(ctx))
	}
	return cycleErr
}

func (s *Scheduler) collectPreparedGroups(ctx context.Context, groups []preparedGroup) error {
	var preparationErr error
	for index := range groups {
		if groups[index].ready == nil {
			continue
		}
		select {
		case result := <-groups[index].ready:
			groups[index].ready = nil
			groups[index].poll = result.poll
			groups[index].err = result.err
			if result.err != nil {
				preparationErr = errors.Join(preparationErr, result.err, s.recordRun(ctx, groups[index].group.branch, s.now(), nil, result.err))
				preparationErr = errors.Join(preparationErr, s.reportFetch(ctx, groups[index].group.branch, result.err))
			}
		default:
		}
	}
	return preparationErr
}

func (s *Scheduler) pollPreparedBranch(ctx context.Context, group preparedGroup) error {
	if group.poll != nil {
		return s.pollBranchWithFetch(ctx, group.group, group.poll.Fetch)
	}
	return s.pollBranchWithFetch(ctx, group.group, func(ctx context.Context) ([]domain.Showtime, error) {
		return group.provider.FetchBranchSnapshot(ctx, group.group.branch)
	})
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
	provider, ok := s.providers[group.branch.Provider]
	if !ok {
		err := fmt.Errorf("provider %q is unavailable", group.branch.Provider)
		return errors.Join(err, s.recordRun(ctx, group.branch, s.now(), nil, err))
	}
	return s.pollBranchWithFetch(ctx, group, func(ctx context.Context) ([]domain.Showtime, error) {
		return provider.FetchBranchSnapshot(ctx, group.branch)
	})
}

func (s *Scheduler) pollBranchWithFetch(ctx context.Context, group branchGroup, fetch func(context.Context) ([]domain.Showtime, error)) error {
	startedAt := s.now()
	showtimes, fetchErr := fetch(ctx)
	resultErr := fetchErr
	resultErr = errors.Join(resultErr, s.reportFetch(ctx, group.branch, fetchErr))
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

func (s *Scheduler) reportFetch(ctx context.Context, branch domain.Branch, fetchErr error) error {
	if s.reporter == nil {
		return nil
	}
	return s.reporter.Report(ctx, database.PollingGroup{Provider: branch.Provider, TheaterID: branch.TheaterID}, branch.TheaterName, fetchErr)
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
		boundary := burstBoundary(s.now(), s.interval)
		for {
			prepareAt := boundary.Add(-prewarmLead)
			if wait := prepareAt.Sub(s.now()); wait > 0 {
				if err := s.sleep(ctx, wait); err != nil {
					return
				}
			}
			if err := s.runBurst(ctx, boundary); err != nil {
				if errors.Is(err, ErrCycleRunning) {
					boundary = boundary.Add(s.interval)
					continue
				}
				if !errors.Is(err, context.Canceled) {
					slog.Error("poll burst failed", "error", err)
				}
			}
			if ctx.Err() != nil {
				return
			}
			boundary = burstBoundary(s.now().Add(time.Nanosecond), s.interval)
		}
	}()
}

func burstOffsets() []time.Duration {
	offsets := make([]time.Duration, 0, int(burstWindow/burstStep)+1)
	for offset := time.Duration(0); offset <= burstWindow; offset += burstStep {
		offsets = append(offsets, offset)
	}
	return offsets
}

func burstBoundary(now time.Time, interval time.Duration) time.Time {
	boundary := now.Truncate(interval)
	if now.Sub(boundary) <= burstWindow {
		return boundary
	}
	return boundary.Add(interval)
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
		for _, provider := range s.providers {
			if closer, ok := provider.(interface{ ClosePrepared() error }); ok {
				if err := closer.ClosePrepared(); err != nil {
					return err
				}
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
