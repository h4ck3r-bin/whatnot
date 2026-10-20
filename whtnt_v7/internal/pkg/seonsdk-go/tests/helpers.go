package whatnottest

// MIRRORS internal/internal/whtnt/{whtnt.go,kasada_new.go,emails.go,onboarding.go}
// This file is a deliberate parallel mini-flow used only by whatnot_test.go.
// Keep it narrowly scoped — do not turn it into a second whtnt package.

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

// ---------------------------------------------------------------------------
// Hardcoded credentials sourced from internal/setup/config.json.
// Update here if the config file is rotated.
// ---------------------------------------------------------------------------

const HyperKey = "cdc1b824-e9f0-40f8-8453-241cf0c24431"

// ---------------------------------------------------------------------------
// Browser fingerprint constants — mirror internal/internal/whtnt/kasada_new.go.
// ---------------------------------------------------------------------------

const (
	WebHost           = "www.whatnot.com"
	UserAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
	SecChUa           = `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`
	SecChUaPlatform   = `"Windows"`
	AcceptLanguage    = "en-GB,en;q=0.9 "
	KasadaVersion     = "j-1.2.386"
	KasadaBasePath    = "/149e9513-01fa-4fb0-aad4-566afd725d1b/2d206a39-8ed7-437e-a3be-862e0f06eea3"
	WhatnotAppVersion = "20260410-0208"

	// IDs and copy that the live flow expects.
	pokemonCategoryID  = "TGl2ZXN0cmVhbVRhZ05vZGU6OTA3"
	defaultLegalEntity = "WHATNOT_EUROPE_LTD"
	defaultCountry     = "GB"
)

// ---------------------------------------------------------------------------
// Email file pop — mirrors internal/internal/whtnt/emails.go.
// ---------------------------------------------------------------------------

var emailLocks sync.Map

// PopEmail removes and returns the LAST non-blank, non-comment line from path.
// Popping from the tail means newly-appended emails are consumed first and
// the file shrinks from the bottom. The file is rewritten with that line
// removed so subsequent runs use new emails.
func PopEmail(path string) (string, error) {
	cleaned := filepath.Clean(path)
	lockValue, _ := emailLocks.LoadOrStore(strings.ToLower(cleaned), &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	input, err := os.Open(cleaned)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", cleaned, err)
	}

	var allLines []string
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		_ = input.Close()
		return "", fmt.Errorf("read %s: %w", cleaned, scanErr)
	}
	if closeErr := input.Close(); closeErr != nil {
		return "", fmt.Errorf("close %s: %w", cleaned, closeErr)
	}

	// Walk from the end, pluck the last usable line.
	pickIndex := -1
	var selected string
	for i := len(allLines) - 1; i >= 0; i-- {
		value := strings.TrimSpace(allLines[i])
		value = strings.TrimPrefix(value, "\ufeff")
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		selected = value
		pickIndex = i
		break
	}
	if pickIndex == -1 {
		return "", fmt.Errorf("%s has no unused emails left", cleaned)
	}
	if !strings.Contains(selected, "@") {
		return "", fmt.Errorf("%s yielded a non-email line: %q", cleaned, selected)
	}

	remaining := append(allLines[:pickIndex:pickIndex], allLines[pickIndex+1:]...)

	dir := filepath.Dir(cleaned)
	tempFile, err := os.CreateTemp(dir, "tmp-emails-*.txt")
	if err != nil {
		return "", fmt.Errorf("temp file in %s: %w", dir, err)
	}
	tempName := tempFile.Name()

	writer := bufio.NewWriter(tempFile)
	for _, line := range remaining {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempName)
			return "", fmt.Errorf("write temp: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
		return "", fmt.Errorf("flush temp: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempName)
		return "", fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tempName, cleaned); err != nil {
		_ = os.Remove(tempName)
		return "", fmt.Errorf("replace %s: %w", cleaned, err)
	}

	return strings.ToLower(strings.TrimSpace(selected)), nil
}

// ---------------------------------------------------------------------------
// Proxy file reader — read-only, picks a random line, never pops.
// ---------------------------------------------------------------------------

// PickProxy reads path, picks a random non-empty/non-comment line, and returns
// it normalized to `http://user:pass@host:port` if it looks like the
// `host:port:user:pass` shape used in internal/setup/proxies.txt. Existing
// schemed URLs (http://, https://, socks5://) are returned untouched. The
// file is never modified.
func PickProxy(path string) (string, error) {
	cleaned := filepath.Clean(path)
	file, err := os.Open(cleaned)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", cleaned, err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", cleaned, err)
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("%s contains no proxies", cleaned)
	}

	pick := lines[rand.Intn(len(lines))]
	return normalizeProxy(pick), nil
}

func normalizeProxy(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "socks5://") ||
		strings.HasPrefix(lower, "socks4://") {
		return raw
	}
	parts := strings.Split(raw, ":")
	if len(parts) == 4 {
		host, port, user, pass := parts[0], parts[1], parts[2], parts[3]
		return fmt.Sprintf("http://%s:%s@%s:%s", user, pass, host, port)
	}
	if len(parts) == 2 {
		return fmt.Sprintf("http://%s", raw)
	}
	return raw
}

// ---------------------------------------------------------------------------
// SEON device + fingerprint token — mirrors internal/internal/whtnt/whtnt.go.
// ---------------------------------------------------------------------------

