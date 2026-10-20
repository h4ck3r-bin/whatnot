package whtnt

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/Hyper-Solutions/hyper-sdk-go/v2"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/brianvoe/gofakeit/v7"
	"status403.com/rsavv/seonsdk-go"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/logger"
	"status403.com/rsavv/seonsdk-go/devices"
)

type account struct {
	firstName string
	lastName  string
	username  string
	email     string
	password  string
}

type Module struct {
	config   *appconfig.Config
	platform DevicePlatform

	client tls_client.HttpClient
	sdk    *hyper.Session

	ip string

	kasadaValues SolvedKasada

	account     account
	emailLocked bool
	proxy       string

	session sessionCreateResult

	seonDevice            *devices.Device
	usedSeonSessionTokens []string
	lastProxy             string

	referralAmountSafe        int
	rewardBalanceAmountSafe   int
	rewardBalanceCurrency     string
	rewardCreditCheckError    string
	referralCode              string
	lastAddress               AddressFields
	lastCard                  CardDetails
	lastPaymentID             string
	lastPaymentLast4          string
	shippingAddressCreated    bool
	overrideCard              *CardDetails
	lastOrderID               string
	lastOrderIDs              []string
	lastOrderProducts         []Product
	priceUpdates              []PriceUpdate
	productIssues             []ProductIssue
	checkoutAttempted         bool
	lastSessionFingerprint    string
	autosaveMu                sync.Mutex
	autosavePending           []<-chan struct{}
	accessToken               string
	lastSMSProvider           string
	lastSMSActivationID       string
	lastSMSPhoneNumber        string
	lastSMSCode               string
	lastSessionRefreshAt      time.Time
	sessionRefreshMu          sync.Mutex
	refreshingSession         bool
	fastProxyAuthRetry        bool
	skipDelays                bool
	forceCheckout             bool
	allowInsufficientBalance  bool
	reviewPreflightUsername   string
	reviewPreflightFollowed   int
	usernameAdequate          bool
	usernameAdequateCheckedAt time.Time
	completedRequests         map[string]bool
	rewardDetectedHook        func(AccountSnapshot, SessionSnapshot) error
	checkoutOrderHook         func(CheckoutSnapshot)

	kasadaSolver *KasadaSolver

	viewID           string
	baggageSessionID string
	usgmtReady       bool

	events *EventsClient
	log    *logger.Logger
}

// Reference device, pinned to the freshwhatnot.har capture
// (seonsdk-go/testdata/freshwhatnot). Holding the hardware and OS fixed keeps
// the number of moving parts down while payload fidelity is being validated:
// any divergence from the reference payloads is then attributable to our
// generation logic rather than to a different device having been rolled.
//
// Trade-off to revisit before running at scale: a fleet that all reports one
// machine + one iOS version is itself a population tell.
// devices/ios_population.go documents SEON tightening its iOS-version risk
// score on 2026-04-30, with the banned fingerprints in that run being the ones
// on non-latest patches. devices.RandomMachineAndRealisticIOS("26.1") is the
// population-weighted alternative when that matters more than reproducibility;
// note it returns BOTH a machine and a version, and the version must be used
// rather than discarded.
const (
	referenceSeonMachine    = "iPhone18,1" // iPhone 17 Pro
	referenceSeonIOSVersion = "26.1"       // matches x-system-version in HAR
)

func generateSeonDevice(deviceName string, proxy string) (*devices.Device, error) {
	return BuildDevice(DeviceConfig{
		Machine:    referenceSeonMachine,
		IOSVersion: referenceSeonIOSVersion,
		DeviceName: deviceName,
		Proxy:      proxy,
	})
}

// DeviceConfig tweaks the SEON device build without copying every knob.
type DeviceConfig struct {
	// DeviceName is the user-visible iPhone name (e.g. "Anna's iPhone").
	DeviceName string
	// Proxy passed through to the SEON device pipeline.
	Proxy string
	// IOSVersion override. Empty picks a realistic version (paired with Machine) via devices.RandomMachineAndRealisticIOS.
	IOSVersion string
	// Machine override (e.g. "iPhone15,2"). Empty picks a machine paired with IOSVersion.
	Machine string
	// DCMode override. Possible values (synthetic|real|timeout). Default real.
	DCMode string
	// DCAcceptLanguage override. Default "en-GB,en;q=0.9"
	DCAcceptLanguage string
	// Locale override. Default is a UK config
	Locale *seonsdk.LocaleConfig
	// TrackingGranted override. Default is false.
	TrackingGranted bool
}

