package megabox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"my-movie/internal/domain"
	"my-movie/internal/httpx"
)

func TestFetchBranchSnapshotReturnsEveryDolbyMovieWithMetadata(t *testing.T) {
	response := bookingResponse{
		StatCd:          0,
		MovieFormDeList: []dateResponse{{PlayDe: "20260719", FormAt: "Y"}},
		MovieFormList: []scheduleResponse{
			{PlaySchdlNo: "dolby-1", BrchNo: "1351", MovieNo: "detail-1", RpstMovieNo: "m1", MovieNm: "호프", MovieEngNm: "Hope", PlayDe: "20260719", PlayStartTime: "19:10", PlayEndTime: "21:56", TheabExpoNm: "Dolby Cinema", TheabKindCd: "DBC", BokdAbleAt: "Y", RestSeatCnt: "57", TotSeatCnt: "144", AdmisClassCdNm: "15세이상관람가", PlayKindNm: "2D Dolby"},
			{PlaySchdlNo: "dolby-2", BrchNo: "1351", MovieNo: "detail-2", RpstMovieNo: "m2", MovieNm: "두 번째 영화", PlayDe: "20260719", PlayStartTime: "22:00", PlayEndTime: "24:10", TheabExpoNm: "Dolby Cinema", TheabKindCd: "DBC", BokdAbleAt: "Y", RestSeatCnt: "unknown", TotSeatCnt: "144"},
			{PlaySchdlNo: "normal", BrchNo: "1351", MovieNo: "detail-3", RpstMovieNo: "m3", MovieNm: "일반관 영화", PlayDe: "20260719", PlayStartTime: "12:00", PlayEndTime: "14:00", TheabExpoNm: "1관", TheabKindCd: "NOR", BokdAbleAt: "Y"},
		},
	}
	transport := &fixtureTransport{selectedByDate: map[string]bookingResponse{"20260719": response}}
	provider := newProvider(transport, func() time.Time { return time.Date(2026, 7, 19, 12, 0, 0, 0, time.Local) })

	got, err := provider.FetchBranchSnapshot(context.Background(), domain.Branch{Provider: domain.ProviderMegabox, TheaterID: "1351", TheaterName: "코엑스", AreaCode: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("showtimes=%+v", got)
	}
	first := got[0]
	if first.MovieName != "호프" || first.MovieEnglishName != "Hope" || first.EndsAt != "21:56" || first.RemainingSeats != 57 || first.TotalSeats != 144 || !first.SeatCountKnown {
		t.Fatalf("first=%+v", first)
	}
	if got[1].SeatCountKnown {
		t.Fatalf("invalid seats should be unknown: %+v", got[1])
	}
	if !reflect.DeepEqual(transport.selectedMovieIDs, []string{""}) {
		t.Fatalf("branch request selected a movie: %v", transport.selectedMovieIDs)
	}
}

func TestScheduleResponseDecodesNumericSeatCounts(t *testing.T) {
	var response bookingResponse
	err := json.Unmarshal([]byte(`{"statCd":0,"movieFormDeList":[],"movieFormList":[{"restSeatCnt":57,"totSeatCnt":144}]}`), &response)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.MovieFormList) != 1 || string(response.MovieFormList[0].RestSeatCnt) != "57" || string(response.MovieFormList[0].TotSeatCnt) != "144" {
		t.Fatalf("response=%+v", response)
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
	provider := newProvider(&fixtureTransport{}, time.Now)
	target := domain.AlertTarget{Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "branch/1"}}
	links := provider.BuildBookingLinks(target, "movie 1")
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
}

func TestTransportSendsBranchRequestWithoutMovieOrCookies(t *testing.T) {
	var got bookingRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if cookies := request.Cookies(); len(cookies) != 0 {
			t.Errorf("cookies=%v", cookies)
		}
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"statCd":0,"msg":"ok","movieFormDeList":[],"movieFormList":[]}`))
	}))
	defer server.Close()
	transport := newHTTPTransport(httpx.NewClient(httpx.Options{HTTPClient: server.Client(), MaxAttempts: 1}), server.URL)

	if _, err := transport.selected(context.Background(), selection{TheaterID: "1351", AreaCode: "10", PlayDate: "20260719", AuditoriumCode: "DBC"}); err != nil {
		t.Fatal(err)
	}
	if got.ArrMovieNo != "" || got.MovieNo1 != "" || got.BrchNo1 != "1351" || got.AreaCd1 != "10" || got.TheabKindCd1 != "DBC" {
		t.Fatalf("request=%+v", got)
	}
}

type fixtureTransport struct {
	selectedByDate   map[string]bookingResponse
	selectedMovieIDs []string
}

func (t *fixtureTransport) selected(_ context.Context, input selection) (bookingResponse, error) {
	t.selectedMovieIDs = append(t.selectedMovieIDs, input.MovieID)
	return t.selectedByDate[input.PlayDate], nil
}
