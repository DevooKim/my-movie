package config

import (
	"strings"
	"testing"
)

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("DISCORD_APPLICATION_ID", "123")
	t.Setenv("DISCORD_GUILD_ID", "456")
	t.Setenv("DATABASE_PATH", "")
	t.Setenv("PORT", "")
	t.Setenv("TZ", "")
	t.Setenv("APP_LAUNCH_BASE_URL", "")
	t.Setenv("HEALTHCHECK_PING_URL", "")
}

func TestLoadAcceptsHTTPSAppLaunchBaseURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("APP_LAUNCH_BASE_URL", "https://example.lambda-url.ap-northeast-2.on.aws/")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppLaunchBaseURL != "https://example.lambda-url.ap-northeast-2.on.aws" {
		t.Fatalf("base URL=%q", cfg.AppLaunchBaseURL)
	}
}

func TestLoadRejectsNonHTTPSAppLaunchBaseURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("APP_LAUNCH_BASE_URL", "http://example.com")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_LAUNCH_BASE_URL") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadUsesOperationalDefaults(t *testing.T) {
	setRequiredEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabasePath != "/data/my-movie.sqlite" {
		t.Fatalf("path=%q", cfg.DatabasePath)
	}
	if cfg.Port != 3000 {
		t.Fatalf("port=%d", cfg.Port)
	}
	if cfg.Timezone != "Asia/Seoul" {
		t.Fatalf("timezone=%q", cfg.Timezone)
	}
}

func TestLoadRejectsMissingDiscordToken(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("DISCORD_BOT_TOKEN", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DISCORD_BOT_TOKEN") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("PORT", "70000")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("err=%v", err)
	}
}
