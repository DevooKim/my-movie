package providers

import (
	"context"
	"testing"

	"my-movie/internal/domain"
)

func TestRegistryAdvertisesOnlyConfiguredProviders(t *testing.T) {
	registry := New(&stubProvider{id: domain.ProviderMegabox})

	ids := registry.IDs()
	if len(ids) != 1 || ids[0] != domain.ProviderMegabox {
		t.Fatalf("ids=%v", ids)
	}
	if _, ok := registry.Get(domain.ProviderCGV); ok {
		t.Fatal("disabled CGV provider was advertised")
	}
}

type stubProvider struct{ id domain.ProviderID }

func (p *stubProvider) ID() domain.ProviderID                                        { return p.id }
func (p *stubProvider) SearchMovies(context.Context, string) ([]domain.Movie, error) { return nil, nil }
func (p *stubProvider) SearchTheaters(context.Context, string) ([]domain.Theater, error) {
	return nil, nil
}
func (p *stubProvider) FetchShowtimes(context.Context, string, string) ([]domain.Showtime, error) {
	return nil, nil
}
func (p *stubProvider) BuildBookingLinks(string, string) domain.BookingLinks {
	return domain.BookingLinks{}
}