func BuildDevice(cfg DeviceConfig) (*devices.Device, error) {
	machine := cfg.Machine
	iosVersion := cfg.IOSVersion
	if machine == "" || iosVersion == "" {
		m, v := devices.RandomMachineAndRealisticIOS("26.1")
		if machine == "" {
			machine = m
		}
		if iosVersion == "" {
			iosVersion = v
		}
	}

	mode := strings.ToLower(cfg.DCMode)
	if mode == "" {
		mode = "real"
	}

	skipDC := true
	syntheticSuccess := false
	simulateTimeout := false
	dcDeviceIP := ""
	switch mode {
	case "real":
		skipDC = false
	case "timeout":
		simulateTimeout = true
	case "synthetic":
		syntheticSuccess = true
		// We don't know the egress IP without making a probe; supply the
		// proxy host as a best-effort default. The DC server in real
		// captures echoes back its own observed source IP, which is the
		// proxy's egress, so this only needs to be a routable IPv4.
		dcDeviceIP = extractProxyHost(cfg.Proxy)
	default:
		return nil, fmt.Errorf("unknown DC mode %q (want synthetic|real|timeout)", mode)
	}

	dcAcceptLanguage := "en-GB,en;q=0.9"
	if cfg.DCAcceptLanguage != "" {
		dcAcceptLanguage = cfg.DCAcceptLanguage
	}
	locale := &seonsdk.LocaleConfig{
		RegionTimezone:     "+01:00",
		RegionLanguage:     "en",
		RegionCountry:      "GB",
		TimezoneIdentifier: "Europe/London",
		CalendarIdentifier: "gregorian",
		SpeakerName:        "Speaker",
	}
	if cfg.Locale != nil {
		locale = cfg.Locale
	}

	return devices.NewRealisticDevice(devices.DeviceOptions{
		Machine:            machine,
		IOSVersion:         iosVersion,
		UserDeviceName:     cfg.DeviceName,
		TrackingGranted:    cfg.TrackingGranted,
		Locale:             locale,
		Proxy:              cfg.Proxy,
		SkipDC:             skipDC,
		SimulateDCTimeout:  simulateTimeout,
		DCSyntheticSuccess: syntheticSuccess,
		DCDeviceIP:         dcDeviceIP,
		DCAcceptLanguage:   dcAcceptLanguage,
	})
}

func extractProxyHost(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return ""
	}
	// Strip scheme.
	if i := strings.Index(proxy, "://"); i >= 0 {
		proxy = proxy[i+3:]
	}
	// Strip user:pass@.
	if i := strings.LastIndex(proxy, "@"); i >= 0 {
		proxy = proxy[i+1:]
	}
	// Strip :port.
	if i := strings.Index(proxy, ":"); i >= 0 {
		proxy = proxy[:i]
	}
	return proxy
}

func NewModule() (*Module, error) {
	return newModule("", "")
}

func NewModuleWithCredentials(email string, password string) (*Module, error) {
	return newModule(email, password)
}

func newModule(emailOverride string, passwordOverride string) (*Module, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return nil, err
	}
	firstName := gofakeit.FirstName()
	lastName := gofakeit.LastName()
	email := strings.ToLower(strings.TrimSpace(emailOverride))
	if email == "" {
		if cfg.RandomEmail {
			email = randomGmail(firstName, lastName)
		} else {
			email, err = popEmailFromEmailsTXT()
			if err != nil {
				return nil, err
			}
		}
	}
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("invalid email %q", email)
	}
	password := strings.TrimSpace(passwordOverride)
	if password == "" {
		password = gofakeit.Password(true, true, true, true, false, 10)
	}

	return &Module{
		config:   cfg,
		platform: currentPlatform(),
		sdk:      hyper.NewSession(cfg.HyperKey),
		account: account{
			firstName: firstName,
			lastName:  lastName,
			email:     email,
			password:  password,
		},
		emailLocked: strings.TrimSpace(emailOverride) != "",
		log:         logger.New(logger.NextID()),
	}, nil
}

func randomGmail(firstName string, lastName string) string {
	localPart := normalizeEmailPart(firstName) + normalizeEmailPart(lastName)
	if localPart == "" {
		localPart = "user"
	}
	return fmt.Sprintf("%s%s@gmail.com", localPart, randomNumericSuffix(12))
}

