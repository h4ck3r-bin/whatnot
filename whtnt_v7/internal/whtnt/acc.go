package whtnt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/google/uuid"
	"status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

type sessionCreateResult struct {
	sessionID    string
	appSessionID string
	requestID    string
}

const (
	accessTokenCookieName  = "__Secure-access-token"
	refreshTokenCookieName = "__Secure-refresh-token"
)

var errKasadaBanned = errors.New("session banned by kasada")
var errEmailAlreadyTaken = errors.New("email already taken")

const (
	maxRegistrationAttempts = 8
	maxValidationAttempts   = 3
	maxOnboardingAttempts   = 3
)

type registrationCompletionMode int

const (
	registrationCompletionCheckout registrationCompletionMode = iota
	registrationCompletionAccountOnly
	registrationCompletionSEONTester
	registrationCompletionPreparedCheckout
)

func (m *Module) checkIfAccountValid(sessionData sessionCreateResult) (bool, error) {
	m.log.Refresh("Checking account session...")
	liveID := uuid.NewString()

	res, err := m.postRequestWithRetry(
		"refresh session",
		"https://www.whatnot.com/services/api/v2/refresh",
		[]byte("{}"),
		http.Header{
			"x-whatnot-app-session-id":      {sessionData.appSessionID},
			"x-request-id":                  {sessionData.requestID},
			"authorization":                 {"Cookie"},
			"sec-ch-ua-platform":            {m.browserSecChUaPlatform()},
			"sec-ch-ua":                     {m.browserSecChUa()},
			"sec-ch-ua-mobile":              {"?0"},
			"x-whatnot-app-context":         {m.requestAppContext()},
			"x-whatnot-app":                 {m.requestAppHeader()},
			"accept":                        {"application/json"},
			"content-type":                  {"application/json"},
			"x-whatnot-app-user-session-id": {sessionData.sessionID},
			"x-whatnot-app-version":         {m.requestAppVersion()},
			"x-whatnot-app-pathname":        {"/live/" + liveID},
			"x-whatnot-app-screen":          {"/live/?"},
			"x-whatnot-livestream-id":       {liveID},
			"x-client-timezone":             {m.deviceTimezone()},
			"user-agent":                    {m.browserUserAgent()},
			"origin":                        {"https://www.whatnot.com"},
			"sec-fetch-site":                {"same-origin"},
			"sec-fetch-mode":                {"cors"},
			"sec-fetch-dest":                {"empty"},
			"referer":                       {"https://www.whatnot.com/live/" + liveID},
			"accept-encoding":               {"gzip, deflate, br, zstd"},
			"accept-language":               {AcceptLanguage},
			"priority":                      {"u=1, i"},
			http.HeaderOrderKey:             {"x-whatnot-app-session-id", "x-request-id", "authorization", "sec-ch-ua-platform", "sec-ch-ua", "sec-ch-ua-mobile", "x-whatnot-app-context", "x-whatnot-app", "accept", "content-type", "x-whatnot-app-user-session-id", "x-whatnot-app-version", "x-whatnot-app-pathname", "x-whatnot-app-screen", "x-whatnot-livestream-id", "x-client-timezone", "user-agent", "origin", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "referer", "accept-encoding", "accept-language", "cookie", "priority"},
		},
	)

	if err != nil {
		m.log.RefreshError("Failed to refresh tokens: " + err.Error())
		return false, fmt.Errorf("couldn't send refresh session request: %w", err)
	}

	if strings.Contains(strings.ToLower(res.String()), "banned") {
		token := ""
		if len(m.usedSeonSessionTokens) > 0 {
			token = m.usedSeonSessionTokens[0]
		}
		m.log.RefreshError("Account came back banned. " + token)
		return false, nil
	}

	var dto SuccessLoginResponse
	if err := res.JSON(&dto); err == nil && strings.TrimSpace(dto.AccessToken.Token) != "" {
		if err := m.storeSessionCookies(dto); err != nil {
			m.log.RefreshError("Failed to store refreshed tokens: " + err.Error())
			return false, fmt.Errorf("store refreshed tokens: %w", err)
		}
	} else if m.client != nil && m.client.GetCookieJar() != nil {
		if token, tokenErr := m.checkoutAccessToken(); tokenErr == nil {
			m.accessToken = strings.TrimSpace(token)
		}
	}

	m.lastSessionRefreshAt = time.Now()
	m.log.Refresh("Successfully refreshed tokens.")
	return true, nil
}

