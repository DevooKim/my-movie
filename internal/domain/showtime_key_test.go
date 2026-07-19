package domain

import "testing"

func TestShowtimeKeyPrefersExternalID(t *testing.T) {
	showtime := Showtime{Provider: ProviderMegabox, ExternalID: "schedule-1"}

	if got := ShowtimeKey(showtime); got != "megabox:schedule-1" {
		t.Fatalf("key=%q", got)
	}
}

func TestShowtimeFallbackKeyNormalizesAuditorium(t *testing.T) {
	first := Showtime{
		Provider:   ProviderCGV,
		TheaterID:  "0013",
		MovieID:    "m1",
		PlayDate:   "2026-07-19",
		StartsAt:   "09:30",
		Auditorium: " IMAX관 ",
	}
	second := first
	second.Auditorium = "imax관"

	if ShowtimeKey(first) != ShowtimeKey(second) {
		t.Fatal("normalized fallback keys differ")
	}
}

func TestShowtimeFallbackKeyChangesWithStartTime(t *testing.T) {
	first := Showtime{
		Provider: ProviderCGV, TheaterID: "0013", MovieID: "m1",
		PlayDate: "2026-07-19", StartsAt: "09:30", Auditorium: "1관",
	}
	second := first
	second.StartsAt = "10:30"

	if ShowtimeKey(first) == ShowtimeKey(second) {
		t.Fatal("different showtimes share a key")
	}
}
