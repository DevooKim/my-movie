package cgv

import (
	"context"
	"testing"
	"time"

	"my-movie/internal/domain"
)

func TestFetchBranchSnapshotSplitsPremiumTargetsAndKeepsMetadata(t *testing.T) {
	provider := newProvider(fakeTransport{
		dateValues: []string{"20260719"},
		showtimeValues: []showtimeResponse{
			{SiteNo: "0013", SiteNm: "용산아이파크몰", MovNo: "m1", MovNm: "호프", EngProdNm: "Hope", TcscnsGradCd: "03", ScnYmd: "20260719", ScnsNo: "001", ScnSseq: "2", ScnsNm: "IMAX관", ScnsrtTm: "1910", ScnendTm: "2156", FrSeatCnt: "57", Stcnt: "144", CratgClsNm: "15세이상관람가", MovkndDsplNm: "IMAX"},
			{SiteNo: "0013", SiteNm: "용산아이파크몰", MovNo: "m2", MovNm: "4DX 영화", TcscnsGradCd: "02", ScnYmd: "20260719", ScnsNo: "002", ScnSseq: "3", ScnsNm: "4DX관", ScnsrtTm: "2200", ScnendTm: "2410", FrSeatCnt: "-", Stcnt: "100", MovkndDsplNm: "4DX"},
			{SiteNo: "0013", MovNo: "m3", MovNm: "일반관 영화", TcscnsGradCd: "01", ScnYmd: "20260719", ScnsNo: "003", ScnSseq: "4", ScnsNm: "1관", ScnsrtTm: "1200"},
		},
	}, func() time.Time { return time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC) })

	got, err := provider.FetchBranchSnapshot(context.Background(), domain.Branch{Provider: domain.ProviderCGV, TheaterID: "0013", TheaterName: "용산아이파크몰"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("showtimes=%+v", got)
	}
	if got[0].TargetID != "cgv-yongsan-imax" || got[0].MovieName != "호프" || got[0].EndsAt != "21:56" || got[0].RemainingSeats != 57 || got[0].TotalSeats != 144 || !got[0].SeatCountKnown {
		t.Fatalf("imax=%+v", got[0])
	}
	if got[1].TargetID != "cgv-yongsan-4dx" || got[1].EndsAt != "24:10" || got[1].SeatCountKnown {
		t.Fatalf("4dx=%+v", got[1])
	}
}

func TestNormalizeDateTimePreservesCGVBusinessDateAndExtendedHour(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
	}{
		{raw: "1910", want: "19:10"},
		{raw: "2400", want: "24:00"},
		{raw: "2510", want: "25:10"},
		{raw: "4759", want: "47:59"},
	} {
		date, clock, err := normalizeDateTime("20260809", test.raw)
		if err != nil || date != "2026-08-09" || clock != test.want {
			t.Fatalf("raw=%s date=%s clock=%s err=%v", test.raw, date, clock, err)
		}
	}
}

func TestFetchBranchSnapshotRejectsRowsMissingStableIdentityBeforeFiltering(t *testing.T) {
	provider := newProvider(fakeTransport{
		dateValues:     []string{"20260719"},
		showtimeValues: []showtimeResponse{{MovNo: "m1", MovNm: "호프", TcscnsGradCd: "03", ScnYmd: "20260719", ScnsNo: "001", ScnSseq: "2", ScnsrtTm: "1910"}},
	}, time.Now)
	if _, err := provider.FetchBranchSnapshot(context.Background(), domain.Branch{Provider: domain.ProviderCGV, TheaterID: "0013"}); err == nil {
		t.Fatal("expected missing site identity error")
	}
}

func TestPrepareBranchServesBurstFromOneSession(t *testing.T) {
	session := &preparedTransportFixture{fakeTransport: fakeTransport{
		dateValues: []string{"20260719"},
		showtimeValues: []showtimeResponse{{
			SiteNo: "0013", MovNo: "m1", MovNm: "호프", TcscnsGradCd: "03",
			ScnYmd: "20260719", ScnsNo: "001", ScnSseq: "2", ScnsrtTm: "1910",
		}},
	}}
	opener := &openingTransportFixture{session: session}
	provider := newProvider(opener, time.Now)

	poll, err := provider.PrepareBranch(context.Background(), domain.Branch{Provider: domain.ProviderCGV, TheaterID: "0013"})
	if err != nil {
		t.Fatal(err)
	}
	// A cycle's burst reuses the one session it prepared.
	if _, err := poll.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := poll.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opener.openCalls != 1 || session.closeCalls != 0 {
		t.Fatalf("during cycle: open=%d close=%d", opener.openCalls, session.closeCalls)
	}
	// Ending the cycle closes the tab so its heap is freed.
	poll.Close()
	if session.closeCalls != 1 {
		t.Fatalf("close=%d", session.closeCalls)
	}
}

func TestPrepareBranchOpensFreshSessionEachCycle(t *testing.T) {
	session := &preparedTransportFixture{fakeTransport: fakeTransport{
		dateValues: []string{"20260719"},
		showtimeValues: []showtimeResponse{{
			SiteNo: "0013", MovNo: "m1", MovNm: "호프", TcscnsGradCd: "03",
			ScnYmd: "20260719", ScnsNo: "001", ScnSseq: "2", ScnsrtTm: "1910",
		}},
	}}
	opener := &openingTransportFixture{session: session}
	provider := newProvider(opener, time.Now)
	branch := domain.Branch{Provider: domain.ProviderCGV, TheaterID: "0013"}

	first, err := provider.PrepareBranch(context.Background(), branch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := provider.PrepareBranch(context.Background(), branch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	second.Close()

	// Two cycles → two fresh tabs opened and both closed.
	if opener.openCalls != 2 || session.closeCalls != 2 {
		t.Fatalf("open=%d close=%d", opener.openCalls, session.closeCalls)
	}
}

type preparedTransportFixture struct {
	fakeTransport
	closeCalls int
}

func (t *preparedTransportFixture) Close() error {
	t.closeCalls++
	return nil
}

type openingTransportFixture struct {
	session   *preparedTransportFixture
	openCalls int
}

func (t *openingTransportFixture) dates(ctx context.Context, theaterID string) ([]string, error) {
	return t.session.dates(ctx, theaterID)
}
func (t *openingTransportFixture) showtimes(ctx context.Context, theaterID, playDate string) ([]showtimeResponse, error) {
	return t.session.showtimes(ctx, theaterID, playDate)
}
func (t *openingTransportFixture) open(context.Context) (preparedTransport, error) {
	t.openCalls++
	return t.session, nil
}
