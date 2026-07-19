package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"my-movie/internal/config"
	"my-movie/internal/control"
	"my-movie/internal/database"
	"my-movie/internal/discordbot"
	"my-movie/internal/domain"
	"my-movie/internal/health"
	"my-movie/internal/httpx"
	"my-movie/internal/notification"
	"my-movie/internal/providers/cgv"
	"my-movie/internal/providers/megabox"
	"my-movie/internal/scheduler"
)

type databaseCloser interface{ Close() error }
type healthServer interface {
	Start() error
	Shutdown(context.Context) error
}
type discordClient interface {
	Start() error
	StopAccepting() error
	Stop() error
}
type pollScheduler interface {
	Start(context.Context)
	Stop(context.Context) error
}

type components struct {
	database  databaseCloser
	health    healthServer
	discord   discordClient
	scheduler pollScheduler
}

type App struct {
	components
	rootCancel  context.CancelFunc
	shutdown    sync.Once
	shutdownErr error
	started     bool
}

func New(configuration config.Config) (*App, error) {
	db, err := database.Open(configuration.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	repository := database.NewRepository(db, time.Now)
	httpClient := httpx.NewClient(httpx.Options{})
	megaboxProvider := megabox.New(httpClient, time.Now)
	cgvProvider := cgv.New("http://lightpanda:9222", time.Now)
	session, err := discordbot.NewSession(configuration.DiscordBotToken)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create Discord session: %w", err)
	}
	notifier := discordbot.NewNotifier(session)
	linkProviders := map[domain.ProviderID]notification.LinkProvider{
		domain.ProviderMegabox: megaboxProvider,
		domain.ProviderCGV:     cgvProvider,
	}
	branchProviders := map[domain.ProviderID]scheduler.BranchProvider{
		domain.ProviderMegabox: megaboxProvider,
		domain.ProviderCGV:     cgvProvider,
	}
	controlProviders := map[domain.ProviderID]control.BranchProvider{
		domain.ProviderMegabox: megaboxProvider,
		domain.ProviderCGV:     cgvProvider,
	}
	channels := discordbot.NewChannelManager(session, func() string {
		if session.State != nil && session.State.User != nil && session.State.User.ID != "" {
			return session.State.User.ID
		}
		return configuration.DiscordApplicationID
	})
	controller := control.New(repository, channels, controlProviders)
	notifications := notification.NewService(repository, notifier, linkProviders, controller)
	bot := discordbot.NewBot(session, configuration.DiscordGuildID, controller)
	poller := scheduler.New(repository, notifications, branchProviders, scheduler.Options{Interval: configuration.PollInterval})
	healthHandler := health.NewHandler(repository, configuration.PollInterval, time.Now)
	healthServer := health.NewServer(configuration.Port, healthHandler)
	return newWithComponents(components{database: db, health: healthServer, discord: bot, scheduler: poller}), nil
}

func newWithComponents(input components) *App { return &App{components: input} }

func (a *App) Start() error {
	if err := a.health.Start(); err != nil {
		_ = a.database.Close()
		return fmt.Errorf("start health server: %w", err)
	}
	if err := a.discord.Start(); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return errors.Join(fmt.Errorf("start Discord: %w", err), a.health.Shutdown(cleanupCtx), a.database.Close())
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	a.rootCancel = cancel
	a.scheduler.Start(rootCtx)
	a.started = true
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	a.shutdown.Do(func() {
		if a.rootCancel != nil {
			a.rootCancel()
		}
		stopAcceptingErr := a.discord.StopAccepting()
		var schedulerErr error
		if a.started {
			schedulerErr = a.scheduler.Stop(ctx)
		}
		a.shutdownErr = errors.Join(stopAcceptingErr, schedulerErr, a.discord.Stop(), a.health.Shutdown(ctx), a.database.Close())
	})
	return a.shutdownErr
}
