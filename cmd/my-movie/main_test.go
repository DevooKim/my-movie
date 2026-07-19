package main

import (
	"path/filepath"
	"testing"
)

func TestDatabaseCheckDoesNotRequireDiscordConfiguration(t *testing.T) {
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.sqlite"))
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("DISCORD_APPLICATION_ID", "")
	t.Setenv("DISCORD_GUILD_ID", "")

	if code := runDatabaseCheck(); code != 0 {
		t.Fatalf("exit code=%d", code)
	}
}
