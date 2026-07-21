package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDatabasePath = "/data/my-movie.sqlite"
	defaultPort         = 3000
	defaultTimezone     = "Asia/Seoul"
	PollInterval        = 3 * time.Minute
)

type Config struct {
	DiscordBotToken      string
	DiscordApplicationID string
	DiscordGuildID       string
	DatabasePath         string
	Port                 int
	Timezone             string
	AppLaunchBaseURL     string
	HealthcheckPingURL   string
}

func Load() (Config, error) {
	botToken, err := required("DISCORD_BOT_TOKEN")
	if err != nil {
		return Config{}, err
	}
	applicationID, err := required("DISCORD_APPLICATION_ID")
	if err != nil {
		return Config{}, err
	}
	guildID, err := required("DISCORD_GUILD_ID")
	if err != nil {
		return Config{}, err
	}

	port, err := positiveInt("PORT", defaultPort)
	if err != nil || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be between 1 and 65535")
	}
	appLaunchBaseURL, err := optionalHTTPSBaseURL("APP_LAUNCH_BASE_URL")
	if err != nil {
		return Config{}, err
	}
	healthcheckPingURL, err := optionalHTTPSBaseURL("HEALTHCHECK_PING_URL")
	if err != nil {
		return Config{}, err
	}

	return Config{
		DiscordBotToken:      botToken,
		DiscordApplicationID: applicationID,
		DiscordGuildID:       guildID,
		DatabasePath:         valueOrDefault("DATABASE_PATH", defaultDatabasePath),
		Port:                 port,
		Timezone:             valueOrDefault("TZ", defaultTimezone),
		AppLaunchBaseURL:     appLaunchBaseURL,
		HealthcheckPingURL:   healthcheckPingURL,
	}, nil
}

func optionalHTTPSBaseURL(name string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must be an HTTPS base URL without credentials, query, or fragment", name)
	}
	return strings.TrimRight(raw, "/"), nil
}

func required(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func positiveInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
