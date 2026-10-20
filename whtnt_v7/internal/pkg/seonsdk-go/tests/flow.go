package whatnottest

// MIRRORS internal/internal/whtnt/{acc.go,onboarding.go,gql.go,whtnt.go}.
// Implements the registration → onboarding (sans phone) → refresh flow
// the test invokes once per run.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/Noooste/azuretls-client"
	http "github.com/Noooste/fhttp"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/google/uuid"
)

// FlowConfig is everything the test needs to drive the live flow.
type FlowConfig struct {
	HyperKey string // Hyper Solutions API key
	Proxy    string // proxy URL passed to azuretls + SEON device
	Email    string // email popped from emails.txt

	// SeonToken is the encrypted SEON session token to submit at register time.
	// Built by the test after BuildDevice + FingerprintToken.
	SeonToken string

	// FirstName / Password / DisplayName drive the register payload.
	// Empty fields fall back to gofakeit defaults.
	FirstName   string
	LastName    string
	Password    string
	DisplayName string

	// AppType is "whatnot-android" or "whatnot-ios". Empty defaults to web-equivalent android.
	AppType string
}

// FlowResult captures the outcome of a single fingerprint validation attempt.
type FlowResult struct {
	Email           string
	SessionToken    string // SEON fingerprint token submitted to /register
	AccessToken     string // Whatnot access token returned by /register (empty on register failure)
	RefreshToken    string // Whatnot refresh token returned by /register (empty on register failure)
	Banned          bool   // true if the post-wait refresh response contained "banned"
	BannedAtInitial bool   // true if the very first refresh already came back banned
	RegisterStatus  int
	RefreshBefore   string // short body from the initial refresh (debug)
	RefreshAfter    string // short body from the post-wait refresh
}

// SuccessLoginResponse mirrors whtnt/dtos.go.
type SuccessLoginResponse struct {
	UserID      int `json:"user_id"`
	AccessToken struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	} `json:"access_token"`
	RefreshToken struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	} `json:"refresh_token"`
	Appsid string `json:"appsid"`
}

// Run executes the entire test scenario: register → initial refresh → onboarding (no phone)
// → wait → refresh again → return ban verdict.
func Run(ctx context.Context, cfg FlowConfig, waitBeforeRefresh time.Duration) (FlowResult, error) {
	result := FlowResult{Email: cfg.Email, SessionToken: cfg.SeonToken}

	if cfg.HyperKey == "" {
		return result, errors.New("hyper key required")
	}
	if cfg.SeonToken == "" {
		return result, errors.New("seon token required")
	}
	if cfg.Email == "" {
		return result, errors.New("email required")
	}

	if cfg.FirstName == "" {
		cfg.FirstName = gofakeit.FirstName()
	}
	if cfg.LastName == "" {
		cfg.LastName = gofakeit.LastName()
	}
	if cfg.Password == "" {
		cfg.Password = gofakeit.Password(true, true, true, true, false, 10)
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = cfg.FirstName + " " + cfg.LastName
	}
	if cfg.AppType == "" {
		cfg.AppType = "whatnot-android"
	}

	session := azuretls.NewSession()
	session.InsecureSkipVerify = true
	session.Browser = azuretls.Chrome
	session.GetClientHelloSpec = azuretls.GetLastChromeVersion
	if cfg.Proxy != "" {
		if err := session.SetProxy(cfg.Proxy); err != nil {
			return result, fmt.Errorf("set proxy: %w", err)
		}
	}
	defer session.Close()

	pageURL := fmt.Sprintf("https://%s/register?redirect=/login?source=logout&source=header", WebHost)
	solver, err := NewKasada(ctx, session, cfg.HyperKey, pageURL)
	if err != nil {
		return result, fmt.Errorf("init kasada: %w", err)
	}

	solved, ids, err := solver.Solve(ctx)
	if err != nil {
		return result, fmt.Errorf("kasada solve: %w", err)
	}
	if ids.AppSessionID == "" || ids.UserSessionID == "" {
		return result, fmt.Errorf("missing session IDs after solve: %+v", ids)
	}

	pow, err := solver.GeneratePow(ctx)
	if err != nil {
		return result, fmt.Errorf("kasada pow: %w", err)
	}

	registerStatus, registerDTO, err := submitRegister(session, cfg, solved, pow, ids)
	result.RegisterStatus = registerStatus
	result.AccessToken = registerDTO.AccessToken.Token
	result.RefreshToken = registerDTO.RefreshToken.Token
	if err != nil {
		return result, fmt.Errorf("register: %w", err)
	}

	// Initial refresh — if banned here, fingerprint already failed first checkpoint.
	body1, err := refreshSession(session, ids, cfg.AppType)
	result.RefreshBefore = shortBody(body1)
	if err != nil {
		return result, fmt.Errorf("initial refresh: %w", err)
	}
	if containsBanned(body1) {
		result.BannedAtInitial = true
		result.Banned = true
		return result, nil
	}

	// Onboarding (skip phone + referral).
	if err := runOnboarding(ctx, session, solver, ids, cfg); err != nil {
		return result, fmt.Errorf("onboarding: %w", err)
	}

	// Wait then refresh again — the actual ban-check the test cares about.
	if waitBeforeRefresh > 0 {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(waitBeforeRefresh):
		}
	}

	body2, err := refreshSession(session, ids, cfg.AppType)
	result.RefreshAfter = shortBody(body2)
	if err != nil {
		return result, fmt.Errorf("post-wait refresh: %w", err)
	}
	result.Banned = containsBanned(body2)

	return result, nil
}

