package whtnt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
)

const (
	webGraphQLEndpoint = "https://www.whatnot.com/services/graphql/"
	webInviteBaseURL   = "https://www.whatnot.com/invite/"
)

type WebReferralResult struct {
	Success bool
	Error   string
}

// orderedGqlBody guarantees JSON key order: operationName, variables, query.
// Go maps serialize in random order which is a fingerprint.
type orderedGqlBody struct {
	OperationName string            `json:"operationName"`
	Variables     map[string]string `json:"variables"`
	Query         string            `json:"query"`
}

func (m *Module) runWebPreRegistration(phoneNumber string, referrerCode string) error {
	if strings.TrimSpace(phoneNumber) == "" {
		return fmt.Errorf("web pre-registration requires a phone number")
	}
	if strings.TrimSpace(referrerCode) == "" {
		return fmt.Errorf("web pre-registration requires a referrer code")
	}

	m.log.Manager("Starting web pre-registration for referral...")

	proxy := ""
	if m.client != nil {
		proxy = m.client.GetProxy()
	}

	webClient, err := createChromeWebClient(proxy)
	if err != nil {
		return fmt.Errorf("create chrome web client: %w", err)
	}
	defer webClient.CloseIdleConnections()

	inviteURL := webInviteBaseURL + strings.TrimSpace(referrerCode)
	browserUA := m.browserUserAgent()
	secChUa := m.browserSecChUa()
	secChUaPlatform := m.browserSecChUaPlatform()
	acceptLanguage := "en-US,en;q=0.9"
	webAppVersion := defaultWhatnotWebAppVersion
	if m.config != nil && m.config.AppVersions.WebAppVersion != "" {
		webAppVersion = m.config.AppVersions.WebAppVersion
	}

	m.log.Manager("Visiting invite page: " + inviteURL)
	usid, err := webVisitInvitePage(webClient, inviteURL, browserUA, secChUa, secChUaPlatform, acceptLanguage)
	if err != nil {
		return fmt.Errorf("visit invite page: %w", err)
	}

	m.sleepDelay("after_web_invite_page")

	cfg := m.buildWebKasadaConfig(webClient, inviteURL, proxy)
	webKasada, err := NewKasadaSolver(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("web kasada solver init: %w", err)
	}

	m.log.Kasada("Solving web Kasada challenge for invite page...")
	solved, kasadaResult, _, err := webKasada.Solve(context.Background())
	if err != nil {
		return fmt.Errorf("web kasada solve: %w", err)
	}
	if !solved {
		return fmt.Errorf("web kasada challenge was not solved")
	}
	m.log.Kasada("Web Kasada challenge solved.")

	m.sleepDelay("after_web_kasada")

	m.log.Kasada("Generating web proof of work...")
	webPow, err := webKasada.generatePow(context.Background())
	if err != nil {
		return fmt.Errorf("web pow generation: %w", err)
	}

	kpsdkH := webKasada.MfcH()
	if kpsdkH == "" {
		kpsdkH = kasadaResult.h
	}

	appSessionID := usid
	if appSessionID == "" {
		appSessionID = uuid.NewString()
	}

	m.log.Manager("Submitting pre-signup referral contact...")
	result, err := webCreatePreSignUpReferralContact(
		webClient, phoneNumber, referrerCode, inviteURL,
		browserUA, secChUa, secChUaPlatform, acceptLanguage, webAppVersion, m.deviceTimezone(),
		kasadaResult, webPow, kpsdkH, appSessionID, webKasada,
	)
	if err != nil {
		return fmt.Errorf("create pre-signup referral contact: %w", err)
	}
	if !result.Success {
		if result.Error != "" {
			return fmt.Errorf("pre-signup referral contact failed: %s", result.Error)
		}
		return fmt.Errorf("pre-signup referral contact returned success=false")
	}

	m.log.Manager("Pre-signup referral contact created successfully.")

	webPostMutationRedirect(webClient, inviteURL, browserUA, secChUa, secChUaPlatform, acceptLanguage)

	m.sleepDelay("after_web_referral")

	return nil
}