func (m *Module) RegisterAccountOnly() (bool, error) {
	return m.registerWithCompletion(registrationCompletionAccountOnly)
}

func (m *Module) RegisterSEONTester() (bool, error) {
	return m.registerWithCompletion(registrationCompletionSEONTester)
}

func (m *Module) RegisterPreparedCheckout() (bool, error) {
	return m.registerWithCompletion(registrationCompletionPreparedCheckout)
}

func (m *Module) registerWithCompletion(mode registrationCompletionMode) (bool, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRegistrationAttempts; attempt++ {
		if err := m.rotateProxy(); err != nil {
			lastErr = fmt.Errorf("proxy bootstrap failed: %w", err)
			m.log.ManagerError("Failed to prepare session: " + err.Error())
			continue
		}

		m.log.Kasada("Solving Kasada challenge...")
		created, err := m.registerAttempt(mode)
		if err == nil {
			return true, nil
		}

		lastErr = err

		if errors.Is(err, errEmailAlreadyTaken) {
			changed, resetErr := m.resetAccountIdentityAfterEmailTaken()
			if resetErr != nil {
				return false, fmt.Errorf("email already taken for %s; failed to move to next email: %w", m.account.email, resetErr)
			}
			if changed {
				m.log.Manager("Email was already taken; moved to the next email and continuing.")
				continue
			}
		}

		if created {
			return true, err
		}
	}

	if lastErr == nil {
		lastErr = errors.New("registration failed without an explicit error")
	}

	return false, fmt.Errorf("exhausted %d proxy attempts for %s: %w", maxRegistrationAttempts, m.account.email, lastErr)
}

