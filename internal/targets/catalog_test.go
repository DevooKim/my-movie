package targets_test

import (
	"reflect"
	"testing"

	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

func TestCatalogContainsOnlySupportedTargets(t *testing.T) {
	t.Parallel()
	want := []domain.AlertTarget{
		{ID: "megabox-coex-dolby", Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "1351", Name: "코엑스", AreaCode: "10"}, AuditoriumName: "Dolby Cinema", AuditoriumCode: "DBC"},
		{ID: "megabox-namhyeona-dolby", Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "0019", Name: "남양주현대아울렛 스페이스원", AreaCode: "30"}, AuditoriumName: "Dolby Cinema", AuditoriumCode: "DBC"},
		{ID: "cgv-yongsan-imax", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "IMAX", AuditoriumCode: "03"},
		{ID: "cgv-yongsan-4dx", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "4DX", AuditoriumCode: "02"},
		{ID: "cgv-yongsan-screenx", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "SCREENX", AuditoriumCode: "04"},
	}
	if got := targets.All(); !reflect.DeepEqual(got, want) {
		t.Fatalf("targets=%+v, want=%+v", got, want)
	}
}

func TestCatalogRejectsUnknownTarget(t *testing.T) {
	t.Parallel()
	if _, ok := targets.Find("forged"); ok {
		t.Fatal("unexpected target")
	}
}

func TestTargetDisplayName(t *testing.T) {
	t.Parallel()
	target := targets.MustFind("cgv-yongsan-imax")
	if got, want := target.DisplayName(), "CGV 용산아이파크몰 · IMAX"; got != want {
		t.Fatalf("DisplayName()=%q, want=%q", got, want)
	}
}