// ---------------------------------------------------------------------------
// register
// ---------------------------------------------------------------------------

func submitRegister(session *azuretls.Session, cfg FlowConfig, solved SolvedKasada, pow string, ids SessionIDs) (int, SuccessLoginResponse, error) {
	var dto SuccessLoginResponse
	payload, err := json.Marshal(map[string]string{
		"app_type":     cfg.AppType,
		"display_name": cfg.DisplayName,
		"email":        cfg.Email,
		"session":      cfg.SeonToken,
		"password":     cfg.Password,
	})
	if err != nil {
		return 0, dto, err
	}

	headers := http.Header{
		"x-whatnot-app-session-id":      {ids.AppSessionID},
		"x-kpsdk-ct":                    {solved.Ct},
		"x-request-id":                  {ids.RequestID},
		"sec-ch-ua-platform":            {SecChUaPlatform},
		"sec-ch-ua":                     {SecChUa},
		"sec-ch-ua-mobile":              {"?0"},
		"x-kpsdk-v":                     {KasadaVersion},
		"x-whatnot-app-context":         {appContext(cfg.AppType)},
		"x-whatnot-app":                 {cfg.AppType},
		"x-kpsdk-cd":                    {pow},
		"accept":                        {"application/json, application/json"},
		"content-type":                  {"application/json, application/json"},
		"x-whatnot-app-user-session-id": {ids.AppSessionID},
		"x-whatnot-app-version":         {WhatnotAppVersion},
		"x-whatnot-app-pathname":        {"/"},
		"x-kpsdk-h":                     {solved.H},
		"x-whatnot-app-screen":          {"/"},
		"x-client-timezone":             {"Europe/Warsaw"},
		"user-agent":                    {UserAgent},
		"origin":                        {"https://api.whatnot.com"},
		"sec-fetch-site":                {"same-origin"},
		"sec-fetch-mode":                {"cors"},
		"sec-fetch-dest":                {"empty"},
		"referer":                       {"https://api.whatnot.com/"},
		"accept-encoding":               {"gzip, deflate, br, zstd"},
		"accept-language":               {AcceptLanguage},
		"priority":                      {"u=1, i"},
	}

	resp, err := session.Post("https://api.whatnot.com/api/v2/register", bytes.NewBuffer(payload), headers)
	if err != nil {
		return 0, dto, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, dto, fmt.Errorf("register returned %d: %s", resp.StatusCode, shortBody(resp.String()))
	}

	if err := resp.JSON(&dto); err != nil {
		return resp.StatusCode, dto, fmt.Errorf("decode register response: %w", err)
	}
	if err := storeSessionCookies(session, dto); err != nil {
		return resp.StatusCode, dto, err
	}
	return resp.StatusCode, dto, nil
}