func randomNumericSuffix(width int) string {
	if width < 4 {
		width = 4
	}
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(width)), nil)
	value, err := crand.Int(crand.Reader, limit)
	if err != nil {
		return fmt.Sprintf("%0*d", width, rand.Int63n(1_000_000_000_000))
	}
	return fmt.Sprintf("%0*s", width, value.String())
}

func normalizeEmailPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if char >= 'a' && char <= 'z' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func (m *Module) resetAccountIdentityAfterEmailTaken() (bool, error) {
	if m == nil || m.config == nil || m.emailLocked {
		return false, nil
	}

	firstName := gofakeit.FirstName()
	lastName := gofakeit.LastName()
	email := ""
	var err error
	if m.config.RandomEmail {
		email = randomGmail(firstName, lastName)
	} else {
		email, err = popEmailFromEmailsTXT()
		if err != nil {
			return false, err
		}
	}

	m.account = account{
		firstName: firstName,
		lastName:  lastName,
		email:     email,
		password:  gofakeit.Password(true, true, true, true, false, 10),
	}
	m.resetAttemptState()
	return true, nil
}

func (m *Module) Close() {
	m.detachSessionAutosaves()
	if m.kasadaSolver != nil {
		m.kasadaSolver.Close()
		m.kasadaSolver = nil
	}

	if m.client != nil {
		m.client.CloseIdleConnections()
		m.client = nil
	}
}

func (m *Module) resetAttemptState() {
	m.ip = ""
	m.proxy = ""
	m.session = sessionCreateResult{}
	m.usedSeonSessionTokens = nil
	m.kasadaValues = SolvedKasada{}
	m.events = nil
}

func (m *Module) buildKasadaConfig(session tls_client.HttpClient) *Config {
	cfg := &Config{
		Session:        session,
		APIKey:         m.config.HyperKey,
		HyperDebug:     m.config.HyperDebug,
		HyperProxy:     m.config.HyperProxy,
		AcceptLanguage: AcceptLanguage,
		ProxyURL:       session.GetProxy(),
	}

	if m.platform == PlatformIOS {
		cfg.Mobile = true
		if m.seonDevice != nil {
			cfg.MobileUserAgent = m.whatnotIOSUserAgent(m.seonDevice.IosVersion(), m.seonDevice.Machine())
		} else {
			cfg.MobileUserAgent = m.whatnotIOSUserAgent(m.seonIOSVersion(), m.deviceMachine())
		}
	} else {
		cfg.PageURL = fmt.Sprintf("https://%s/register?redirect=/login?source=logout&source=header", WEB_HOST)
		cfg.BrowserUserAgent = m.browserUserAgent()
		cfg.BrowserSecChUa = m.browserSecChUa()
		cfg.BrowserSecChUaPlatform = m.browserSecChUaPlatform()
		cfg.KasadaVersion = m.kasadaVersion()
	}

	return cfg
}

func (m *Module) rotateProxy() error {
	m.Close()
	m.resetAttemptState()

	proxy := chooseProxy(m.lastProxy)
	if m.config != nil && m.config.HyperDebug {
		proxy = hyperDebugProxy
	}
	session, err := createHTTPClient(m.platform, proxy)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}

	if m.platform == PlatformIOS {
		var deviceErr error
		m.seonDevice, deviceErr = generateSeonDevice(fmt.Sprintf("%s's iPhone", m.account.firstName), proxy)
		if deviceErr != nil {
			session.CloseIdleConnections()
			return fmt.Errorf("failed to generate SEON device: %w", deviceErr)
		}
	}

	solver, err := NewKasadaSolver(context.Background(), m.buildKasadaConfig(session))
	if err != nil {
		session.CloseIdleConnections()
		return err
	}

	m.client = session
	m.kasadaSolver = solver

	// Obtain cf_clearance before first API use so CF edge doesn't flag the session.
	m.cfSolveOrSkip(context.Background())
	m.proxy = proxy
	m.lastProxy = proxy

	return nil
}