// DeviceConfig lets the test caller tweak the device build without copying every knob.
type DeviceConfig struct {
	// DeviceName is the user-visible iPhone name (e.g. "Anna's iPhone").
	DeviceName string
	// Proxy passed through to the SEON device pipeline.
	Proxy string
	// IOSVersion override. Empty falls back to a realistic 18.5 pick.
	IOSVersion string
	// Machine override (e.g. "iPhone15,2"). Empty picks a machine paired with IOSVersion.
	Machine string
}

// BuildDevice constructs a UK iOS SEON device matching whtnt.buildSeonDevice.
//
// DC behaviour is controlled by the SEON_DC_MODE env var:
//
//	"synthetic" (default) — fake a successful DC roundtrip locally; payload
//	                        looks like the dominant pattern in the legit
//	                        iPhone 15 / iOS 26.5 corpus.
//	"real"                — actually call seondnsresolve.com via the proxy;
//	                        slow but produces a genuine DC server response.
//	"timeout"             — legacy SimulateDCTimeout mode; payload looks like
//	                        a DC-unreachable iPhone (timeout-shaped). Kept for
//	                        debugging the older codepath; do not use in
//	                        production runs.
func BuildDevice(cfg DeviceConfig) (*devices.Device, error) {
	machine := cfg.Machine
	iosVersion := cfg.IOSVersion
	if machine == "" || iosVersion == "" {
		m, v := devices.RandomMachineAndRealisticIOS("18.5")
		if machine == "" {
			machine = m
		}
		if iosVersion == "" {
			iosVersion = v
		}
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SEON_DC_MODE")))
	if mode == "" {
		mode = "synthetic"
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
		return nil, fmt.Errorf("unknown SEON_DC_MODE %q (want synthetic|real|timeout)", mode)
	}

	return devices.NewRealisticDevice(devices.DeviceOptions{
		Machine:            machine,
		IOSVersion:         iosVersion,
		UserDeviceName:     cfg.DeviceName,
		TrackingGranted:    false,
		Locale: &seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "en",
			RegionCountry:      "GB",
			TimezoneIdentifier: "Europe/London",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		Proxy:              cfg.Proxy,
		SkipDC:             skipDC,
		SimulateDCTimeout:  simulateTimeout,
		DCSyntheticSuccess: syntheticSuccess,
		DCDeviceIP:         dcDeviceIP,
		DCAcceptLanguage:   "en-GB,en;q=0.9",
	})
}

// extractProxyHost returns the host portion of a proxy URL like
// "http://user:pass@host:port" or "host:port". Returns empty string on
// parse failure so callers can fall back to "" (which the synthetic DC
// builder will simply emit as device_ip:"").
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

// FingerprintToken runs the SEON pipeline against device with whtnt-style
// behaviour. Behaviour-results emission rate, window duration, and heading
// activity all match the iPhone 15 / iOS 26.5 corpus distribution: 15% of
// captures emit no behaviour_results at all, windows are 2–10 s, and the
// heading-update scenario is drawn from the population mix (70% Still / 25%
// Active / 5% OnTable).
func FingerprintToken(device *devices.Device) (string, error) {
	if device == nil {
		return "", errors.New("device is nil")
	}

	var behaviourOpts *devices.BehaviourOptions
	if rand.Float64() >= 0.15 {
		durationMs := 2000 + rand.Intn(8000)
		behaviourOpts = &devices.BehaviourOptions{
			Enabled:             true,
			Duration:            time.Duration(durationMs) * time.Millisecond,
			Activity:            behaviour.RandomizeActivity(),
			HeadingActivity:     behaviour.RandomizeHeadingActivity(),
			InitialHeading:      rand.Float64() * 360,
			MagFieldMagnitude:   devices.BehaviourMagField(device.LocalePreset(), device.Machine()),
			EmitAtEar:           false,
			MagneticDeclination: devices.BehaviourMagDeclination(device.LocalePreset()),
		}
	}
	cfg := device.NewFingerprint(devices.FingerprintOverrides{
		Behaviour: behaviourOpts,
	})
	return seonsdk.Fingerprint(*cfg)
}

// ---------------------------------------------------------------------------
// Results log.
// ---------------------------------------------------------------------------

// AppendResult writes a single CSV line:
//
//	timestamp,email,status,seon_token,access_token,refresh_token
//
// `seon_token` is the SEON fingerprint submitted to /register; the access and
// refresh tokens are the Whatnot session tokens returned by /register (empty
// when register failed). The file is created with a header on first write.
// Empty path is a no-op.
func AppendResult(path, email, status, seonToken, accessToken, refreshToken string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	cleaned := filepath.Clean(path)
	if dir := filepath.Dir(cleaned); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	needsHeader := false
	if info, err := os.Stat(cleaned); err != nil || info.Size() == 0 {
		needsHeader = true
	}

	file, err := os.OpenFile(cleaned, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if needsHeader {
		if _, err := file.WriteString("timestamp,email,status,seon_token,access_token,refresh_token\n"); err != nil {
			return err
		}
	}
	line := fmt.Sprintf("%s,%s,%s,%s,%s,%s\n",
		time.Now().UTC().Format(time.RFC3339),
		csvEscape(email),
		csvEscape(status),
		csvEscape(seonToken),
		csvEscape(accessToken),
		csvEscape(refreshToken),
	)
	_, err = file.WriteString(line)
	return err
}

func csvEscape(value string) string {
	if !strings.ContainsAny(value, ",\"\n") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func shortBody(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= 600 {
		return body
	}
	return body[:600] + "..."
}