func storeSessionCookies(session *azuretls.Session, dto SuccessLoginResponse) error {
	parsed, err := url.Parse(fmt.Sprintf("https://%s", WebHost))
	if err != nil {
		return err
	}
	cookies := []*http.Cookie{
		{Name: "__Secure-access-token", Value: dto.AccessToken.Token},
		{Name: "__Secure-refresh-token", Value: dto.RefreshToken.Token},
		{Name: "__Secure-access-token-fp", Value: "none"},
		{Name: "__Secure-refresh-token-fp", Value: "none"},
		{Name: "__Secure-is-http-only-auth", Value: "1"},
	}
	session.CookieJar.SetCookies(parsed, cookies)
	return nil
}

// ---------------------------------------------------------------------------
// refresh (the ban check)
// ---------------------------------------------------------------------------

func refreshSession(session *azuretls.Session, ids SessionIDs, appType string) (string, error) {
	liveID := uuid.NewString()
	headers := http.Header{
		"x-whatnot-app-session-id":      {ids.AppSessionID},
		"x-request-id":                  {ids.RequestID},
		"authorization":                 {"Cookie"},
		"sec-ch-ua-platform":            {SecChUaPlatform},
		"sec-ch-ua":                     {SecChUa},
		"sec-ch-ua-mobile":              {"?0"},
		"x-whatnot-app-context":         {appContext(appType)},
		"x-whatnot-app":                 {appType},
		"accept":                        {"application/json"},
		"content-type":                  {"application/json"},
		"x-whatnot-app-user-session-id": {ids.UserSessionID},
		"x-whatnot-app-version":         {WhatnotAppVersion},
		"x-whatnot-app-pathname":        {"/live/" + liveID},
		"x-whatnot-app-screen":          {"/live/?"},
		"x-whatnot-livestream-id":       {liveID},
		"x-client-timezone":             {"Europe/Warsaw"},
		"user-agent":                    {UserAgent},
		"origin":                        {"https://www.whatnot.com"},
		"sec-fetch-site":                {"same-origin"},
		"sec-fetch-mode":                {"cors"},
		"sec-fetch-dest":                {"empty"},
		"referer":                       {"https://www.whatnot.com/live/" + liveID},
		"accept-encoding":               {"gzip, deflate, br, zstd"},
		"accept-language":               {AcceptLanguage},
		"priority":                      {"u=1, i"},
	}

	resp, err := session.Post("https://www.whatnot.com/services/api/v2/refresh", bytes.NewBuffer([]byte("{}")), headers)
	if err != nil {
		return "", err
	}
	return resp.String(), nil
}

func containsBanned(body string) bool {
	return strings.Contains(strings.ToLower(body), "banned")
}

// ---------------------------------------------------------------------------
// onboarding (skip phone, skip referral)
// ---------------------------------------------------------------------------

