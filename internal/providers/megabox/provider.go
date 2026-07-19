package megabox

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"my-movie/internal/cache"
	"my-movie/internal/domain"
	"my-movie/internal/httpx"
)

type catalog struct {
	movies   []domain.Movie
	theaters []domain.Theater
}

type Provider struct {
	transport bookingTransport
	now       func() time.Time
	catalog   *cache.Cache[string, catalog]
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
	return &Provider{transport: transport, now: now, catalog: cache.New[string, catalog](10*time.Minute, now)}
}

func (p *Provider) ID() domain.ProviderID { return domain.ProviderMegabox }

func (p *Provider) SearchMovies(ctx context.Context, query string) ([]domain.Movie, error) {
	catalog, err := p.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]domain.Movie, 0, len(catalog.movies))
	for _, movie := range catalog.movies {
		if query == "" || strings.Contains(strings.ToLower(movie.Name), query) {
			result = append(result, movie)
		}
	}
	return result, nil
}

func (p *Provider) SearchTheaters(ctx context.Context, query string) ([]domain.Theater, error) {
	catalog, err := p.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]domain.Theater, 0, len(catalog.theaters))
	for _, theater := range catalog.theaters {
		if query == "" || strings.Contains(strings.ToLower(theater.Name), query) {
			result = append(result, theater)
		}
	}
	return result, nil
}

func (p *Provider) FetchShowtimes(ctx context.Context, theaterID, movieID string) ([]domain.Showtime, error) {
	catalog, err := p.loadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var theater domain.Theater
	if !containsMovie(catalog.movies, movieID) {
		return nil, fmt.Errorf("megabox movie %q is not in the catalog", movieID)
	}
	for _, candidate := range catalog.theaters {
		if candidate.ID == theaterID {
			theater = candidate
			break
		}
	}
	if theater.ID == "" {
		return nil, fmt.Errorf("megabox theater %q is not in the catalog", theaterID)
	}

	currentDate := p.now().Format("20060102")
	first, err := p.transport.selected(ctx, selection{MovieID: movieID, TheaterID: theaterID, AreaCode: theater.AreaCode, PlayDate: currentDate})
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
		response, err := p.transport.selected(ctx, selection{MovieID: movieID, TheaterID: theaterID, AreaCode: theater.AreaCode, PlayDate: playDate})
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
		for _, schedule := range response.MovieFormList {
			if err := schedule.validate(); err != nil {
				return nil, err
			}
			if schedule.BokdAbleAt != "Y" || schedule.BrchNo != theaterID || schedule.RpstMovieNo != movieID {
				continue
			}
			playDate, startsAt, err := normalizeScheduleDateTime(schedule.PlayDe, schedule.PlayStartTime)
			if err != nil {
				return nil, err
			}
			byID[schedule.PlaySchdlNo] = domain.Showtime{
				Provider: domain.ProviderMegabox, TheaterID: theaterID, MovieID: movieID,
				ExternalID: schedule.PlaySchdlNo, PlayDate: playDate,
				StartsAt: startsAt, Auditorium: strings.TrimSpace(schedule.TheabExpoNm),
			}
		}
	}
	showtimes := make([]domain.Showtime, 0, len(byID))
	for _, showtime := range byID {
		showtimes = append(showtimes, showtime)
	}
	sort.Slice(showtimes, func(i, j int) bool {
		left := showtimes[i].PlayDate + showtimes[i].StartsAt + showtimes[i].ExternalID
		right := showtimes[j].PlayDate + showtimes[j].StartsAt + showtimes[j].ExternalID
		return left < right
	})
	return showtimes, nil
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

func (p *Provider) BuildBookingLinks(theaterID, movieID string) domain.BookingLinks {
	query := url.Values{"rpstMovieNo": {movieID}, "brchNo1": {theaterID}}
	return domain.BookingLinks{
		App: "https://m.megabox.co.kr/re/AppOnly/booking?" + query.Encode(),
		Web: "https://www.megabox.co.kr/booking?" + query.Encode(),
	}
}

func (p *Provider) loadCatalog(ctx context.Context) (catalog, error) {
	return p.catalog.Get("catalog", func() (catalog, error) {
		response, err := p.transport.bootstrap(ctx, p.now().Format("20060102"))
		if err != nil {
			return catalog{}, err
		}
		if err := response.validateCatalog(); err != nil {
			return catalog{}, err
		}
		result := catalog{movies: make([]domain.Movie, 0, len(response.MovieList)), theaters: make([]domain.Theater, 0, len(response.AreaBrchList))}
		for _, movie := range response.MovieList {
			result.movies = append(result.movies, domain.Movie{ID: movie.MovieNo, Name: movie.MovieNm})
		}
		for _, theater := range response.AreaBrchList {
			result.theaters = append(result.theaters, domain.Theater{ID: theater.BrchNo, Name: theater.BrchNm, AreaCode: theater.AreaCd})
		}
		return result, nil
	})
}

func containsMovie(movies []domain.Movie, id string) bool {
	for _, movie := range movies {
		if movie.ID == id {
			return true
		}
	}
	return false
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