func (m *Module) buildWebKasadaConfig(webClient tls_client.HttpClient, pageURL string, proxy string) *Config {
	apiKey := ""
	hyperDebug := false
	hyperProxy := false
	if m.config != nil {
		apiKey = m.config.HyperKey
		hyperDebug = m.config.HyperDebug
		hyperProxy = m.config.HyperProxy
	}

	kasadaVersion := ""
	if m.config != nil && m.config.AppVersions.Kasada.WebVersion != "" {
		kasadaVersion = m.config.AppVersions.Kasada.WebVersion
	}
	if kasadaVersion == "" {
		kasadaVersion = defaultKasadaVersion
	}

	return &Config{
		Session:                webClient,
		APIKey:                 apiKey,
		HyperDebug:             hyperDebug,
		HyperProxy:             hyperProxy,
		PageURL:                pageURL,
		AcceptLanguage:         "en-US,en;q=0.9",
		BrowserUserAgent:       m.browserUserAgent(),
		BrowserSecChUa:         m.browserSecChUa(),
		BrowserSecChUaPlatform: m.browserSecChUaPlatform(),
		KasadaVersion:          kasadaVersion,
		Mobile:                 false,
		SkipFetchPage:          true,
		ProxyURL:               proxy,
	}
}

func createChromeWebClient(proxy string) (tls_client.HttpClient, error) {
	jar := tls_client.NewCookieJar()
	opts := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithCookieJar(jar),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithInsecureSkipVerify(),
		tls_client.WithClientProfile(profiles.Chrome_133_PSK),
		tls_client.WithRandomTLSExtensionOrder(),
	}
	if proxy != "" {
		opts = append(opts, tls_client.WithProxyUrl(proxy))
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
}

// webVisitInvitePage loads the invite page and extracts the usid cookie for
// use as x-whatnot-app-session-id in subsequent requests.
func webVisitInvitePage(client tls_client.HttpClient, inviteURL, ua, secChUa, secChUaPlatform, acceptLang string) (string, error) {
	req, err := http.NewRequest("GET", inviteURL, nil)
	if err != nil {
		return "", err
	}
	req.Header = http.Header{
		"sec-ch-ua":                 {secChUa},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {secChUaPlatform},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {ua},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {acceptLang},
		"priority":                  {"u=0, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user",
			"sec-fetch-dest", "accept-encoding", "accept-language",
			"cookie", "priority",
		},
	}

	client.SetFollowRedirect(true)
	defer client.SetFollowRedirect(false)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("invite page request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("invite page returned %d", resp.StatusCode)
	}

	usid := extractUsidFromCookies(client, inviteURL)
	if usid == "" {
		usid = extractUsidFromBody(string(body))
	}

	return usid, nil
}

func extractUsidFromCookies(client tls_client.HttpClient, rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || client.GetCookieJar() == nil {
		return ""
	}
	for _, c := range client.GetCookieJar().Cookies(parsed) {
		if c.Name == "usid" {
			return c.Value
		}
	}
	return ""
}

var usidRe = regexp.MustCompile(`"usid":"([0-9a-f-]{36})"`)

