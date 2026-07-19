package domain

type ProviderID string

const (
	ProviderCGV     ProviderID = "cgv"
	ProviderMegabox ProviderID = "megabox"
)

type Movie struct {
	ID   string
	Name string
}

type Theater struct {
	ID       string
	Name     string
	AreaCode string
}

type Showtime struct {
	Provider   ProviderID
	TargetID   string
	TheaterID  string
	MovieID    string
	ExternalID string
	PlayDate   string
	StartsAt   string
	Auditorium string
}

type BookingLinks struct {
	App string
	Web string
}
