package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/httpx"
	"my-movie/internal/providers"
	"my-movie/internal/providers/megabox"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(arguments []string) int {
	if len(arguments) != 1 || (arguments[0] != string(domain.ProviderMegabox) && arguments[0] != string(domain.ProviderCGV)) {
		fmt.Fprintln(os.Stderr, "usage: provider-smoke <megabox|cgv>")
		return 1
	}
	registry := providers.New(megabox.New(httpx.NewClient(httpx.Options{}), time.Now))
	providerID := domain.ProviderID(arguments[0])
	provider, ok := registry.Get(providerID)
	if !ok {
		fmt.Fprintf(os.Stderr, "%s provider is disabled\n", providerID)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	movies, err := provider.SearchMovies(ctx, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	theaters, err := provider.SearchTheaters(ctx, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	result := smokeResult{Provider: providerID, Movies: len(movies), Theaters: len(theaters)}
	if len(movies) > 0 && len(theaters) > 0 {
		showtimes, err := provider.FetchShowtimes(ctx, theaters[0].ID, movies[0].ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		result.Showtimes = len(showtimes)
		if len(showtimes) > 0 {
			result.Sample = &showtimes[0]
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

type smokeResult struct {
	Provider  domain.ProviderID `json:"provider"`
	Movies    int               `json:"movies"`
	Theaters  int               `json:"theaters"`
	Showtimes int               `json:"showtimes"`
	Sample    *domain.Showtime  `json:"sample,omitempty"`
}
