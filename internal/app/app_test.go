package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestStartOrdersHealthDiscordAndScheduler(t *testing.T) {
	var events []string
	application := newWithComponents(components{
		database:  &fakeDatabase{onClose: func() { events = append(events, "database-close") }},
		health:    &fakeHealth{onStart: func() { events = append(events, "health") }},
		discord:   &fakeDiscord{onStart: func() { events = append(events, "discord") }},
		scheduler: &fakeScheduler{onStart: func() { events = append(events, "scheduler") }},
	})

	if err := application.Start(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"health", "discord", "scheduler"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v want=%v", events, want)
	}
}

func TestDiscordStartFailureCleansHealthAndDatabase(t *testing.T) {
	database := &fakeDatabase{}
	health := &fakeHealth{}
	application := newWithComponents(components{
		database: database, health: health,
		discord:   &fakeDiscord{startErr: errors.New("discord unavailable")},
		scheduler: &fakeScheduler{},
	})

	if err := application.Start(); err == nil {
		t.Fatal("expected startup error")
	}
	if health.shutdownCalls != 1 || database.closeCalls != 1 {
		t.Fatalf("health shutdown=%d database close=%d", health.shutdownCalls, database.closeCalls)
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	database := &fakeDatabase{}
	health := &fakeHealth{}
	discord := &fakeDiscord{}
	scheduler := &fakeScheduler{}
	application := newWithComponents(components{database: database, health: health, discord: discord, scheduler: scheduler})
	if err := application.Start(); err != nil {
		t.Fatal(err)
	}

	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.stopCalls != 1 || discord.stopCalls != 1 || health.shutdownCalls != 1 || database.closeCalls != 1 {
		t.Fatalf("stops scheduler=%d discord=%d health=%d database=%d", scheduler.stopCalls, discord.stopCalls, health.shutdownCalls, database.closeCalls)
	}
	if discord.stopAcceptingCalls != 1 {
		t.Fatalf("stop accepting calls=%d", discord.stopAcceptingCalls)
	}
}

func TestShutdownWaitsForPollToFinish(t *testing.T) {
	release := make(chan struct{})
	scheduler := &fakeScheduler{stopBlock: release}
	application := newWithComponents(components{database: &fakeDatabase{}, health: &fakeHealth{}, discord: &fakeDiscord{}, scheduler: scheduler})
	if err := application.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- application.Shutdown(context.Background()) }()
	select {
	case <-done:
		t.Fatal("shutdown returned before polling stopped")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDeadlineBoundsSchedulerWait(t *testing.T) {
	release := make(chan struct{})
	application := newWithComponents(components{
		database: &fakeDatabase{}, health: &fakeHealth{}, discord: &fakeDiscord{},
		scheduler: &fakeScheduler{stopBlock: release},
	})
	if err := application.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := application.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

type fakeDatabase struct {
	mu         sync.Mutex
	closeCalls int
	onClose    func()
}

func (d *fakeDatabase) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeCalls++
	if d.onClose != nil {
		d.onClose()
	}
	return nil
}

type fakeHealth struct {
	startErr      error
	shutdownCalls int
	onStart       func()
}

func (h *fakeHealth) Start() error {
	if h.onStart != nil {
		h.onStart()
	}
	return h.startErr
}
func (h *fakeHealth) Shutdown(context.Context) error { h.shutdownCalls++; return nil }

type fakeDiscord struct {
	startErr           error
	stopAcceptingCalls int
	stopCalls          int
	onStart            func()
}

func (d *fakeDiscord) Start() error {
	if d.onStart != nil {
		d.onStart()
	}
	return d.startErr
}
func (d *fakeDiscord) StopAccepting() error { d.stopAcceptingCalls++; return nil }
func (d *fakeDiscord) Stop() error          { d.stopCalls++; return nil }

type fakeScheduler struct {
	startCalls int
	stopCalls  int
	onStart    func()
	stopBlock  chan struct{}
}

func (s *fakeScheduler) Start(context.Context) {
	s.startCalls++
	if s.onStart != nil {
		s.onStart()
	}
}
func (s *fakeScheduler) Stop(ctx context.Context) error {
	s.stopCalls++
	if s.stopBlock != nil {
		select {
		case <-s.stopBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