func (m *Module) registerAttempt(mode registrationCompletionMode) (bool, error) {
	referralCode := m.resolveReferralCode()

	if err := m.reserveEarlySMSNumber(); err != nil {
		m.log.ManagerError("Early SMS reservation failed (non-blocking): " + err.Error())
	}

	if m.lastSMSPhoneNumber == "" {
		return false, fmt.Errorf("no phone number reserved; cannot proceed without pre-registration")
	}
	if err := m.runWebPreRegistration(m.lastSMSPhoneNumber, referralCode); err != nil {
		return false, fmt.Errorf("web pre-registration failed: %w", err)
	}

	waitKasadaGlobalBackoff()
	solved, kasada, sessionData, err := m.kasadaSolver.Solve(context.Background())
	if err != nil {
		recordKasadaSolveResult(false)
		m.log.ManagerError("Kasada solve failed: " + err.Error())
		return false, fmt.Errorf("kasada solve failed: %w", err)
	}
	if !solved {
		recordKasadaSolveResult(false)
		m.log.ManagerError("Kasada challenge was not solved.")
		return false, errKasadaBanned
	}
	recordKasadaSolveResult(true)

	m.kasadaValues = kasada
	m.session = sessionData

	ip, err := getPublicIP(m.client)
	if err != nil {
		return false, fmt.Errorf("failed to resolve proxy IP: %w", err)
	}
	m.ip = ip

	if m.seonDevice == nil {
		m.seonDevice, err = generateSeonDevice(fmt.Sprintf("%s's iPhone", m.account.firstName), m.client.GetProxy())
	}
	if err != nil {
		m.log.ManagerError("Failed to generate fingerprint: " + err.Error())
		return false, fmt.Errorf("fingerprint generation failed: %w", err)
	}

	m.events = NewEventsClient(m.log, m.client, m.seonDevice, m.whatnotIOSAppVersion(), m.whatnotIOSAppBuild())
	if m.events != nil {
		m.events.SendAppLaunch()
		m.events.SendAppForeground()
		m.events.SendPreRegistration()
	}

	m.sleepDelay("after_seon")

	m.log.Kasada("Generating proof of work...")
	pow, err := m.kasadaSolver.generatePow(context.Background())
	if err != nil {
		m.log.ManagerError("Proof of work generation failed: " + err.Error())
		return false, fmt.Errorf("pow generation failed: %w", err)
	}
	m.sleepDelay("after_pow")

	m.log.Seon("Generating fingerprint payload...")
	var behaviourOpts *devices.BehaviourOptions
	if rand.Float64() >= 0.15 {
		durationMs := 2000 + rand.Intn(8000)
		behaviourOpts = &devices.BehaviourOptions{
			Enabled:             true,
			Duration:            time.Duration(durationMs) * time.Millisecond,
			Activity:            behaviour.RandomizeActivity(),
			HeadingActivity:     behaviour.RandomizeHeadingActivity(),
			InitialHeading:      rand.Float64() * 360,
			MagFieldMagnitude:   devices.BehaviourMagField(m.seonDevice.LocalePreset(), m.seonDevice.Machine()),
			EmitAtEar:           false,
			MagneticDeclination: devices.BehaviourMagDeclination(m.seonDevice.LocalePreset()),
		}
	}
	fpCfg := m.seonDevice.NewFingerprint(devices.FingerprintOverrides{
		Behaviour: behaviourOpts,
	})
	sessionToken, err := seonsdk.Fingerprint(*fpCfg)
	if err != nil {
		m.log.ManagerError("Failed to submit fingerprint: " + err.Error())
		return false, fmt.Errorf("fingerprint submittion failed: %w", err)
	}
	m.log.Seon("Fingerprint payload ready.")

	payload, err := marshalOrdered(
		"session", sessionToken,
		"app_type", "whatnot-ios",

		"password", m.account.password,
		"display_name", m.account.firstName+" "+m.account.lastName,
		"email", m.account.email,
	)

	if err != nil {
		return false, err
	}
	payload = bytes.ReplaceAll(payload, []byte("/"), []byte("\\/"))

	m.log.Manager("Submitting registration request with referral code " + referralCode + "...")

	registerHeaders := http.Header{
		"x-system-version":         {m.seonIOSVersion()},
		"x-client-timezone":        {m.deviceTimezone()},
		"user-agent":               {m.whatnotIOSUserAgent(m.seonIOSVersion(), m.deviceMachine())},
		"x-whatnot-app-build":      {m.whatnotIOSAppBuild()},
		"x-whatnot-app":            {"whatnot-ios"},
		"x-kpsdk-h":                {kasada.h},
		"priority":                 {"u=3"},
		"x-system-name":            {"iOS"},
		"x-whatnot-app-version":    {m.whatnotIOSAppVersion()},
		"x-kpsdk-ct":               {kasada.ct},
		"x-kpsdk-cd":               {pow},
		"x-kpsdk-dv":               {kasada.dv},
		"accept-language":          {m.deviceAcceptLanguage()},
		"x-kpsdk-v":                {m.kasadaVersion()},
		"accept":                   {"*/*"},
		"content-type":             {"application/json"},
		"x-whatnot-app-session-id": {m.session.appSessionID},
		"accept-encoding":          {"gzip, deflate, br"},
		http.HeaderOrderKey: {
			"x-system-version", "x-client-timezone", "user-agent", "cookie",
			"x-whatnot-app-build", "x-whatnot-app", "x-kpsdk-h", "priority",
			"x-system-name", "content-length", "x-whatnot-app-version",
			"x-kpsdk-ct", "x-kpsdk-cd", "x-kpsdk-dv",
			"accept-language", "x-kpsdk-v", "accept", "content-type",
			"x-whatnot-app-session-id", "accept-encoding",
		},
	}

	registerHeaders.Set("cookie", m.cookiesExcluding())

	originalJar := m.client.GetCookieJar()
	emptyJar := tls_client.NewCookieJar()

	m.client.SetCookieJar(emptyJar)

	registerReq, reqErr := http.NewRequest("POST", "https://api.whatnot.com/api/v2/register", bytes.NewReader(payload))
	if reqErr != nil {
		m.client.SetCookieJar(originalJar)
		return false, fmt.Errorf("register request creation failed: %w", reqErr)
	}
	registerReq.Header = registerHeaders

	res, err := m.doRequestWithRetry("register account", registerReq)

	for _, c := range emptyJar.Cookies(apiWhatnotURL()) {
		originalJar.SetCookies(apiWhatnotURL(), []*http.Cookie{c})
	}
	m.client.SetCookieJar(originalJar)

	if err != nil {
		m.log.ManagerError("Registration request failed: " + err.Error())
		return false, fmt.Errorf("register request failed: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusBadRequest {
			body := shortResponse(res.String())
			m.log.ManagerError("Registration request failed (bad request): " + body)
			if strings.Contains(strings.ToLower(body), "email has already been taken") {
				return false, errEmailAlreadyTaken
			}
			if referralErr, ok := registerReferralError(referralCode, body); ok {
				m.log.ManagerError("Registration rejected the referral code " + referralCode + ".")
				return false, referralErr
			}
			return false, errKasadaBanned
		}

		m.log.ManagerError(fmt.Sprintf("Registration request failed (%s).", res.Status))
		return false, fmt.Errorf("unexpected register status %s: %s", res.Status, shortResponse(res.String()))
	}

	m.usedSeonSessionTokens = []string{sessionToken}
	m.log.Whatnot("Account Registered!")

	var dto SuccessLoginResponse
	if err := res.JSON(&dto); err != nil {
		m.log.ManagerError("Failed to parse login response: " + err.Error())
		return true, fmt.Errorf("failed to parse login response: %w", err)
	}

	if err := m.storeSessionCookies(dto); err != nil {
		m.log.ManagerError("Failed to store session cookies: " + err.Error())
		return true, err
	}
	m.autosaveSessionIfChanged("register account")

	if m.events != nil {
		m.events.SetAuthenticated(dto.UserID, dto.Appsid)
		m.events.SendPostRegistration()
	}

	m.sleepDelay("after_register")

	if err := m.completeRegisteredAccount(sessionToken, mode); err != nil {
		return true, err
	}

	return true, nil
}