func extractUsidFromBody(body string) string {
	match := usidRe.FindStringSubmatch(body)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func webCreatePreSignUpReferralContact(
	client tls_client.HttpClient,
	phoneNumber, referrerUsername, inviteURL string,
	ua, secChUa, secChUaPlatform, acceptLang, appVersion, timezone string,
	kasada SolvedKasada, pow string, kpsdkH string, appSessionID string, solver *KasadaSolver,
) (WebReferralResult, error) {
	body := orderedGqlBody{
		OperationName: "Invite_CreatePreSignUpReferralContact",
		Variables: map[string]string{
			"phoneNumber":      phoneNumber,
			"referrerUsername": referrerUsername,
		},
		Query: "mutation Invite_CreatePreSignUpReferralContact($phoneNumber: String!, $referrerUsername: String!) {\n  createPreSignUpReferralContact(\n    phoneNumber: $phoneNumber\n    referrerUsername: $referrerUsername\n  ) {\n    success\n    error {\n      ... on BaseError {\n        title\n        __typename\n      }\n      __typename\n    }\n    __typename\n  }\n}",
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return WebReferralResult{}, err
	}

	endpoint := webGraphQLEndpoint + "?operationName=Invite_CreatePreSignUpReferralContact"
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return WebReferralResult{}, err
	}

	webRequestID := webRandomHex(16)
	invitePath := strings.TrimPrefix(inviteURL, "https://www.whatnot.com")

	req.Header = http.Header{
		"x-whatnot-app-session-id":     {appSessionID},
		"x-kpsdk-ct":                   {kasada.ct},
		"sec-ch-ua-platform":           {secChUaPlatform},
		"authorization":                {"Cookie"},
		"sec-ch-ua":                    {secChUa},
		"sec-ch-ua-mobile":             {"?0"},
		"x-whatnot-web-request-id":     {webRequestID},
		"x-kpsdk-v":                    {solver.effectiveKasadaVersion()},
		"x-whatnot-app-context":        {"express/browser"},
		"x-whatnot-app":                {"whatnot-web"},
		"x-kpsdk-cd":                   {pow},
		"accept":                       {"*/*"},
		"apollographql-client-version": {appVersion},
		"content-type":                 {"application/json"},
		"apollographql-client-name":    {"web"},
		"x-whatnot-app-version":        {appVersion},
		"x-whatnot-app-pathname":       {invitePath},
		"accept-language":              {acceptLang},
		"x-kpsdk-h":                    {kpsdkH},
		"x-whatnot-app-screen":         {"/invite/?"},
		"x-client-timezone":            {timezone},
		"user-agent":                   {ua},
		"origin":                       {"https://www.whatnot.com"},
		"sec-fetch-site":               {"same-origin"},
		"sec-fetch-mode":               {"cors"},
		"sec-fetch-dest":               {"empty"},
		"referer":                      {inviteURL},
		"accept-encoding":              {"gzip, deflate, br, zstd"},
		"priority":                     {"u=1, i"},
		http.HeaderOrderKey: {
			"content-length", "x-whatnot-app-session-id", "x-kpsdk-ct",
			"sec-ch-ua-platform", "authorization", "sec-ch-ua",
			"sec-ch-ua-mobile", "x-whatnot-web-request-id", "x-kpsdk-v",
			"x-whatnot-app-context", "x-whatnot-app", "x-kpsdk-cd",
			"accept", "apollographql-client-version", "content-type",
			"apollographql-client-name", "x-whatnot-app-version",
			"x-whatnot-app-pathname", "accept-language", "x-kpsdk-h",
			"x-whatnot-app-screen", "x-client-timezone", "user-agent",
			"origin", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest",
			"referer", "accept-encoding", "cookie", "priority",
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return WebReferralResult{}, fmt.Errorf("referral contact request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return WebReferralResult{}, fmt.Errorf("read referral response: %w", err)
	}

	if resp.StatusCode != 200 {
		return WebReferralResult{}, fmt.Errorf("referral contact returned %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var dto struct {
		Data struct {
			CreatePreSignUpReferralContact struct {
				Success bool `json:"success"`
				Error   *struct {
					Title    string `json:"title"`
					Typename string `json:"__typename"`
				} `json:"error"`
			} `json:"createPreSignUpReferralContact"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &dto); err != nil {
		return WebReferralResult{}, fmt.Errorf("parse referral response: %w", err)
	}

	if len(dto.Errors) > 0 {
		return WebReferralResult{Error: dto.Errors[0].Message}, nil
	}

	result := dto.Data.CreatePreSignUpReferralContact
	errMsg := ""
	if result.Error != nil {
		errMsg = result.Error.Title
	}

	return WebReferralResult{
		Success: result.Success,
		Error:   errMsg,
	}, nil
}

// webPostMutationRedirect fires the GET redirect the real browser does after
// the mutation: invite/<code>?source=referral_invite&claimedPhoneReferral=1
func webPostMutationRedirect(client tls_client.HttpClient, inviteURL, ua, secChUa, secChUaPlatform, acceptLang string) {
	redirectURL := inviteURL + "?source=referral_invite&claimedPhoneReferral=1"
	req, err := http.NewRequest("GET", redirectURL, nil)
	if err != nil {
		return
	}
	req.Header = http.Header{
		"sec-ch-ua":                 {secChUa},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {secChUaPlatform},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {ua},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"referer":                   {inviteURL},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {acceptLang},
		"priority":                  {"u=0, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"upgrade-insecure-requests", "user-agent", "accept",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user",
			"sec-fetch-dest", "referer", "accept-encoding",
			"accept-language", "cookie", "priority",
		},
	}

	client.SetFollowRedirect(true)
	defer client.SetFollowRedirect(false)

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func webRandomHex(length int) string {
	b := make([]byte, length/2+1)
	for i := range b {
		b[i] = byte(i*37 + int(uuid.New()[i%16]))
	}
	s := fmt.Sprintf("%x", b)
	if len(s) > length {
		return s[:length]
	}
	return s
}
