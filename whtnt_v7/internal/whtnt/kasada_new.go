package whtnt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"

	hyper "github.com/Hyper-Solutions/hyper-sdk-go/v2"
	"github.com/Hyper-Solutions/hyper-sdk-go/v2/kasada"
	"github.com/google/uuid"
)

type Config struct {
	Session tls_client.HttpClient

	// APIKey is the Hyper Solutions API key.
	APIKey string

	// HyperDebug prints full Hyper/Kasada request and response diagnostics.
	HyperDebug bool

	// HyperProxy routes Hyper SDK API requests through the same proxy as the flow.
	HyperProxy bool

	// PageURL is the protected page you want to access.
	PageURL string

	// AcceptLanguage is the browser's accept-language header.
	// Should match your target region/language.
	AcceptLanguage string

	BrowserUserAgent       string
	BrowserSecChUa         string
	BrowserSecChUaPlatform string
	KasadaVersion          string

	// Mobile enables iOS/mobile mode for the Kasada flow.
	// When true, the solver uses api.whatnot.com with iOS-style headers
	// instead of www.whatnot.com with Chrome headers.
	Mobile bool

	// MobileUserAgent is the iOS app user-agent string.
	// Required when Mobile is true.
	// Example: "Whatnot v26.33.5 (18), iOS 26.1, iPhone18,1"
	MobileUserAgent string

	// ProxyURL is the flow's proxy; Hyper API requests use it when HyperProxy is set.
	ProxyURL string

	// SkipFetchPage skips the initial page GET in the web flow. Use when
	// the caller already visited the page and only needs Kasada cookies.
	SkipFetchPage bool
}

// =============================================================================
// CONSTANTS
// =============================================================================

const (
	WEB_HOST = "www.whatnot.com"

	// Fixed Kasada paths
	KasadaBasePath = "/149e9513-01fa-4fb0-aad4-566afd725d1b/2d206a39-8ed7-437e-a3be-862e0f06eea3"

	AcceptLanguage = "en-GB"
)

// =============================================================================
// KASADA SOLVER
// =============================================================================

// KasadaSolver handles the complete Kasada bypass flow.
type KasadaSolver struct {
	config   *Config
	session  tls_client.HttpClient
	hyperAPI *hyper.Session

	// Derived from PageURL
	domain  string
	baseURL string

	// Internal state
	ip        string
	ipsScript string
	ipsLink   string

	// Headers from /tl response
	tlHeaders struct {
		Ct string
		St int
		V  string
	}

	// Headers from /mfc response
	mfcHeaders struct {
		Fc string
		H  string
	}
}

// NewKasadaSolver creates a new solver instance.
func NewKasadaSolver(ctx context.Context, config *Config) (*KasadaSolver, error) {
	if config == nil {
		return nil, errors.New("kasada config is required")
	}
	if config.Session == nil {
		return nil, errors.New("kasada session is required")
	}
	if config.APIKey == "" {
		return nil, errors.New("API key is required - get yours at https://hypersolutions.co")
	}
	if strings.TrimSpace(config.AcceptLanguage) == "" {
		config.AcceptLanguage = AcceptLanguage
	}

	var domain, baseURL string

	if config.Mobile {
		if strings.TrimSpace(config.MobileUserAgent) == "" {
			return nil, errors.New("MobileUserAgent is required when Mobile is true")
		}
		if strings.TrimSpace(config.KasadaVersion) == "" {
			config.KasadaVersion = defaultMobileKasadaVersion
		}
		domain = "api.whatnot.com"
		baseURL = "https://api.whatnot.com"
	} else {
		if config.PageURL == "" {
			return nil, errors.New("page URL is required")
		}
		if strings.TrimSpace(config.BrowserUserAgent) == "" {
			config.BrowserUserAgent = defaultBrowserUserAgent
		}
		if strings.TrimSpace(config.BrowserSecChUa) == "" {
			config.BrowserSecChUa = defaultBrowserSecChUa
		}
		if strings.TrimSpace(config.BrowserSecChUaPlatform) == "" {
			config.BrowserSecChUaPlatform = defaultBrowserSecChUaPlatform
		}
		if strings.TrimSpace(config.KasadaVersion) == "" {
			config.KasadaVersion = defaultKasadaVersion
		}

		parsedURL, err := url.Parse(config.PageURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse page URL: %w", err)
		}
		domain = parsedURL.Host
		baseURL = fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	}

	hyperAPI := hyper.NewSession(config.APIKey)
	if config.HyperDebug {
		hyperAPI.WithClient(newHyperDebugHTTPClient())
	} else if config.HyperProxy && strings.TrimSpace(config.ProxyURL) != "" {
		hyperAPI.WithClient(newProxiedHTTPClient(config.ProxyURL))
	}

	ip, err := getPublicIP(config.Session)

	if err != nil {
		return nil, fmt.Errorf("failed to get public IP: %w", err)
	}

	return &KasadaSolver{
		config:   config,
		session:  config.Session,
		hyperAPI: hyperAPI,
		domain:   domain,
		baseURL:  baseURL,
		ip:       ip,
	}, nil
}