func (m *Module) rotateHTTPClientProxy(reason string) error {
	if m == nil || m.config == nil {
		return fmt.Errorf("module config is not available")
	}
	var cookies []CookieJSON
	if m.client != nil && m.client.GetCookieJar() != nil {
		cookies, _ = m.snapshotCookies()
	}

	proxy := chooseProxy(m.lastProxy)
	if m.config.HyperDebug {
		proxy = hyperDebugProxy
	}
	session, err := createHTTPClient(m.platform, proxy)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	solver, err := NewKasadaSolver(context.Background(), m.buildKasadaConfig(session))
	if err != nil {
		session.CloseIdleConnections()
		return err
	}
	oldClient := m.client
	oldSolver := m.kasadaSolver
	m.client = session
	m.kasadaSolver = solver
	m.proxy = proxy
	m.lastProxy = proxy
	for _, cookie := range cookies {
		_ = m.setCookieJSON(cookie)
	}
	if oldSolver != nil {
		oldSolver.Close()
	}
	if oldClient != nil {
		oldClient.CloseIdleConnections()
	}
	if m.log != nil {
		m.log.ManagerNotice(fmt.Sprintf("Rotated proxy for %s: %s", firstNonEmptyString(reason, "request retry"), maskProxy(proxy)))
	}
	return nil
}

func (m *Module) rebuildHTTPClientForRetry(reason string, cause error) error {
	if isClosedSessionError(cause) {
		return m.rebuildHTTPClientSameProxy(reason)
	}
	return m.rotateHTTPClientProxy(reason)
}

func (m *Module) ensureHTTPClient(reason string) error {
	if m == nil || m.config == nil {
		return fmt.Errorf("module config is not available")
	}
	if m.client != nil && m.client.GetCookieJar() != nil {
		return nil
	}
	return m.rebuildHTTPClientSameProxy(reason)
}

func (m *Module) rebuildHTTPClientSameProxy(reason string) error {
	if m == nil || m.config == nil {
		return fmt.Errorf("module config is not available")
	}
	var cookies []CookieJSON
	oldProxy := strings.TrimSpace(m.proxy)
	if oldProxy == "" && m.client != nil {
		oldProxy = strings.TrimSpace(m.client.GetProxy())
	}
	if m.client != nil && m.client.GetCookieJar() != nil {
		cookies, _ = m.snapshotCookies()
	}

	proxy := oldProxy
	if proxy == "" {
		proxy = chooseProxy(m.lastProxy)
	}
	if m.config.HyperDebug {
		proxy = hyperDebugProxy
	}

	session, err := createHTTPClient(m.platform, proxy)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	solver, err := NewKasadaSolver(context.Background(), m.buildKasadaConfig(session))
	if err != nil {
		session.CloseIdleConnections()
		return err
	}
	oldClient := m.client
	oldSolver := m.kasadaSolver
	m.client = session
	m.kasadaSolver = solver
	m.proxy = proxy
	m.lastProxy = proxy
	for _, cookie := range cookies {
		_ = m.setCookieJSON(cookie)
	}
	if oldSolver != nil {
		oldSolver.Close()
	}
	if oldClient != nil {
		oldClient.CloseIdleConnections()
	}
	if m.log != nil {
		m.log.ManagerNotice(fmt.Sprintf("Rebuilt HTTP client for %s: %s", firstNonEmptyString(reason, "request retry"), maskProxy(proxy)))
	}
	return nil
}

func chooseProxy(previous string) string {
	proxyList := currentProxies()
	if len(proxyList) == 0 {
		return ""
	}

	if len(proxyList) == 1 {
		return normalizeProxyURL(strings.TrimSpace(proxyList[0]))
	}

	for i := 0; i < 8; i++ {
		candidate := normalizeProxyURL(strings.TrimSpace(proxyList[rand.Intn(len(proxyList))]))
		if candidate != "" && candidate != previous {
			return candidate
		}
	}

	return normalizeProxyURL(strings.TrimSpace(proxyList[rand.Intn(len(proxyList))]))
}

func normalizeProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "socks") {
		return raw
	}
	parts := strings.Split(raw, ":")
	switch len(parts) {
	case 2:
		return "http://" + parts[0] + ":" + parts[1]
	case 4:
		return "http://" + parts[2] + ":" + parts[3] + "@" + parts[0] + ":" + parts[1]
	default:
		return "http://" + raw
	}
}

func maskProxy(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return "direct"
	}

	if len(proxy) <= 18 {
		return proxy
	}

	return proxy[:9] + "..." + proxy[len(proxy)-6:]
}
