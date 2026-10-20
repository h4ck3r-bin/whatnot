package whatnottest

// MIRRORS internal/internal/whtnt/kasada_new.go.
// Pared-down Kasada solver: no debug logging, no global backoff, no botid.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	hyper "github.com/Hyper-Solutions/hyper-sdk-go/v2"
	"github.com/Hyper-Solutions/hyper-sdk-go/v2/kasada"
	"github.com/Noooste/azuretls-client"
	http "github.com/Noooste/fhttp"
)

// SessionIDs holds the IDs scraped from the register HTML.
type SessionIDs struct {
	UserSessionID string
	AppSessionID  string
	RequestID     string
}

// SolvedKasada bundles the headers required to make protected API calls.
type SolvedKasada struct {
	Ct string
	St int
	H  string
	V  string
}

// Kasada wraps the Hyper-driven Kasada bypass for whatnot.com.
type Kasada struct {
	session  *azuretls.Session
	hyperAPI *hyper.Session
	pageURL  string
	domain   string
	baseURL  string
	ip       string

	ipsLink   string
	ipsScript string

	tlCt string
	tlSt int
	tlV  string

	mfcFc string
	mfcH  string
}

// NewKasada constructs a solver. session must already have its proxy set.
func NewKasada(ctx context.Context, session *azuretls.Session, apiKey, pageURL string) (*Kasada, error) {
	if session == nil {
		return nil, errors.New("session is required")
	}
	if apiKey == "" {
		return nil, errors.New("hyper api key is required")
	}
	if pageURL == "" {
		return nil, errors.New("page url is required")
	}
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parse page url: %w", err)
	}

	ip, err := publicIP(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("resolve public ip: %w", err)
	}

	return &Kasada{
		session:  session,
		hyperAPI: hyper.NewSession(apiKey),
		pageURL:  pageURL,
		domain:   parsed.Host,
		baseURL:  fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host),
		ip:       ip,
	}, nil
}

// Solve runs the full bypass and returns Kasada headers + scraped session IDs.
func (k *Kasada) Solve(ctx context.Context) (SolvedKasada, SessionIDs, error) {
	k.session.MaxRedirects = 0
	defer func() { k.session.MaxRedirects = 10 }()

	status, body, ids, err := k.fetchPage()
	if err != nil {
		return SolvedKasada{}, SessionIDs{}, fmt.Errorf("fetch register page: %w", err)
	}

	if status == http.StatusTooManyRequests {
		if _, err := k.solveFromBlockPage(ctx, body); err != nil {
			return SolvedKasada{}, SessionIDs{}, fmt.Errorf("solve from block page: %w", err)
		}
		status, _, ids, err = k.fetchPage()
		if err != nil {
			return SolvedKasada{}, SessionIDs{}, fmt.Errorf("reload register page: %w", err)
		}
		if status != http.StatusOK {
			return SolvedKasada{}, SessionIDs{}, fmt.Errorf("post-solve fetch returned %d", status)
		}
	} else {
		if _, err := k.solveFromFp(ctx); err != nil {
			return SolvedKasada{}, SessionIDs{}, fmt.Errorf("solve from /fp: %w", err)
		}
	}

	return SolvedKasada{Ct: k.tlCt, St: k.tlSt, V: k.tlV, H: k.mfcH}, ids, nil
}