func (s *KasadaSolver) isMobile() bool {
	return s != nil && s.config != nil && s.config.Mobile
}

func (s *KasadaSolver) effectiveUserAgent() string {
	if s.isMobile() && strings.TrimSpace(s.config.MobileUserAgent) != "" {
		return s.config.MobileUserAgent
	}
	return s.browserUserAgent()
}

func (s *KasadaSolver) effectiveKasadaVersion() string {
	if s.isMobile() {
		if s.config != nil && strings.TrimSpace(s.config.KasadaVersion) != "" {
			return s.config.KasadaVersion
		}
		return defaultMobileKasadaVersion
	}
	return s.kasadaVersion()
}

func (s *KasadaSolver) browserUserAgent() string {
	if s != nil && s.config != nil && strings.TrimSpace(s.config.BrowserUserAgent) != "" {
		return s.config.BrowserUserAgent
	}
	return defaultBrowserUserAgent
}

func (s *KasadaSolver) browserSecChUa() string {
	if s != nil && s.config != nil && strings.TrimSpace(s.config.BrowserSecChUa) != "" {
		return s.config.BrowserSecChUa
	}
	return defaultBrowserSecChUa
}

func (s *KasadaSolver) browserSecChUaPlatform() string {
	if s != nil && s.config != nil && strings.TrimSpace(s.config.BrowserSecChUaPlatform) != "" {
		return s.config.BrowserSecChUaPlatform
	}
	return defaultBrowserSecChUaPlatform
}

func (s *KasadaSolver) kasadaVersion() string {
	if s != nil && s.config != nil && strings.TrimSpace(s.config.KasadaVersion) != "" {
		return s.config.KasadaVersion
	}
	return defaultKasadaVersion
}

// Solve attempts to bypass Kasada protection and access the target page.
// Returns true if successful, false if blocked.
func (s *KasadaSolver) Solve(ctx context.Context) (bool, SolvedKasada, sessionCreateResult, error) {
	if s.isMobile() {
		return s.solveMobile(ctx)
	}
	if s.config != nil && s.config.SkipFetchPage {
		return s.solveFpOnly(ctx)
	}
	return s.solveWeb(ctx)
}

// solveFpOnly handles the web Kasada flow when the page was already visited
// by the caller. Skips fetchPage — which extracts session IDs the page may
// not contain — and goes straight to /fp.
func (s *KasadaSolver) solveFpOnly(ctx context.Context) (bool, SolvedKasada, sessionCreateResult, error) {
	s.session.SetFollowRedirect(false)
	defer s.session.SetFollowRedirect(true)

	result, err := s.solveFromFpEndpoint(ctx)
	if err != nil {
		return false, SolvedKasada{}, sessionCreateResult{}, fmt.Errorf("failed to solve kasada from /fp: %w", err)
	}

	return true, result, sessionCreateResult{}, nil
}

func (s *KasadaSolver) solveMobile(ctx context.Context) (bool, SolvedKasada, sessionCreateResult, error) {
	s.session.SetFollowRedirect(false)
	defer s.session.SetFollowRedirect(true)

	result, err := s.solveFromFpEndpoint(ctx)
	if err != nil {
		return false, SolvedKasada{}, sessionCreateResult{}, fmt.Errorf("failed to solve mobile kasada: %w", err)
	}

	return true, result, sessionCreateResult{
		sessionID:    uuid.NewString(),
		appSessionID: strings.ToUpper(uuid.NewString()),
	}, nil
}

