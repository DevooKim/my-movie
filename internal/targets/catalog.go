package targets

import "my-movie/internal/domain"

var catalog = []domain.AlertTarget{
	{ID: "megabox-coex-dolby", Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "1351", Name: "코엑스", AreaCode: "10"}, AuditoriumName: "Dolby Cinema", AuditoriumCode: "DBC"},
	{ID: "megabox-namhyeona-dolby", Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "0019", Name: "남양주현대아울렛 스페이스원", AreaCode: "30"}, AuditoriumName: "Dolby Cinema", AuditoriumCode: "DBC"},
	{ID: "cgv-yongsan-imax", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "IMAX", AuditoriumCode: "03"},
	{ID: "cgv-yongsan-4dx", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "4DX", AuditoriumCode: "02"},
	{ID: "cgv-yongsan-screenx", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "SCREENX", AuditoriumCode: "04"},
}

func All() []domain.AlertTarget {
	return append([]domain.AlertTarget(nil), catalog...)
}

func Find(id string) (domain.AlertTarget, bool) {
	for _, target := range catalog {
		if target.ID == id {
			return target, true
		}
	}
	return domain.AlertTarget{}, false
}

func MustFind(id string) domain.AlertTarget {
	target, ok := Find(id)
	if !ok {
		panic("unknown alert target: " + id)
	}
	return target
}
