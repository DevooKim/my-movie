package cgv

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/targets"
)

type transport interface {
	dates(context.Context, string) ([]string, error)
	showtimes(context.Context, string, string) ([]showtimeResponse, error)
}

type preparedTransport interface {
	transport
	Close() error
}

type sessionOpener interface {
	open(context.Context) (preparedTransport, error)
}

type Provider struct {
	transport transport
	now       func() time.Time
}

func New(cdpURL string, now func() time.Time) *Provider {
	return newProvider(newCDPTransport(cdpURL), now)
}
func newProvider(transport transport, now func() time.Time) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{transport: transport, now: now}
}
func (p *Provider) ID() domain.ProviderID { return domain.ProviderCGV }
func (p *Provider) FetchBranchSnapshot(ctx context.Context, branch domain.Branch) ([]domain.Showtime, error) {
	if opener, ok := p.transport.(sessionOpener); ok {
		prepared, err := opener.open(ctx)
		if err != nil {
			return nil, err
		}
		defer prepared.Close()
		return p.fetchWithTransport(ctx, branch, prepared)
	}
	return p.fetchWithTransport(ctx, branch, p.transport)
}

func (p *Provider) PrepareBranch(ctx context.Context, branch domain.Branch) (domain.PreparedBranchPoll, error) {
	if branch.Provider != domain.ProviderCGV {
		return nil, fmt.Errorf("cgv branch %q belongs to provider %q", branch.TheaterID, branch.Provider)
	}
	if opener, ok := p.transport.(sessionOpener); ok {
		prepared, err := opener.open(ctx)
		if err != nil {
			return nil, err
		}
		return &preparedBranchPoll{provider: p, branch: branch, transport: prepared, close: prepared.Close}, nil
	}
	return &preparedBranchPoll{provider: p, branch: branch, transport: p.transport, close: func() error { return nil }}, nil
}

type preparedBranchPoll struct {
	provider  *Provider
	branch    domain.Branch
	transport transport
	close     func() error

	mu          sync.Mutex
	terminalErr error
	closed      bool
}

func (p *preparedBranchPoll) Fetch(ctx context.Context) ([]domain.Showtime, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("cgv prepared poll is closed")
	}
	if p.terminalErr != nil {
		return nil, p.terminalErr
	}
	showtimes, err := p.provider.fetchWithTransport(ctx, p.branch, p.transport)
	if err != nil {
		p.terminalErr = err
	}
	return showtimes, err
}

func (p *preparedBranchPoll) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.close()
}

func (p *Provider) fetchWithTransport(ctx context.Context, branch domain.Branch, source transport) ([]domain.Showtime, error) {
	if branch.Provider != domain.ProviderCGV {
		return nil, fmt.Errorf("cgv branch %q belongs to provider %q", branch.TheaterID, branch.Provider)
	}
	dates, err := source.dates(ctx, branch.TheaterID)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.Showtime{}
	for _, date := range dates {
		rows, err := source.showtimes(ctx, branch.TheaterID, date)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := row.validate(); err != nil {
				return nil, err
			}
			target, ok := cgvTarget(branch.TheaterID, row.TcscnsGradCd)
			if !ok || row.SiteNo != branch.TheaterID {
				continue
			}
			playDate, startsAt, err := normalizeDateTime(row.ScnYmd, row.ScnsrtTm)
			if err != nil {
				return nil, err
			}
			endsAt := ""
			if row.ScnendTm != "" {
				_, endsAt, err = normalizeDateTime(row.ScnYmd, row.ScnendTm)
				if err != nil {
					return nil, err
				}
			}
			remaining, total, seatsKnown := parseCGVSeats(row.FrSeatCnt, row.Stcnt)
			id := strings.Join([]string{row.SiteNo, row.ScnYmd, row.ScnsNo, row.ScnSseq, row.MovNo}, "-")
			byID[id] = domain.Showtime{
				Provider: domain.ProviderCGV, TargetID: target.ID, TheaterID: branch.TheaterID, TheaterName: target.Theater.Name,
				MovieID: row.MovNo, MovieName: strings.TrimSpace(row.MovNm), MovieEnglishName: strings.TrimSpace(row.EngProdNm),
				ExternalID: id, PlayDate: playDate, StartsAt: startsAt, EndsAt: endsAt,
				Auditorium: strings.TrimSpace(row.ScnsNm), Format: strings.TrimSpace(row.MovkndDsplNm), Rating: strings.TrimSpace(row.CratgClsNm),
				RemainingSeats: remaining, TotalSeats: total, SeatCountKnown: seatsKnown, PosterURL: strings.TrimSpace(row.PosterPath),
			}
		}
	}
	result := make([]domain.Showtime, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PlayDate+result[i].StartsAt+result[i].ExternalID < result[j].PlayDate+result[j].StartsAt+result[j].ExternalID
	})
	return result, nil
}

func cgvTarget(theaterID, auditoriumCode string) (domain.AlertTarget, bool) {
	for _, target := range targets.All() {
		if target.Provider == domain.ProviderCGV && target.Theater.ID == theaterID && target.AuditoriumCode == auditoriumCode {
			return target, true
		}
	}
	return domain.AlertTarget{}, false
}

func parseCGVSeats(remainingRaw, totalRaw string) (int, int, bool) {
	remaining, remainingErr := strconv.Atoi(strings.TrimSpace(remainingRaw))
	total, totalErr := strconv.Atoi(strings.TrimSpace(totalRaw))
	if remainingErr != nil || totalErr != nil || remaining < 0 || total < 0 {
		return 0, 0, false
	}
	return remaining, total, true
}
func (p *Provider) BuildBookingLinks(target domain.AlertTarget, movieID string) domain.BookingLinks {
	return domain.BookingLinks{App: "https://m.cgv.co.kr/WebApp/Reservation/Reservation.aspx?movNo=" + movieID + "&theaterCd=" + target.Theater.ID, Web: "https://cgv.co.kr/ticket/?MOVIE_CD=" + movieID + "&THEATER_CD=" + target.Theater.ID}
}
func normalizeDateTime(rawDate, rawTime string) (string, string, error) {
	date, err := time.Parse("20060102", rawDate)
	if err != nil {
		return "", "", fmt.Errorf("invalid cgv play date %q: %w", rawDate, err)
	}
	if len(rawTime) != 4 {
		return "", "", fmt.Errorf("invalid cgv start time %q", rawTime)
	}
	hour, errH := strconv.Atoi(rawTime[:2])
	minute, errM := strconv.Atoi(rawTime[2:])
	if errH != nil || errM != nil || hour < 0 || hour >= 48 || minute < 0 || minute >= 60 {
		return "", "", fmt.Errorf("invalid cgv start time %q", rawTime)
	}
	date = date.AddDate(0, 0, hour/24)
	return date.Format("2006-01-02"), fmt.Sprintf("%02d:%02d", hour%24, minute), nil
}

type fakeTransport struct {
	dateValues     []string
	showtimeValues []showtimeResponse
}

func (f fakeTransport) dates(context.Context, string) ([]string, error) { return f.dateValues, nil }
func (f fakeTransport) showtimes(context.Context, string, string) ([]showtimeResponse, error) {
	return f.showtimeValues, nil
}
