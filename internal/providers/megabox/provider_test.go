package megabox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/httpx"
)

func TestSearchCatalogIsCaseInsensitive(t *testing.T) {
	provider, _ := newFixtureProvider(t)

	movies, err := provider.SearchMovies(context.Background(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 || movies[0] != (domain.Movie{ID: "m1", Name: "Sample Movie"}) {
		t.Fatalf("movies=%+v", movies)
	}
	theaters, err := provider.SearchTheaters(context.Background(), "coex")
	if err != nil {
		t.Fatal(err)
	}
	if len(theaters) != 1 || theaters[0] != (domain.Theater{ID: "1351", Name: "COEX", AreaCode: "10"}) {
		t.Fatalf("theaters=%+v", theaters)
	}
}

func TestFetchShowtimesQueriesAllBookableDatesAndNormalizes(t *testing.T) {
	provider, transport := newFixtureProvider(t)

	got, err := provider.FetchShowtimes(context.Background(), "1372", "m1")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Showtime{
		{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "schedule-1", PlayDate: "2026-07-19", StartsAt: "14:00", Auditorium: "6관"},
		{Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1", ExternalID: "schedule-2", PlayDate: "2026-07-20", StartsAt: "10:10", Auditorium: "5관"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("showtimes=%+v want=%+v", got, want)
	}
	if wantDates := []string{"20260719", "20260720"}; !reflect.DeepEqual(transport.selectedDates, wantDates) {
		t.Fatalf("selected dates=%v want=%v", transport.selectedDates, wantDates)
	}
}

func TestFetchShowtimesRejectsMalformedSuccess(t *testing.T) {
	provider, transport := newFixtureProvider(t)
	transport.selectedByDate["20260719"] = bookingResponse{
		StatCd:          0,
		MovieFormDeList: []dateResponse{{PlayDe: "20260719", FormAt: "Y"}},
		MovieFormList:   []scheduleResponse{{PlaySchdlNo: "", BrchNo: "1372", RpstMovieNo: "m1", PlayDe: "20260719", PlayStartTime: "14:00", TheabExpoNm: "6관", BokdAbleAt: "Y"}},
	}

	if _, err := provider.FetchShowtimes(context.Background(), "1372", "m1"); err == nil {
		t.Fatal("expected malformed response error")
	}
}

func TestFetchShowtimesRejectsSuccessWithoutSelectedPayload(t *testing.T) {
	provider, transport := newFixtureProvider(t)
	transport.selectedByDate["20260719"] = bookingResponse{StatCd: 0, Message: "ok"}

	if _, err := provider.FetchShowtimes(context.Background(), "1372", "m1"); err == nil {
		t.Fatal("expected missing selected payload error")
	}
}

func TestFetchShowtimesValidatesIdentityBeforeFiltering(t *testing.T) {
	provider, transport := newFixtureProvider(t)
	malformed := transport.selectedByDate["20260719"]
	malformed.MovieFormList = []scheduleResponse{{
		PlaySchdlNo: "schedule-1", BrchNo: "", MovieNo: "m1-detail", RpstMovieNo: "m1",
		PlayDe: "20260719", PlayStartTime: "14:00", TheabExpoNm: "6관", BokdAbleAt: "Y",
	}}
	transport.selectedByDate["20260719"] = malformed

	if _, err := provider.FetchShowtimes(context.Background(), "1372", "m1"); err == nil {
		t.Fatal("expected missing schedule identity error")
	}
}

func TestNormalizeScheduleDateTimeRollsExtendedHourIntoNextDay(t *testing.T) {
	playDate, startsAt, err := normalizeScheduleDateTime("20260719", "24:10")
	if err != nil {
		t.Fatal(err)
	}
	if playDate != "2026-07-20" || startsAt != "00:10" {
		t.Fatalf("playDate=%q startsAt=%q", playDate, startsAt)
	}
}

func TestNormalizeScheduleDateTimeRejectsInvalidExtendedTime(t *testing.T) {
	for _, start := range []string{"24:60", "48:00", "not-a-time"} {
		if _, _, err := normalizeScheduleDateTime("20260719", start); err == nil {
			t.Fatalf("start=%q: expected error", start)
		}
	}
}

func TestBookingLinksAreOfficialHTTPSAndEncodeIdentifiers(t *testing.T) {
	provider, _ := newFixtureProvider(t)
	links := provider.BuildBookingLinks("branch/1", "movie 1")
	for name, raw := range map[string]string{"app": links.App, "web": links.Web} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Scheme != "https" || !strings.HasSuffix(parsed.Hostname(), "megabox.co.kr") {
			t.Fatalf("%s link=%q", name, raw)
		}
		if parsed.Query().Get("rpstMovieNo") != "movie 1" || parsed.Query().Get("brchNo1") != "branch/1" {
			t.Fatalf("%s query=%v", name, parsed.Query())
		}
	}
	app, _ := url.Parse(links.App)
	if app.Path != "/re/AppOnly/booking" {
		t.Fatalf("app launch path=%q", app.Path)
	}
}

func TestTransportSendsOfficialRequestContractWithoutCookies(t *testing.T) {
	var got bookingRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != "application/json; charset=UTF-8" {
			t.Errorf("content-type=%q", request.Header.Get("Content-Type"))
		}
		if request.Header.Get("Origin") != officialOrigin || request.Header.Get("Referer") != officialReferer {
			t.Errorf("origin=%q referer=%q", request.Header.Get("Origin"), request.Header.Get("Referer"))
		}
		if cookies := request.Cookies(); len(cookies) != 0 {
			t.Errorf("cookies=%v", cookies)
		}
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"statCd":0,"msg":"ok","movieList":[],"areaBrchList":[],"movieFormDeList":[],"movieFormList":[]}`))
	}))
	defer server.Close()
	transport := newHTTPTransport(httpx.NewClient(httpx.Options{HTTPClient: server.Client(), MaxAttempts: 1}), server.URL)

	if _, err := transport.selected(context.Background(), selection{MovieID: "m1", TheaterID: "1372", AreaCode: "10", PlayDate: "20260719"}); err != nil {
		t.Fatal(err)
	}
	if got.ArrMovieNo != "m1" || got.MovieNo1 != "m1" || got.BrchNo1 != "1372" || got.AreaCd1 != "10" || got.BrchNoListCnt != 1 {
		t.Fatalf("request=%+v", got)
	}
	if got.MovieNo2 != "" || got.BrchNo2 != "" || got.SellChnlCd != "" {
		t.Fatalf("unused fields are not blank: %+v", got)
	}
}

type fixtureTransport struct {
	bootstrapResponse bookingResponse
	selectedByDate    map[string]bookingResponse
	selectedDates     []string
}

func (t *fixtureTransport) bootstrap(context.Context, string) (bookingResponse, error) {
	return t.bootstrapResponse, nil
}

func (t *fixtureTransport) selected(_ context.Context, input selection) (bookingResponse, error) {
	t.selectedDates = append(t.selectedDates, input.PlayDate)
	return t.selectedByDate[input.PlayDate], nil
}

func newFixtureProvider(t *testing.T) (*Provider, *fixtureTransport) {
	t.Helper()
	bootstrap := readFixture(t, "bootstrap.json")
	selected := readFixture(t, "selected_schedule.json")
	secondDate := bookingResponse{
		StatCd:          0,
		MovieFormDeList: selected.MovieFormDeList,
		MovieFormList: []scheduleResponse{
			{PlaySchdlNo: "schedule-1", BrchNo: "1372", MovieNo: "m1-detail", RpstMovieNo: "m1", PlayDe: "20260719", PlayStartTime: "14:00", TheabExpoNm: "6관", BokdAbleAt: "Y"},
			{PlaySchdlNo: "schedule-2", BrchNo: "1372", MovieNo: "m1-detail", RpstMovieNo: "m1", PlayDe: "20260720", PlayStartTime: "10:10", TheabExpoNm: "5관", BokdAbleAt: "Y"},
		},
	}
	transport := &fixtureTransport{
		bootstrapResponse: bootstrap,
		selectedByDate: map[string]bookingResponse{
			"20260719": selected,
			"20260720": secondDate,
		},
	}
	now := func() time.Time { return time.Date(2026, 7, 19, 12, 0, 0, 0, time.Local) }
	return newProvider(transport, now), transport
}

func readFixture(t *testing.T, name string) bookingResponse {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "megabox", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var response bookingResponse
	if err := json.Unmarshal(contents, &response); err != nil {
		t.Fatal(err)
	}
	return response
}
