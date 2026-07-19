package cgv

import "fmt"

type apiResponse[T any] struct {
	StatusCode    int    `json:"statusCode"`
	StatusMessage string `json:"statusMessage"`
	Data          T      `json:"data"`
}

type dateResponse struct {
	ScnYmd string `json:"scnYmd"`
}
type showtimeResponse struct {
	SiteNo       string `json:"siteNo"`
	SiteNm       string `json:"siteNm"`
	MovNo        string `json:"movNo"`
	MovNm        string `json:"movNm"`
	EngProdNm    string `json:"engProdNm"`
	TcscnsGradCd string `json:"tcscnsGradCd"`
	ScnYmd       string `json:"scnYmd"`
	ScnsNo       string `json:"scnsNo"`
	ScnSseq      string `json:"scnSseq"`
	ScnsNm       string `json:"scnsNm"`
	ScnsrtTm     string `json:"scnsrtTm"`
	ScnendTm     string `json:"scnendTm"`
	FrSeatCnt    string `json:"frSeatCnt"`
	Stcnt        string `json:"stcnt"`
	CratgClsNm   string `json:"cratgClsNm"`
	MovkndDsplNm string `json:"movkndDsplNm"`
	PosterPath   string `json:"posterPath"`
}

func (row showtimeResponse) validate() error {
	if row.SiteNo == "" || row.MovNo == "" || row.MovNm == "" || row.TcscnsGradCd == "" ||
		row.ScnYmd == "" || row.ScnsNo == "" || row.ScnSseq == "" || row.ScnsrtTm == "" {
		return fmt.Errorf("cgv showtime is missing required identity fields")
	}
	return nil
}
