package domain

type AlertTarget struct {
	ID             string
	Provider       ProviderID
	Theater        Theater
	AuditoriumName string
	AuditoriumCode string
}

func (t AlertTarget) DisplayName() string {
	provider := string(t.Provider)
	switch t.Provider {
	case ProviderMegabox:
		provider = "메가박스"
	case ProviderCGV:
		provider = "CGV"
	}
	return provider + " " + t.Theater.Name + " · " + t.AuditoriumName
}
