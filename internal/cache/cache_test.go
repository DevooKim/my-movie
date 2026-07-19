package cache

import (
	"testing"
	"time"
)

func TestGetCachesValueUntilTTLExpires(t *testing.T) {
	now := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	cache := New[string, string](10*time.Minute, func() time.Time { return now })
	loads := 0
	loader := func() (string, error) {
		loads++
		return "value", nil
	}

	if _, err := cache.Get("movies", loader); err != nil {
		t.Fatal(err)
	}
	now = now.Add(9 * time.Minute)
	if _, err := cache.Get("movies", loader); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("loads before expiry=%d", loads)
	}

	now = now.Add(2 * time.Minute)
	if _, err := cache.Get("movies", loader); err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Fatalf("loads after expiry=%d", loads)
	}
}

func TestGetDoesNotCacheLoaderError(t *testing.T) {
	cache := New[string, string](10*time.Minute, time.Now)
	loads := 0
	loader := func() (string, error) {
		loads++
		if loads == 1 {
			return "", errFixture
		}
		return "value", nil
	}

	if _, err := cache.Get("movies", loader); err == nil {
		t.Fatal("expected loader error")
	}
	value, err := cache.Get("movies", loader)
	if err != nil {
		t.Fatal(err)
	}
	if value != "value" || loads != 2 {
		t.Fatalf("value=%q loads=%d", value, loads)
	}
}

type fixtureError string

func (e fixtureError) Error() string { return string(e) }

const errFixture = fixtureError("fixture error")
