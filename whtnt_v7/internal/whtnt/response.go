package whtnt

import (
	"encoding/json"
	"io"

	http "github.com/bogdanfinn/fhttp"
)

type httpResponse struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

func readResponse(resp *http.Response) (*httpResponse, error) {
	if resp == nil {
		return &httpResponse{}, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &httpResponse{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Body:       body,
	}, nil
}

func (r *httpResponse) String() string {
	if r == nil {
		return ""
	}
	return string(r.Body)
}

func (r *httpResponse) JSON(v any) error {
	if r == nil {
		return json.Unmarshal(nil, v)
	}
	return json.Unmarshal(r.Body, v)
}

func newGetRequest(url string, header http.Header) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = header
	return req, nil
}

func newPostRequest(url string, body io.Reader, header http.Header) (*http.Request, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header = header
	return req, nil
}