func (s *KasadaSolver) solveWeb(ctx context.Context) (bool, SolvedKasada, sessionCreateResult, error) {
	s.session.SetFollowRedirect(false)
	defer s.session.SetFollowRedirect(true)

	// Step 1: Fetch the page and check for 429
	statusCode, pageBody, session, err := s.fetchPage(ctx)

	if err != nil {
		return false, SolvedKasada{}, sessionCreateResult{}, fmt.Errorf("failed to fetch page: %w", err)
	}

	var result SolvedKasada

	if statusCode == 429 {
		if result, err = s.solveFromBlockPage(ctx, pageBody); err != nil {
			return false, SolvedKasada{}, sessionCreateResult{}, fmt.Errorf("failed to solve from block page: %w", err)
		}

		statusCode, _, session, err = s.fetchPage(ctx)

		if err != nil {
			return false, SolvedKasada{}, sessionCreateResult{}, fmt.Errorf("failed to reload page: %w", err)
		}

		if statusCode != 200 {
			return false, SolvedKasada{}, sessionCreateResult{}, nil
		}
	} else {
		if result, err = s.solveFromFpEndpoint(ctx); err != nil {
			return false, SolvedKasada{}, sessionCreateResult{}, fmt.Errorf("failed to solve from /fp: %w", err)
		}
	}

	return true, result, session, nil
}