func (m *Module) storeSessionCookies(dto SuccessLoginResponse) error {
	parsedURL, err := url.Parse(fmt.Sprintf("https://%s", WEB_HOST))
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
	if expiration := jwtClaimString(dto.AccessToken.Token, "exp"); expiration != "" {
		cookies = append(cookies, &http.Cookie{Name: "__Secure-access-token-expiration", Value: expiration})
	}
	m.client.GetCookieJar().SetCookies(parsedURL, cookies)
	m.accessToken = strings.TrimSpace(dto.AccessToken.Token)

	return nil
}

func (m *Module) completeRegisteredAccount(sessionToken string, mode registrationCompletionMode) error {
	if err := appendSessionValidation(validSessionsFile, m, sessionToken, "VALID"); err != nil {
		return err
	}

	m.sendPostRegistrationGQL()

	onboardingHandler := m.handleOnboarding
	if mode == registrationCompletionSEONTester {
		onboardingHandler = m.handleSEONTesterOnboarding
	}
	var onboardingErr error
	for attempt := 1; attempt <= maxOnboardingAttempts; attempt++ {
		onboardingErr = onboardingHandler()
		if onboardingErr == nil {
			break
		}
		m.sleepDelay("retry")
	}
	if onboardingErr != nil {
		return onboardingErr
	}

	if mode == registrationCompletionSEONTester {
		m.log.Whatnot("SEON tester onboarding checkpoint reached: " + m.account.email)
		return nil
	}

	if mode == registrationCompletionAccountOnly {
		m.log.Whatnot("Account onboarding and reward checkpoint reached: " + m.account.email)
		return nil
	}

	if mode == registrationCompletionPreparedCheckout {
		if err := m.saveAccountToCsv(sessionToken); err != nil {
			return err
		}
		m.log.Whatnot("Prepared checkout checkpoint reached: " + m.account.email)
		return nil
	}

	if mode == registrationCompletionCheckout {
		if err := m.handlePostOnboarding(); err != nil {
			return err
		}
		if err := m.saveAccountToCsv(sessionToken); err != nil {
			return err
		}
	}

	m.log.Whatnot("Account ready: " + m.account.email)
	return nil
}

