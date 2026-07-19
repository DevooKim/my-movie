package main

import (
	_ "time/tzdata"

	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"my-movie/internal/app"
	"my-movie/internal/config"
	"my-movie/internal/database"
	"my-movie/internal/logx"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(arguments []string) int {
	if len(arguments) > 0 {
		switch arguments[0] {
		case "healthcheck":
			return runHealthcheck()
		case "database-check":
			return runDatabaseCheck()
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", arguments[0])
			return 1
		}
	}
	configuration, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	location, err := time.LoadLocation(configuration.Timezone)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	time.Local = location
	logger := logx.New(os.Stdout)
	slog.SetDefault(logger)
	application, err := app.New(configuration)
	if err != nil {
		logger.Error("application setup failed", "error", err)
		return 1
	}
	if err := application.Start(); err != nil {
		logger.Error("application startup failed", "error", err)
		return 1
	}
	logger.Info("movie alert service started", "port", configuration.Port, "poll_interval", configuration.PollInterval)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	signal.Stop(signals)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		logger.Error("application shutdown failed", "error", err)
		return 1
	}
	logger.Info("movie alert service stopped")
	return 0
}

func runHealthcheck() int {
	port := 3000
	if raw := os.Getenv("PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			fmt.Fprintln(os.Stderr, "PORT must be between 1 and 65535")
			return 1
		}
		port = parsed
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "health returned HTTP %d\n", response.StatusCode)
		return 1
	}
	return 0
}

func runDatabaseCheck() int {
	path := os.Getenv("DATABASE_PATH")
	if path == "" {
		path = "/data/my-movie.sqlite"
	}
	db, err := database.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()
	version, err := database.LatestMigrationVersion(context.Background(), db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("migration version %d\n", version)
	return 0
}