// GeneratePow returns the x-kpsdk-cd value for a protected API call.
// Solve must have been called successfully first.
func (k *Kasada) GeneratePow(ctx context.Context) (string, error) {
	mfcURL := fmt.Sprintf("%s%s/mfc", k.baseURL, KasadaBasePath)
	version := strings.TrimSpace(k.tlV)
	if version == "" {
		version = KasadaVersion
	}

	headers := http.Header{
		"sec-ch-ua-platform": {SecChUaPlatform},
		"x-kpsdk-h":          {"01"},
		"sec-ch-ua":          {SecChUa},
		"sec-ch-ua-mobile":   {"?0"},
		"x-kpsdk-v":          {version},
		"user-agent":         {UserAgent},
		"accept":             {"*/*"},
		"sec-fetch-site":     {"same-origin"},
		"sec-fetch-mode":     {"cors"},
		"sec-fetch-dest":     {"empty"},
		"referer":            {k.fpURL(version)},
		"accept-encoding":    {"gzip, deflate, br, zstd"},
		"accept-language":    {AcceptLanguage},
		"priority":           {"u=1, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua-platform", "x-kpsdk-h", "sec-ch-ua", "sec-ch-ua-mobile",
			"x-kpsdk-v", "user-agent", "accept", "sec-fetch-site", "sec-fetch-mode",
			"sec-fetch-dest", "referer", "accept-encoding", "accept-language",
			"cookie", "priority",
		},
	}

	resp, err := k.session.Do(&azuretls.Request{Method: "GET", Url: mfcURL, Header: headers})
	if err != nil {
		return "", fmt.Errorf("mfc: %w", err)
	}
	k.mfcFc = resp.Header.Get("x-kpsdk-fc")
	k.mfcH = resp.Header.Get("x-kpsdk-h")

	cd, err := k.hyperAPI.GenerateKasadaPow(ctx, &hyper.KasadaPowInput{
		St:     k.tlSt,
		Ct:     k.tlCt,
		Fc:     k.mfcFc,
		Domain: k.domain,
	})
	if err != nil {
		return "", fmt.Errorf("hyper pow: %w", err)
	}
	return cd, nil
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

var (
	usidRegex   = regexp.MustCompile(`usid\\":\\"(\w+-\w+-\w+-\w+-\w+)`)
	appsidRegex = regexp.MustCompile(`appsid\\":\\"(\w+-\w+-\w+-\w+-\w+)`)
)

func (k *Kasada) fetchPage() (int, []byte, SessionIDs, error) {
	headers := http.Header{
		"cache-control":             {"max-age=0"},
		"sec-ch-ua":                 {SecChUa},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {SecChUaPlatform},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {UserAgent},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {AcceptLanguage},
		"priority":                  {"u=0, i"},
		http.HeaderOrderKey: {
			"cache-control", "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept", "sec-fetch-site",
			"sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest", "accept-encoding",
			"accept-language", "cookie", "priority",
		},
	}

	resp, err := k.session.Do(&azuretls.Request{Method: "GET", Url: k.pageURL, Header: headers})
	if err != nil {
		return 0, nil, SessionIDs{}, err
	}

	body := resp.String()
	usid := usidRegex.FindStringSubmatch(body)
	appsid := appsidRegex.FindStringSubmatch(body)

	ids := SessionIDs{}
	if len(usid) == 2 {
		ids.UserSessionID = usid[1]
	}
	if len(appsid) == 2 {
		ids.AppSessionID = appsid[1]
	}
	if cf := resp.Header.Get("cf-ray"); cf != "" {
		ids.RequestID = strings.Split(cf, "-")[0]
	}
	return resp.StatusCode, resp.Body, ids, nil
}

func (k *Kasada) solveFromBlockPage(ctx context.Context, body []byte) (SolvedKasada, error) {
	ipsPath, err := kasada.ParseScriptPath(bytes.NewReader(body))
	if err != nil {
		return SolvedKasada{}, fmt.Errorf("parse script path: %w", err)
	}
	k.ipsLink = k.baseURL + ipsPath
	if err := k.fetchIps(); err != nil {
		return SolvedKasada{}, err
	}
	return k.solveChallenge(ctx)
}

func (k *Kasada) solveFromFp(ctx context.Context) (SolvedKasada, error) {
	fp := k.fpURL(KasadaVersion)
	headers := http.Header{
		"sec-ch-ua":                 {SecChUa},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {SecChUaPlatform},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {UserAgent},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-dest":            {"iframe"},
		"referer":                   {k.pageURL},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {AcceptLanguage},
		"priority":                  {"u=0, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept", "sec-fetch-site",
			"sec-fetch-mode", "sec-fetch-dest", "referer", "accept-encoding",
			"accept-language", "priority",
		},
	}
	resp, err := k.session.Do(&azuretls.Request{Method: "GET", Url: fp, Header: headers})
	if err != nil {
		return SolvedKasada{}, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		return SolvedKasada{}, fmt.Errorf("/fp returned %d (expected 429)", resp.StatusCode)
	}
	ipsPath, err := kasada.ParseScriptPath(bytes.NewReader(resp.Body))
	if err != nil {
		return SolvedKasada{}, fmt.Errorf("parse script path: %w", err)
	}
	k.ipsLink = k.baseURL + ipsPath
	if err := k.fetchIps(); err != nil {
		return SolvedKasada{}, err
	}
	return k.solveChallenge(ctx)
}

func (k *Kasada) fetchIps() error {
	headers := http.Header{
		"sec-ch-ua-platform": {SecChUaPlatform},
		"user-agent":         {UserAgent},
		"sec-ch-ua":          {SecChUa},
		"sec-ch-ua-mobile":   {"?0"},
		"accept":             {"*/*"},
		"sec-fetch-site":     {"same-origin"},
		"sec-fetch-mode":     {"no-cors"},
		"sec-fetch-dest":     {"script"},
		"referer":            {k.fpURL(KasadaVersion)},
		"accept-encoding":    {"gzip, deflate, br, zstd"},
		"accept-language":    {AcceptLanguage},
		"priority":           {"u=1"},
		http.HeaderOrderKey: {
			"sec-ch-ua-platform", "user-agent", "sec-ch-ua", "sec-ch-ua-mobile",
			"accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest",
			"referer", "accept-encoding", "accept-language", "cookie", "priority",
		},
	}
	resp, err := k.session.Do(&azuretls.Request{Method: "GET", Url: k.ipsLink, Header: headers})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ips.js returned %d", resp.StatusCode)
	}
	k.ipsScript = string(resp.Body)
	return nil
}

