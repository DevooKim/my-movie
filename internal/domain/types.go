package domain

type ProviderID string

const (
	ProviderCGV     ProviderID = "cgv"
	ProviderMegabox ProviderID = "megabox"
)

type Theater struct {
	ID       string
	Name     string
	AreaCode string
}

type Branch struct {
	Provider    ProviderID
	TheaterID   string
	TheaterName string
	AreaCode    string
}

type Showtime struct {
	Provider         ProviderID
	TargetID         string
	TheaterID        string
	TheaterName      string
	MovieID          string
	MovieName        string
	MovieEnglishName string
	ExternalID       string
	PlayDate         string
	StartsAt         string
	EndsAt           string
	Auditorium       string
	Format           string
	Rating           string
	RemainingSeats   int
	TotalSeats       int
	SeatCountKnown   bool
	PosterURL        string
}

type BookingLinks struct {
	App string
	Web string
}
