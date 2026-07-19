package megabox

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/httpx"
	"my-movie/internal/targets"
)

type Provider struct {
	transport bookingTransport
	now       func() time.Time
}

func New(client *httpx.Client, now func() time.Time) *Provider {
	if client == nil {
		client = httpx.NewClient(httpx.Options{})
	}
	if now == nil {
		now = time.Now
	}
	return newProvider(newHTTPTransport(client, officialEndpoint), now)
}

func newProvider(transport bookingTransport, now func() time.Time) *Provider {
	return &Provider{transport: transport, now: now}
}

func (p *Provider) ID() domain.ProviderID { return domain.ProviderMegabox }

func (p *Provider) FetchBranchSnapshot(ctx context.Context, branch domain.Branch) ([]domain.Showtime, error) {
	if branch.Provider != domain.ProviderMegabox {
		return nil, fmt.Errorf("megabox branch %q belongs to provider %q", branch.TheaterID, branch.Provider)
	}
	target, ok := targetForBranch(branch.TheaterID, "DBC")
	if !ok {
		return nil, fmt.Errorf("megabox branch %q is unsupported", branch.TheaterID)
	}
	currentDate := p.now().Format("20060102")
	first, err := p.transport.selected(ctx, selection{TheaterID: branch.TheaterID, AreaCode: branch.AreaCode, PlayDate: currentDate, AuditoriumCode: "DBC"})
	if err != nil {
		return nil, err
	}
	if err := first.validateSelected(); err != nil {
		return nil, err
	}
	dates, err := bookableDates(first.MovieFormDeList)
	if err != nil {
		return nil, err
	}
	responses := []bookingResponse{first}
	for _, playDate := range dates {
		if playDate == currentDate {
			continue
		}
		response, err := p.transport.selected(ctx, selection{TheaterID: branch.TheaterID, AreaCode: branch.AreaCode, PlayDate: playDate, AuditoriumCode: "DBC"})
		if err != nil {
			return nil, err
		}
		if err := response.validateSelected(); err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	byID := make(map[string]domain.Showtime)
	for _, response := range responses {
		for _, row := range response.MovieFormList {
			if err := row.validate(); err != nil {
				return nil, err
			}
			if row.BokdAbleAt != "Y" || row.BrchNo != branch.TheaterID || row.TheabKindCd != "DBC" {
				continue
			}
			playDate, startsAt, err := normalizeScheduleDateTime(row.PlayDe, row.PlayStartTime)
			if err != nil {
				return nil, err
			}
			endsAt := ""
			if row.PlayEndTime != "" {
				_, endsAt, err = normalizeScheduleDateTime(row.PlayDe, row.PlayEndTime)
				if err != nil {
					return nil, err
				}
			}
			remaining, total, seatsKnown := parseSeats(row.RestSeatCnt, row.TotSeatCnt)
			byID[row.PlaySchdlNo] = domain.Showtime{
				Provider: domain.ProviderMegabox, TargetID: target.ID,
				TheaterID: branch.TheaterID, TheaterName: target.Theater.Name,
				MovieID: row.RpstMovieNo, MovieName: strings.TrimSpace(row.MovieNm), MovieEnglishName: strings.TrimSpace(row.MovieEngNm),
				ExternalID: row.PlaySchdlNo, PlayDate: playDate, StartsAt: startsAt, EndsAt: endsAt,
				Auditorium: strings.TrimSpace(row.TheabExpoNm), Format: strings.TrimSpace(row.PlayKindNm), Rating: strings.TrimSpace(row.AdmisClassCdNm),
				RemainingSeats: remaining, TotalSeats: total, SeatCountKnown: seatsKnown, PosterURL: strings.TrimSpace(row.MoviePosterImg),
			}
		}
	}
	result := make([]domain.Showtime, 0, len(byID))
	for _, showtime := range byID {
		result = append(result, showtime)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PlayDate+result[i].StartsAt+result[i].ExternalID < result[j].PlayDate+result[j].StartsAt+result[j].ExternalID
	})
	return result, nil
}

func targetForBranch(theaterID, auditoriumCode string) (domain.AlertTarget, bool) {
	for _, target := range targets.All() {
		if target.Provider == domain.ProviderMegabox && target.Theater.ID == theaterID && target.AuditoriumCode == auditoriumCode {
			return target, true
		}
	}
	return domain.AlertTarget{}, false
}

func parseSeats(remainingRaw, totalRaw flexibleText) (int, int, bool) {
	remaining, remainingErr := strconv.Atoi(strings.TrimSpace(string(remainingRaw)))
	total, totalErr := strconv.Atoi(strings.TrimSpace(string(totalRaw)))
	if remainingErr != nil || totalErr != nil || remaining < 0 || total < 0 {
		return 0, 0, false
	}
	return remaining, total, true
}

func normalizeScheduleDateTime(playDate, startsAt string) (string, string, error) {
	date, err := time.Parse("20060102", playDate)
	if err != nil {
		return "", "", fmt.Errorf("invalid megabox play date %q: %w", playDate, err)
	}
	if len(startsAt) != 5 || startsAt[2] != ':' {
		return "", "", fmt.Errorf("invalid megabox start time %q", startsAt)
	}
	hour, hourErr := strconv.Atoi(startsAt[:2])
	minute, minuteErr := strconv.Atoi(startsAt[3:])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour >= 48 || minute < 0 || minute >= 60 {
		return "", "", fmt.Errorf("invalid megabox start time %q", startsAt)
	}
	date = date.AddDate(0, 0, hour/24)
	return date.Format("2006-01-02"), fmt.Sprintf("%02d:%02d", hour%24, minute), nil
}

func (p *Provider) BuildBookingLinks(target domain.AlertTarget, movieID string) domain.BookingLinks {
	query := url.Values{"rpstMovieNo": {movieID}, "brchNo1": {target.Theater.ID}}
	return domain.BookingLinks{
		App: "https://m.megabox.co.kr/re/AppOnly/booking?" + query.Encode(),
		Web: "https://www.megabox.co.kr/booking?" + query.Encode(),
	}
}

func bookableDates(input []dateResponse) ([]string, error) {
	var dates []string
	seen := make(map[string]bool)
	for _, date := range input {
		if date.FormAt != "Y" || seen[date.PlayDe] {
			continue
		}
		if _, err := time.Parse("20060102", date.PlayDe); err != nil {
			return nil, fmt.Errorf("invalid megabox bookable date %q: %w", date.PlayDe, err)
		}
		seen[date.PlayDe] = true
		dates = append(dates, date.PlayDe)
	}
	return dates, nil
}
