package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func ShowtimeKey(showtime Showtime) string {
	if showtime.ExternalID != "" {
		return string(showtime.Provider) + ":" + showtime.ExternalID
	}

	identity := strings.Join([]string{
		string(showtime.Provider),
		strings.TrimSpace(showtime.TargetID),
		strings.TrimSpace(showtime.TheaterID),
		strings.TrimSpace(showtime.MovieID),
		strings.TrimSpace(showtime.PlayDate),
		strings.TrimSpace(showtime.StartsAt),
		strings.ToLower(strings.TrimSpace(showtime.Auditorium)),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return string(showtime.Provider) + ":" + hex.EncodeToString(sum[:])
}