func (k *Kasada) solveChallenge(ctx context.Context) (SolvedKasada, error) {
	payload, payloadHeaders, err := k.hyperAPI.GenerateKasadaPayload(ctx, &hyper.KasadaPayloadInput{
		UserAgent:      UserAgent,
		IpsLink:        k.ipsLink,
		Script:         k.ipsScript,
		AcceptLanguage: AcceptLanguage,
		IP:             k.ip,
	})
	if err != nil {
		return SolvedKasada{}, fmt.Errorf("hyper payload: %w", err)
	}

	tlURL := fmt.Sprintf("%s%s/tl", k.baseURL, KasadaBasePath)
	version := strings.TrimSpace(payloadHeaders.XKpsdkV)
	if version == "" {
		version = KasadaVersion
	}

	reqHeaders := http.Header{
		"sec-ch-ua-platform": {SecChUaPlatform},
		"sec-ch-ua":          {SecChUa},
		"sec-ch-ua-mobile":   {"?0"},
		"user-agent":         {UserAgent},
		"content-type":       {"application/octet-stream"},
		"accept":             {"*/*"},
		"origin":             {k.baseURL},
		"sec-fetch-site":     {"same-origin"},
		"sec-fetch-mode":     {"cors"},
		"sec-fetch-dest":     {"empty"},
		"referer":            {k.fpURL(version)},
		"accept-encoding":    {"gzip, deflate, br, zstd"},
		"accept-language":    {AcceptLanguage},
		"priority":           {"u=1, i"},
		http.HeaderOrderKey: {
			"content-length", "x-kpsdk-ct", "sec-ch-ua-platform", "x-kpsdk-dt",
			"sec-ch-ua", "x-kpsdk-im", "sec-ch-ua-mobile", "x-kpsdk-v",
			"user-agent", "content-type", "accept", "origin",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "referer",
			"accept-encoding", "accept-language", "cookie", "priority",
		},
	}
	if payloadHeaders.XKpsdkCt != "" {
		reqHeaders.Set("x-kpsdk-ct", payloadHeaders.XKpsdkCt)
	}
	if payloadHeaders.XKpsdkDt != "" {
		reqHeaders.Set("x-kpsdk-dt", payloadHeaders.XKpsdkDt)
	}
	reqHeaders.Set("x-kpsdk-v", version)
	if payloadHeaders.XKpsdkR != "" {
		reqHeaders.Set("x-kpsdk-r", payloadHeaders.XKpsdkR)
	}
	if payloadHeaders.XKpsdkDv != "" {
		reqHeaders.Set("x-kpsdk-dv", payloadHeaders.XKpsdkDv)
	}
	if payloadHeaders.XKpsdkH != "" {
		reqHeaders.Set("x-kpsdk-h", payloadHeaders.XKpsdkH)
	}
	if payloadHeaders.XKpsdkFc != "" {
		reqHeaders.Set("x-kpsdk-fc", payloadHeaders.XKpsdkFc)
	}
	if payloadHeaders.XKpsdkIm != "" {
		reqHeaders.Set("x-kpsdk-im", payloadHeaders.XKpsdkIm)
	}

	resp, err := k.session.Do(&azuretls.Request{
		Method: "POST",
		Url:    tlURL,
		Body:   payload,
		Header: reqHeaders,
	})
	if err != nil {
		return SolvedKasada{}, fmt.Errorf("/tl: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return SolvedKasada{}, fmt.Errorf("/tl returned %d: %s", resp.StatusCode, shortBody(resp.String()))
	}
	var tl struct {
		Reload bool `json:"reload"`
	}
	if err := json.Unmarshal(resp.Body, &tl); err != nil {
		return SolvedKasada{}, fmt.Errorf("decode /tl: %w (%s)", err, shortBody(resp.String()))
	}
	if !tl.Reload {
		return SolvedKasada{}, errors.New("/tl did not return reload:true")
	}

	k.tlCt = resp.Header.Get("x-kpsdk-ct")
	k.tlV = version
	if st := resp.Header.Get("x-kpsdk-st"); st != "" {
		k.tlSt, _ = strconv.Atoi(st)
	}

	return SolvedKasada{Ct: k.tlCt, St: k.tlSt, V: k.tlV}, nil
}

func (k *Kasada) fpURL(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = KasadaVersion
	}
	return fmt.Sprintf("%s%s/fp?x-kpsdk-v=%s", k.baseURL, KasadaBasePath, v)
}

func publicIP(ctx context.Context, session *azuretls.Session) (string, error) {
	if session == nil {
		return "", errors.New("session is nil")
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = deadline
	resp, err := session.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(resp.Body)), nil
}
