package megabox

import "fmt"

type bookingResponse struct {
	StatCd          int                `json:"statCd"`
	Message         string             `json:"msg"`
	MovieList       []movieResponse    `json:"movieList"`
	AreaBrchList    []theaterResponse  `json:"areaBrchList"`
	MovieFormDeList []dateResponse     `json:"movieFormDeList"`
	MovieFormList   []scheduleResponse `json:"movieFormList"`
}

type movieResponse struct {
	MovieNo string `json:"movieNo"`
	MovieNm string `json:"movieNm"`
}

type theaterResponse struct {
	BrchNo     string `json:"brchNo"`
	BrchNm     string `json:"brchNm"`
	AreaCd     string `json:"areaCd"`
	BrchFormAt string `json:"brchFormAt"`
}

type dateResponse struct {
	PlayDe string `json:"playDe"`
	FormAt string `json:"formAt"`
}

type scheduleResponse struct {
	PlaySchdlNo   string `json:"playSchdlNo"`
	BrchNo        string `json:"brchNo"`
	MovieNo       string `json:"movieNo"`
	RpstMovieNo   string `json:"rpstMovieNo"`
	PlayDe        string `json:"playDe"`
	PlayStartTime string `json:"playStartTime"`
	TheabExpoNm   string `json:"theabExpoNm"`
	BokdAbleAt    string `json:"bokdAbleAt"`
}

func (r bookingResponse) validateStatus() error {
	if r.StatCd != 0 {
		return fmt.Errorf("megabox response status %d: %s", r.StatCd, r.Message)
	}
	return nil
}

func (r bookingResponse) validateCatalog() error {
	if err := r.validateStatus(); err != nil {
		return err
	}
	if len(r.MovieList) == 0 || len(r.AreaBrchList) == 0 {
		return fmt.Errorf("megabox catalog is missing movies or theaters")
	}
	for index, movie := range r.MovieList {
		if movie.MovieNo == "" || movie.MovieNm == "" {
			return fmt.Errorf("megabox movie %d is missing required fields", index)
		}
	}
	for index, theater := range r.AreaBrchList {
		if theater.BrchNo == "" || theater.BrchNm == "" || theater.AreaCd == "" {
			return fmt.Errorf("megabox theater %d is missing required fields", index)
		}
	}
	return nil
}

func (r bookingResponse) validateSelected() error {
	if err := r.validateStatus(); err != nil {
		return err
	}
	if r.MovieFormDeList == nil || r.MovieFormList == nil {
		return fmt.Errorf("megabox selected response is missing schedule lists")
	}
	return nil
}

func (s scheduleResponse) validate() error {
	if s.PlaySchdlNo == "" || s.BrchNo == "" || s.MovieNo == "" || s.RpstMovieNo == "" ||
		s.PlayDe == "" || s.PlayStartTime == "" || s.TheabExpoNm == "" || s.BokdAbleAt == "" {
		return fmt.Errorf("megabox schedule is missing required fields")
	}
	return nil
}
