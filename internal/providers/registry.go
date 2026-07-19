package providers

import (
	"sort"

	"my-movie/internal/domain"
)

type Registry struct {
	providers map[domain.ProviderID]domain.TheaterProvider
}

func New(configured ...domain.TheaterProvider) *Registry {
	registry := &Registry{providers: make(map[domain.ProviderID]domain.TheaterProvider)}
	for _, provider := range configured {
		if provider != nil {
			registry.providers[provider.ID()] = provider
		}
	}
	return registry
}

func (r *Registry) Get(id domain.ProviderID) (domain.TheaterProvider, bool) {
	provider, ok := r.providers[id]
	return provider, ok
}

func (r *Registry) IDs() []domain.ProviderID {
	ids := make([]domain.ProviderID, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (r *Registry) Map() map[domain.ProviderID]domain.TheaterProvider {
	result := make(map[domain.ProviderID]domain.TheaterProvider, len(r.providers))
	for id, provider := range r.providers {
		result[id] = provider
	}
	return result
}
