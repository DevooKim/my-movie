package megabox

import (
	"context"
	"net/http"

	"my-movie/internal/httpx"
)

const (
	officialOrigin   = "https://www.megabox.co.kr"
	officialReferer  = "https://www.megabox.co.kr/on/oh/ohb/SimpleBooking/simpleBookingPage.do"
	officialEndpoint = "https://www.megabox.co.kr/on/oh/ohb/SimpleBooking/selectBokdList.do"
)

type bookingTransport interface {
	bootstrap(context.Context, string) (bookingResponse, error)
	selected(context.Context, selection) (bookingResponse, error)
}

type selection struct {
	MovieID   string
	TheaterID string
	AreaCode  string
	PlayDate  string
}

type bookingRequest struct {
	ArrMovieNo    string `json:"arrMovieNo"`
	PlayDe        string `json:"playDe"`
	OnLoad        string `json:"onLoad"`
	BrchNoListCnt int    `json:"brchNoListCnt"`
	BrchNo1       string `json:"brchNo1"`
	BrchNo2       string `json:"brchNo2"`
	BrchNo3       string `json:"brchNo3"`
	BrchNo4       string `json:"brchNo4"`
	BrchNo5       string `json:"brchNo5"`
	AreaCd1       string `json:"areaCd1"`
	AreaCd2       string `json:"areaCd2"`
	AreaCd3       string `json:"areaCd3"`
	AreaCd4       string `json:"areaCd4"`
	AreaCd5       string `json:"areaCd5"`
	SpclbYn1      string `json:"spclbYn1"`
	SpclbYn2      string `json:"spclbYn2"`
	SpclbYn3      string `json:"spclbYn3"`
	SpclbYn4      string `json:"spclbYn4"`
	SpclbYn5      string `json:"spclbYn5"`
	TheabKindCd1  string `json:"theabKindCd1"`
	TheabKindCd2  string `json:"theabKindCd2"`
	TheabKindCd3  string `json:"theabKindCd3"`
	TheabKindCd4  string `json:"theabKindCd4"`
	TheabKindCd5  string `json:"theabKindCd5"`
	BrchAll       string `json:"brchAll"`
	BrchSpcl      string `json:"brchSpcl"`
	MovieNo1      string `json:"movieNo1"`
	MovieNo2      string `json:"movieNo2"`
	MovieNo3      string `json:"movieNo3"`
	SellChnlCd    string `json:"sellChnlCd"`
}

type httpTransport struct {
	client   *httpx.Client
	endpoint string
}

func newHTTPTransport(client *httpx.Client, endpoint string) *httpTransport {
	return &httpTransport{client: client, endpoint: endpoint}
}

func (t *httpTransport) bootstrap(ctx context.Context, playDate string) (bookingResponse, error) {
	return t.request(ctx, bookingRequest{PlayDe: playDate, OnLoad: "Y"})
}

func (t *httpTransport) selected(ctx context.Context, input selection) (bookingResponse, error) {
	return t.request(ctx, bookingRequest{
		ArrMovieNo: input.MovieID, PlayDe: input.PlayDate, BrchNoListCnt: 1,
		BrchNo1: input.TheaterID, AreaCd1: input.AreaCode, SpclbYn1: "N",
		MovieNo1: input.MovieID,
	})
}

func (t *httpTransport) request(ctx context.Context, body bookingRequest) (bookingResponse, error) {
	var response bookingResponse
	err := t.client.DoJSON(ctx, httpx.Request{
		Method: http.MethodPost,
		URL:    t.endpoint,
		Headers: map[string]string{
			"Content-Type": "application/json; charset=UTF-8",
			"Origin":       officialOrigin,
			"Referer":      officialReferer,
		},
		Body: body,
	}, &response, func() error { return response.validateStatus() })
	return response, err
}