func runOnboarding(ctx context.Context, session *azuretls.Session, solver *Kasada, ids SessionIDs, cfg FlowConfig) error {
	steps := []struct {
		op   string
		body gqlBody
	}{
		{
			op: "AcceptBuyerTermsMutation",
			body: gqlBody{
				OperationName: "AcceptBuyerTermsMutation",
				Variables:     map[string]any{"legalEntity": defaultLegalEntity},
				Query:         "mutation AcceptBuyerTermsMutation($legalEntity:LegalEntity){acceptBuyerTerms(buyerTermsLegalEntity:$legalEntity){user{id buyerTermsLegalEntity __typename}__typename}}",
			},
		},
		{
			op: "UpdateOnboardingCountryMutation",
			body: gqlBody{
				OperationName: "UpdateOnboardingCountryMutation",
				Variables:     map[string]any{"country": defaultCountry},
				Query:         "mutation UpdateOnboardingCountryMutation($country:String!){updateOnboardingCountry(country:$country)}",
			},
		},
		{
			op: "BulkFollowEntities",
			body: gqlBody{
				OperationName: "BulkFollowEntities",
				Variables:     map[string]any{"entityIds": []string{pokemonCategoryID}},
				Query:         "mutation BulkFollowEntities($entityIds:[ID]!){bulkFollowEntities(entityIds:$entityIds){success categories{id label image{id url(width:300 height:100 format:WEBP fit:COVER)__typename}__typename}livestream_tags{id label image{id url(width:300 height:100 format:WEBP fit:COVER)__typename}__typename}__typename}}",
			},
		},
		{
			op: "UpdateUsername",
			body: gqlBody{
				OperationName: "UpdateUsername",
				Variables:     map[string]any{"username": randomUsername()},
				Query:         "mutation UpdateUsername($username:String!){updateUsername(username:$username){user{id username __typename}__typename}}",
			},
		},
	}

	for _, step := range steps {
		if err := sendGql(ctx, session, solver, ids, cfg.AppType, step.op, step.body); err != nil {
			return fmt.Errorf("%s: %w", step.op, err)
		}
		// The production flow sleeps between steps; keep a small jitter so we don't slam the API.
		time.Sleep(time.Duration(800+rand.Intn(800)) * time.Millisecond)
	}
	return nil
}

type gqlBody struct {
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
	Query         string         `json:"query"`
}

func sendGql(ctx context.Context, session *azuretls.Session, solver *Kasada, ids SessionIDs, appType, op string, body gqlBody) error {
	pow, err := solver.GeneratePow(ctx)
	if err != nil {
		return fmt.Errorf("pow: %w", err)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("https://www.whatnot.com/services/graphql/?operationName=%s&ssr=0", op)
	headers := http.Header{
		"host":                          {"www.whatnot.com"},
		"user-agent":                    {UserAgent},
		"accept":                        {"*/*"},
		"accept-language":               {AcceptLanguage},
		"accept-encoding":               {"gzip, deflate, br, zstd"},
		"referer":                       {"https://www.whatnot.com/"},
		"content-type":                  {"application/json"},
		"x-whatnot-app":                 {appType},
		"x-whatnot-app-context":         {appContext(appType)},
		"x-whatnot-app-pathname":        {"/"},
		"x-whatnot-app-screen":          {"/"},
		"x-whatnot-app-session-id":      {ids.AppSessionID},
		"x-whatnot-app-version":         {WhatnotAppVersion},
		"x-request-id":                  {ids.RequestID},
		"x-whatnot-app-user-session-id": {ids.AppSessionID},
		"authorization":                 {"Cookie"},
		"x-client-timezone":             {"Europe/Warsaw"},
		"x-kpsdk-v":                     {KasadaVersion},
		"x-kpsdk-ct":                    {solver.tlCt},
		"x-kpsdk-cd":                    {pow},
		"x-kpsdk-h":                     {solver.mfcH},
		"origin":                        {"https://www.whatnot.com"},
		"sec-gpc":                       {"1"},
		"connection":                    {"keep-alive"},
		"sec-fetch-dest":                {"empty"},
		"sec-fetch-mode":                {"cors"},
		"sec-fetch-site":                {"same-origin"},
		"priority":                      {"u=4"},
		"sec-ch-ua":                     {SecChUa},
		"sec-ch-ua-platform":            {SecChUaPlatform},
		"sec-ch-ua-mobile":              {"?0"},
	}

	resp, err := session.Post(endpoint, bytes.NewBuffer(payload), headers)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d: %s", op, resp.StatusCode, shortBody(resp.String()))
	}
	return nil
}

func appContext(appType string) string {
	switch appType {
	case "whatnot-ios":
		return "ios/native"
	case "whatnot-android":
		return "android/native"
	default:
		return "next-js/browser"
	}
}

func randomUsername() string {
	return strings.ToLower(fmt.Sprintf("%s%s%d", gofakeit.FirstName(), gofakeit.LastName(), rand.Intn(100000)))
}