// fetchPage makes a GET request to the page URL.
func (s *KasadaSolver) fetchPage(ctx context.Context) (int, []byte, sessionCreateResult, error) {
	headers := http.Header{
		"cache-control":             {"max-age=0"},
		"sec-ch-ua":                 {s.browserSecChUa()},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {s.browserSecChUaPlatform()},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {s.browserUserAgent()},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {s.config.AcceptLanguage},
		"priority":                  {"u=0, i"},
		http.HeaderOrderKey:         {"cache-control", "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "upgrade-insecure-requests", "user-agent", "accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest", "accept-encoding", "accept-language", "cookie", "priority"},
	}

	req, err := http.NewRequest("GET", s.config.PageURL, nil)
	if err != nil {
		return 0, nil, sessionCreateResult{}, err
	}
	req.Header = headers

	resp, err := s.doRequestWithRetry(req)

	if err != nil {
		return 0, nil, sessionCreateResult{}, err
	}

	sessionIdRe := regexp.MustCompile(`usid\\":\\"(\w+-\w+-\w+-\w+-\w+)`)
	appSessionIdRe := regexp.MustCompile(`appsid\\":\\"(\w+-\w+-\w+-\w+-\w+)`)

	sessionIdMatch := sessionIdRe.FindStringSubmatch(resp.String())
	appSessionIdMatch := appSessionIdRe.FindStringSubmatch(resp.String())

	if len(sessionIdMatch) != 2 || len(appSessionIdMatch) != 2 {
		return 0, nil, sessionCreateResult{}, fmt.Errorf("unexpected session or app session id regexp result")
	}

	sessionId := sessionIdMatch[1]
	appSessionId := appSessionIdMatch[1]
	requestID := strings.Split(resp.Header.Get("cf-ray"), "-")[0]

	return resp.StatusCode, resp.Body, sessionCreateResult{
		sessionID:    sessionId,
		appSessionID: appSessionId,
		requestID:    requestID,
	}, nil
}

// solveFromBlockPage handles the flow when page returns 429.
func (s *KasadaSolver) solveFromBlockPage(ctx context.Context, blockPageBody []byte) (SolvedKasada, error) {
	// Extract ips.js URL from block page
	ipsPath, err := kasada.ParseScriptPath(bytes.NewReader(blockPageBody))
	if err != nil {
		return SolvedKasada{}, fmt.Errorf("failed to parse script path: %w", err)
	}

	s.ipsLink = s.baseURL + ipsPath

	// Fetch ips.js script
	if err := s.fetchIpsScript(ctx); err != nil {
		return SolvedKasada{}, err
	}

	// Generate and submit payload
	return s.solveChallenge(ctx)
}

// solveFromFpEndpoint handles the flow when page doesn't return 429.
func (s *KasadaSolver) solveFromFpEndpoint(ctx context.Context) (SolvedKasada, error) {
	// Request /fp endpoint to trigger 429
	fpURL := s.kasadaFpURL(s.effectiveKasadaVersion())

	var headers http.Header
	if s.isMobile() {
		headers = http.Header{
			"accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
			"sec-fetch-site":  {"none"},
			"x-kpsdk-h":       {"01"},
			"priority":        {"u=0, i"},
			"x-kpsdk-dv":      {"QkZWEmcDRUBEDloaAg8GABpSDxVEX1JfXBRZRQF5VRoJFFozUQtXBABeGwIPHA=="},
			"accept-encoding": {"gzip, deflate, br"},
			"accept-language": {s.config.AcceptLanguage},
			"sec-fetch-mode":  {"navigate"},
			"x-kpsdk-v":       {s.effectiveKasadaVersion()},
			"user-agent":      {s.effectiveUserAgent()},
			"sec-fetch-dest":  {"document"},
			http.HeaderOrderKey: {
				"accept", "sec-fetch-site", "x-kpsdk-h", "priority",
				"x-kpsdk-dv", "accept-encoding", "accept-language",
				"sec-fetch-mode", "x-kpsdk-v", "user-agent", "sec-fetch-dest",
			},
		}
	} else {
		headers = http.Header{
			"sec-ch-ua":                 {s.browserSecChUa()},
			"sec-ch-ua-mobile":          {"?0"},
			"sec-ch-ua-platform":        {s.browserSecChUaPlatform()},
			"upgrade-insecure-requests": {"1"},
			"user-agent":                {s.browserUserAgent()},
			"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
			"sec-fetch-site":            {"same-origin"},
			"sec-fetch-mode":            {"navigate"},
			"sec-fetch-dest":            {"iframe"},
			"referer":                   {s.config.PageURL},
			"accept-encoding":           {"gzip, deflate, br, zstd"},
			"accept-language":           {s.config.AcceptLanguage},
			"priority":                  {"u=0, i"},
			http.HeaderOrderKey: {
				"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
				"upgrade-insecure-requests", "user-agent", "accept", "sec-fetch-site",
				"sec-fetch-mode", "sec-fetch-dest", "referer", "accept-encoding",
				"accept-language", "priority",
			},
		}
	}

	req, err := http.NewRequest("GET", fpURL, nil)
	if err != nil {
		return SolvedKasada{}, err
	}
	req.Header = headers

	resp, err := s.doRequestWithRetry(req)

	if err != nil {
		return SolvedKasada{}, err
	}

	if resp.StatusCode != 429 {
		return SolvedKasada{}, fmt.Errorf("/fp returned unexpected status code: %d", resp.StatusCode)
	}

	// Extract ips.js URL
	ipsPath, err := kasada.ParseScriptPath(bytes.NewReader(resp.Body))
	if err != nil {
		return SolvedKasada{}, fmt.Errorf("failed to parse script path: %w", err)
	}

	s.ipsLink = s.baseURL + ipsPath

	// Fetch ips.js script
	if err := s.fetchIpsScript(ctx); err != nil {
		return SolvedKasada{}, err
	}

	// Generate and submit payload
	return s.solveChallenge(ctx)
}

// fetchIpsScript retrieves the ips.js script content.
func (s *KasadaSolver) fetchIpsScript(ctx context.Context) error {
	var headers http.Header
	if s.isMobile() {
		headers = http.Header{
			"accept":          {"*/*"},
			"sec-fetch-site":  {"same-origin"},
			"priority":        {"u=1, i"},
			"accept-encoding": {"gzip, deflate, br"},
			"sec-fetch-mode":  {"no-cors"},
			"accept-language": {s.config.AcceptLanguage},
			"user-agent":      {s.effectiveUserAgent()},
			"referer":         {s.kasadaFpURL("")},
			"sec-fetch-dest":  {"script"},
			http.HeaderOrderKey: {
				"accept", "sec-fetch-site", "priority", "accept-encoding",
				"sec-fetch-mode", "accept-language", "user-agent",
				"referer", "cookie", "sec-fetch-dest",
			},
		}
	} else {
		headers = http.Header{
			"sec-ch-ua-platform": {s.browserSecChUaPlatform()},
			"user-agent":         {s.browserUserAgent()},
			"sec-ch-ua":          {s.browserSecChUa()},
			"sec-ch-ua-mobile":   {"?0"},
			"accept":             {"*/*"},
			"sec-fetch-site":     {"same-origin"},
			"sec-fetch-mode":     {"no-cors"},
			"sec-fetch-dest":     {"script"},
			"referer":            {s.kasadaFpURL(s.kasadaVersion())},
			"accept-encoding":    {"gzip, deflate, br, zstd"},
			"accept-language":    {s.config.AcceptLanguage},
			"priority":           {"u=1"},
			http.HeaderOrderKey: {
				"sec-ch-ua-platform", "user-agent", "sec-ch-ua", "sec-ch-ua-mobile",
				"accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest",
				"referer", "accept-encoding", "accept-language", "cookie", "priority",
			},
		}
	}

	req, err := http.NewRequest("GET", s.ipsLink, nil)
	if err != nil {
		return err
	}
	req.Header = headers

	resp, err := s.doRequestWithRetry(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("ips.js request returned %d", resp.StatusCode)
	}

	s.ipsScript = string(resp.Body)

	return nil
}

type SolvedKasada struct {
	ct string
	st int
	fc string
	h  string
	im string
	dv string
	r  string
	v  string
	dt string
}

// solveChallenge generates payload and submits to /tl endpoint.
func (s *KasadaSolver) solveChallenge(ctx context.Context) (SolvedKasada, error) {

	// Generate payload
	input := &hyper.KasadaPayloadInput{
		UserAgent:      s.effectiveUserAgent(),
		IpsLink:        s.ipsLink,
		Script:         s.ipsScript,
		AcceptLanguage: s.config.AcceptLanguage,
		IP:             s.ip,
	}
	s.debugJSON("Hyper SDK input: GenerateKasadaPayload", input)
	payload, headers, err := s.hyperAPI.GenerateKasadaPayload(ctx, input)
	if err != nil {
		s.debugError("Hyper SDK error: GenerateKasadaPayload", err)
		s.sleepAfterHyperDebugError()
		return SolvedKasada{}, fmt.Errorf("Hyper API error: %w", err)
	}
	s.debugJSON("Hyper SDK headers: GenerateKasadaPayload", headers)
	s.debugBytes("Hyper SDK payload: GenerateKasadaPayload", payload)

	// Submit to /tl
	tlURL := fmt.Sprintf("%s%s/tl", s.baseURL, KasadaBasePath)
	kpsdkVersion := strings.TrimSpace(headers.XKpsdkV)
	if kpsdkVersion == "" {
		kpsdkVersion = s.effectiveKasadaVersion()
	}

	var reqHeaders http.Header
	if s.isMobile() {
		reqHeaders = http.Header{
			"referer":         {s.kasadaFpURL("")},
			"user-agent":      {s.effectiveUserAgent()},
			"origin":          {s.baseURL},
			"sec-fetch-dest":  {"empty"},
			"sec-fetch-site":  {"same-origin"},
			"priority":        {"u=3, i"},
			"accept-language": {s.config.AcceptLanguage},
			"accept":          {"*/*"},
			"content-type":    {"application/octet-stream"},
			"accept-encoding": {"gzip, deflate, br"},
			"sec-fetch-mode":  {"cors"},
			http.HeaderOrderKey: {
				"referer", "cookie", "user-agent", "origin",
				"sec-fetch-dest", "x-kpsdk-h", "sec-fetch-site",
				"content-length", "priority", "x-kpsdk-ct", "x-kpsdk-fc",
				"x-kpsdk-dt", "x-kpsdk-dv", "accept-language", "x-kpsdk-v",
				"x-kpsdk-im", "accept", "content-type", "accept-encoding",
				"sec-fetch-mode",
			},
		}
	} else {
		reqHeaders = http.Header{
			"sec-ch-ua-platform": {s.browserSecChUaPlatform()},
			"sec-ch-ua":          {s.browserSecChUa()},
			"sec-ch-ua-mobile":   {"?0"},
			"user-agent":         {s.browserUserAgent()},
			"content-type":       {"application/octet-stream"},
			"accept":             {"*/*"},
			"origin":             {s.baseURL},
			"sec-fetch-site":     {"same-origin"},
			"sec-fetch-mode":     {"cors"},
			"sec-fetch-dest":     {"empty"},
			"referer":            {s.kasadaFpURL(kpsdkVersion)},
			"accept-encoding":    {"gzip, deflate, br, zstd"},
			"accept-language":    {s.config.AcceptLanguage},
			"priority":           {"u=1, i"},
			http.HeaderOrderKey: {
				"content-length", "x-kpsdk-ct", "sec-ch-ua-platform", "x-kpsdk-dt",
				"sec-ch-ua", "x-kpsdk-im", "sec-ch-ua-mobile", "x-kpsdk-v",
				"user-agent", "content-type", "accept", "origin",
				"sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "referer",
				"accept-encoding", "accept-language", "cookie", "priority",
			},
		}
	}

	// Set Kasada headers from payload generation
	if headers.XKpsdkCt != "" {
		reqHeaders.Set("x-kpsdk-ct", headers.XKpsdkCt)
	}
	if headers.XKpsdkDt != "" {
		reqHeaders.Set("x-kpsdk-dt", headers.XKpsdkDt)
	}
	if headers.XKpsdkV != "" {
		reqHeaders.Set("x-kpsdk-v", headers.XKpsdkV)
	}
	if headers.XKpsdkR != "" {
		reqHeaders.Set("x-kpsdk-r", headers.XKpsdkR)
	}
	if headers.XKpsdkDv != "" {
		reqHeaders.Set("x-kpsdk-dv", headers.XKpsdkDv)
	}
	if headers.XKpsdkH != "" {
		reqHeaders.Set("x-kpsdk-h", headers.XKpsdkH)
	}
	if headers.XKpsdkFc != "" {
		reqHeaders.Set("x-kpsdk-fc", headers.XKpsdkFc)
	}
	if headers.XKpsdkIm != "" {
		reqHeaders.Set("x-kpsdk-im", headers.XKpsdkIm)
	}

	req, err := http.NewRequest("POST", tlURL, bytes.NewReader(payload))
	if err != nil {
		return SolvedKasada{}, err
	}
	req.Header = reqHeaders

	resp, err := s.doRequestWithRetry(req)
	if err != nil {
		return SolvedKasada{}, err
	}

	if resp.StatusCode != 200 {
		s.sleepAfterHyperDebugError()
		return SolvedKasada{}, fmt.Errorf("/tl returned %d", resp.StatusCode)
	}

	// Check for reload:true
	var tlResponse struct {
		Reload bool `json:"reload"`
	}

	if err := json.Unmarshal(resp.Body, &tlResponse); err != nil {
		s.sleepAfterHyperDebugError()
		return SolvedKasada{}, fmt.Errorf("failed to parse /tl response: %s, %w", resp.String(), err)
	}

	if !tlResponse.Reload {
		s.sleepAfterHyperDebugError()
		return SolvedKasada{}, errors.New("/tl did not return reload:true")
	}

	s.tlHeaders.Ct = resp.Header.Get("x-kpsdk-ct")
	s.tlHeaders.V = kpsdkVersion
	stStr := resp.Header.Get("x-kpsdk-st")

	if stStr != "" {
		s.tlHeaders.St, _ = strconv.Atoi(stStr)
	}

	if s.isMobile() {
		s.mfcHeaders.Fc = resp.Header.Get("x-kpsdk-fc")
		s.mfcHeaders.H = resp.Header.Get("x-kpsdk-h")
	}

	return SolvedKasada{
		ct: s.tlHeaders.Ct,
		st: s.tlHeaders.St,
		fc: headers.XKpsdkFc,
		h:  headers.XKpsdkH,
		im: headers.XKpsdkIm,
		dv: headers.XKpsdkDv,
		r:  headers.XKpsdkR,
		v:  kpsdkVersion,
		dt: headers.XKpsdkDt,
	}, nil
}

// generatePow generates the x-kpsdk-cd header for protected API requests.
func (s *KasadaSolver) generatePow(ctx context.Context) (string, error) {
	if !s.isMobile() {
		// Web flow: make /mfc request to get fc and h headers
		mfcURL := fmt.Sprintf("%s%s/mfc", s.baseURL, KasadaBasePath)
		kpsdkVersion := strings.TrimSpace(s.tlHeaders.V)
		if kpsdkVersion == "" {
			kpsdkVersion = s.effectiveKasadaVersion()
		}

		mfcHeaders := http.Header{
			"sec-ch-ua-platform": {s.browserSecChUaPlatform()},
			"x-kpsdk-h":          {"01"},
			"sec-ch-ua":          {s.browserSecChUa()},
			"sec-ch-ua-mobile":   {"?0"},
			"x-kpsdk-v":          {kpsdkVersion},
			"user-agent":         {s.browserUserAgent()},
			"accept":             {"*/*"},
			"sec-fetch-site":     {"same-origin"},
			"sec-fetch-mode":     {"cors"},
			"sec-fetch-dest":     {"empty"},
			"referer":            {s.kasadaFpURL(kpsdkVersion)},
			"accept-encoding":    {"gzip, deflate, br, zstd"},
			"accept-language":    {s.config.AcceptLanguage},
			"priority":           {"u=1, i"},
			http.HeaderOrderKey: {
				"sec-ch-ua-platform", "x-kpsdk-h", "sec-ch-ua", "sec-ch-ua-mobile",
				"x-kpsdk-v", "user-agent", "accept", "sec-fetch-site",
				"sec-fetch-mode", "sec-fetch-dest", "referer", "accept-encoding",
				"accept-language", "cookie", "priority",
			},
		}

		req, err := http.NewRequest("GET", mfcURL, nil)
		if err != nil {
			return "", err
		}
		req.Header = mfcHeaders

		resp, err := s.doRequestWithRetry(req)
		if err != nil {
			return "", err
		}

		s.mfcHeaders.Fc = resp.Header.Get("x-kpsdk-fc")
		s.mfcHeaders.H = resp.Header.Get("x-kpsdk-h")
	}
	// Mobile flow: fc and h already captured from /tl response

	// Generate POW
	var input = &hyper.KasadaPowInput{
		St: s.tlHeaders.St,
	}
	if !s.isMobile() {
		// Hyper web input
		input.Ct = s.tlHeaders.Ct
		input.Fc = s.mfcHeaders.Fc
		input.Domain = s.domain
	}
	s.debugJSON("Hyper SDK input: GenerateKasadaPow", input)
	cd, err := s.hyperAPI.GenerateKasadaPow(ctx, input)
	if err != nil {
		s.debugError("Hyper SDK error: GenerateKasadaPow", err)
		s.sleepAfterHyperDebugError()
		return "", fmt.Errorf("failed to generate POW: %w", err)
	}
	s.debugText("Hyper SDK response: GenerateKasadaPow", cd)

	if s.isMobile() {
		cd = reorderPowJSON(cd)
	}

	return cd, nil
}

type powOrdered struct {
	Answers  json.RawMessage `json:"answers"`
	St       json.RawMessage `json:"st"`
	D        json.RawMessage `json:"d"`
	Duration json.RawMessage `json:"duration"`
	Rst      json.RawMessage `json:"rst"`
	WorkTime json.RawMessage `json:"workTime"`
	ID       json.RawMessage `json:"id"`
}

func reorderPowJSON(raw string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return raw
	}
	ordered := powOrdered{
		Answers:  fields["answers"],
		St:       fields["st"],
		D:        fields["d"],
		Duration: fields["duration"],
		Rst:      fields["rst"],
		WorkTime: fields["workTime"],
		ID:       fields["id"],
	}
	out, err := json.Marshal(ordered)
	if err != nil {
		return raw
	}
	return string(out)
}

func (s *KasadaSolver) MfcH() string {
	if s == nil {
		return ""
	}
	return s.mfcHeaders.H
}

func (s *KasadaSolver) Close() {
	if s.session != nil {
		s.session.CloseIdleConnections()
	}
}

func (s *KasadaSolver) kasadaFpURL(version string) string {
	if s.isMobile() {
		return fmt.Sprintf("%s%s/fp", s.baseURL, KasadaBasePath)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = s.effectiveKasadaVersion()
	}
	return fmt.Sprintf("%s%s/fp?x-kpsdk-v=%s", s.baseURL, KasadaBasePath, version)
}

func getPublicIP(client tls_client.HttpClient) (string, error) {
	if client == nil {
		return "", errors.New("client is nil")
	}
	req, err := http.NewRequest("GET", "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}
	// Use a plain accept header; no user-agent so the tls_client's configured
	// profile UA is sent rather than the default Go-http-client string.
	req.Header.Set("accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}
