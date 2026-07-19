package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/httpx"
	"my-movie/internal/providers/cgv"
	"my-movie/internal/providers/megabox"
	"my-movie/internal/targets"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(arguments []string) int {
	if len(arguments) != 1 || (arguments[0] != string(domain.ProviderMegabox) && arguments[0] != string(domain.ProviderCGV)) {
		fmt.Fprintln(os.Stderr, "usage: provider-smoke <megabox|cgv>")
		return 1
	}
	client := httpx.NewClient(httpx.Options{})
	providerID := domain.ProviderID(arguments[0])
	var provider interface {
		FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error)
	}
	if providerID == domain.ProviderMegabox {
		provider = megabox.New(client, time.Now)
	} else {
		provider = cgv.New("http://lightpanda:9222", time.Now)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var target domain.AlertTarget
	for _, candidate := range targets.All() {
		if candidate.Provider == providerID {
			target = candidate
			break
		}
	}
	if target.ID == "" {
		fmt.Fprintln(os.Stderr, "provider has no configured target")
		return 2
	}
	showtimes, err := provider.FetchBranchSnapshot(ctx, domain.Branch{
		Provider: providerID, TheaterID: target.Theater.ID,
		TheaterName: target.Theater.Name, AreaCode: target.Theater.AreaCode,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	result := smokeResult{Provider: providerID, Branch: target.Theater.Name, Showtimes: len(showtimes)}
	if len(showtimes) > 0 {
		result.Sample = &showtimes[0]
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

type smokeResult struct {
	Provider  domain.ProviderID `json:"provider"`
	Branch    string            `json:"branch"`
	Showtimes int               `json:"showtimes"`
	Sample    *domain.Showtime  `json:"sample,omitempty"`
}
