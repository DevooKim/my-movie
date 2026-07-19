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
	if got[1].TargetID != "cgv-yongsan-4dx" || got[1].SeatCountKnown {
		t.Fatalf("4dx=%+v", got[1])
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