func (m *Module) cookiesExcluding(exclude ...string) string {
	if m.client == nil || m.client.GetCookieJar() == nil {
		return ""
	}
	apiURL, _ := url.Parse("https://api.whatnot.com")
	excluded := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		excluded[name] = true
	}
	var parts []string
	for _, c := range m.client.GetCookieJar().Cookies(apiURL) {
		if !excluded[c.Name] {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func apiWhatnotURL() *url.URL {
	u, _ := url.Parse("https://api.whatnot.com")
	return u
}

func shortResponse(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= 1000 {
		return body
	}
	return body[:1000] + "..."
}

// sendPostRegistrationGQL fires the GraphQL calls the iOS app sends
// right after registration, before onboarding. Order matches fresh_har.har:
//
//	bindingToLoggedOutUser → MyUserData → RegisterUserDevice →
//	GetAccountControlsAlarms → InitialMe → GetAccountAlert →
//	SaveConversionData → AcceptTerms → InitializeOnboardingFlow
//
// Failures are logged but never block the flow.
func (m *Module) sendPostRegistrationGQL() {
	m.log.Manager("Sending post-registration GraphQL calls...")

	lib := map[string]any{
		"clientLibrary": m.referralClientLibrary(),
	}

	m.viewID = strings.ToUpper(uuid.NewString()) + "_forYou"
	m.baggageSessionID = uuid.NewString()

	m.gqlBestEffort("bindingToLoggedOutUser", gqlBody[any]{
		OperationName: "bindingToLoggedOutUser",
		Query:         qBindingToLoggedOutUser,
		Variables: map[string]any{
			"input": map[string]any{
				"attributionUuid": strings.ToUpper(uuid.NewString()),
				"iosStableId":     m.postRegIosStableID(),
				"webAppsid":       uuid.NewString(),
				"webStableId":     uuid.NewString(),
			},
		},
		Extensions: lib,
	})

	m.usgmtReady = true

	m.gqlBestEffort("MyUserData", gqlBody[any]{
		OperationName: "MyUserData",
		Query:         qMyUserData,
		Extensions:    lib,
	})

	idfv := ""
	if m.seonDevice != nil {
		idfv = strings.ToUpper(m.seonDevice.DeviceUdid())
	}
	apnsToken := eventsRandomHex(64)
	firebaseToken := fmt.Sprintf("%s:%s", base64LikeToken(22), "APA91b"+base64LikeToken(113))

	m.gqlBestEffort("RegisterUserDevice", gqlBody[any]{
		OperationName: "RegisterUserDevice",
		Query:         qRegisterUserDevice,
		Variables: map[string]any{
			"active":            true,
			"apnsEnvironment":   "PRODUCTION",
			"apnsToken":         apnsToken,
			"appVersion":        m.whatnotIOSAppVersion(),
			"brand":             "Apple",
			"deviceName":        "iPhone",
			"deviceUniqueId":    idfv,
			"firebasePushToken": firebaseToken,
			"manufacturer":      "Apple",
			"notifAuthStatus":   "AUTHORIZED",
			"systemVersion":     m.seonIOSVersion(),
		},
		Extensions: lib,
	})

	m.gqlBestEffort("GetAccountControlsAlarms", gqlBody[any]{
		OperationName: "GetAccountControlsAlarms",
		Query:         qGetAccountControlsAlarms,
		Extensions:    lib,
	})

	m.gqlBestEffort("InitialMe", gqlBody[any]{
		OperationName: "InitialMe",
		Query:         qInitialMe,
		Extensions:    lib,
	})

	m.gqlBestEffort("GetAccountAlert", gqlBody[any]{
		OperationName: "GetAccountAlert",
		Query:         qGetAccountAlert,
		Extensions:    lib,
	})

	m.gqlBestEffort("SaveConversionData", gqlBody[any]{
		OperationName: "SaveConversionData",
		Query:         qSaveConversionData,
		Variables: map[string]any{
			"input": map[string]any{
				"afStatus":      "Organic",
				"isFirstLaunch": true,
			},
		},
		Extensions: lib,
	})

	m.gqlBestEffort("AcceptTerms", gqlBody[any]{
		OperationName: "AcceptTerms",
		Query:         qAcceptTerms,
		Variables: map[string]string{
			"legalEntity": "WHATNOT_EUROPE_LTD",
		},
		Extensions: lib,
	})

	m.gqlBestEffort("InitializeOnboardingFlow", gqlBody[any]{
		OperationName: "InitializeOnboardingFlow",
		Query:         qInitializeOnboardingFlow,
		Extensions:    lib,
	})
}

func (m *Module) gqlBestEffort(name string, body gqlBody[any]) {
	_, err := m.sendGqlRequest(name, body)
	if err != nil {
		m.log.Manager("Post-registration " + name + " failed (non-blocking): " + err.Error())
	}
}

func (m *Module) postRegIosStableID() string {
	if m.events != nil {
		return m.events.IosStableID()
	}
	return strings.ToUpper(uuid.NewString())
}

func (m *Module) seonIOSVersion() string {
	if m.seonDevice != nil {
		return m.seonDevice.IosVersion()
	}
	return "26.1"
}

func (m *Module) deviceMachine() string {
	if m.seonDevice != nil {
		return m.seonDevice.Machine()
	}
	return "iPhone16,2"
}

func (m *Module) deviceTimezone() string {
	if m.seonDevice != nil {
		return m.seonDevice.Locale().TimezoneIdentifier
	}
	return "Europe/London"
}

func (m *Module) deviceAcceptLanguage() string {
	if m.seonDevice != nil {
		loc := m.seonDevice.Locale()
		return loc.RegionLanguage + "-" + loc.RegionCountry
	}
	return "en-GB"
}

func base64LikeToken(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
