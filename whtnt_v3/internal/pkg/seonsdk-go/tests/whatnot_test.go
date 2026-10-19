package whatnottest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

// TestSeonFingerprintBan drives one Whatnot account creation per `go test` invocation,
// runs the onboarding flow up to (but not including) phone verification, waits, and
// re-checks the session. A "banned" response on the second refresh is treated as
// proof that the SEON fingerprint was rejected by the Whatnot/SEON pipeline.
//
// HYPER_KEY is hardcoded (see helpers.go) from internal/setup/config.json.
// Proxies are read (without popping) from internal/setup/proxies.txt.
//
// Optional environment:
//
//	EMAILS_FILE   Path to emails.txt (default: ../internal/setup/emails.txt)
//	PROXIES_FILE  Path to proxies.txt (default: ../internal/setup/proxies.txt)
//	RESULTS_FILE  CSV log of results (default: results.csv inside the tests dir)
//	WAIT_SECONDS  Seconds to wait between initial refresh and ban-check refresh (default: 60)
//	APP_TYPE      "whatnot-android" or "whatnot-ios" (default: whatnot-android)
func TestSeonFingerprintBan(t *testing.T) {
	emailsPath := envOrDefault("EMAILS_FILE", filepath.Join("..", "internal", "setup", "emails.txt"))
	proxiesPath := envOrDefault("PROXIES_FILE", filepath.Join("..", "internal", "setup", "proxies.txt"))
	resultsPath := envOrDefault("RESULTS_FILE", "results.csv")
	waitSeconds := envIntOrDefault("WAIT_SECONDS", 60)
	appType := envOrDefault("APP_TYPE", "whatnot-android")

	proxy, err := PickProxy(proxiesPath)
	if err != nil {
		t.Fatalf("pick proxy from %s: %v", proxiesPath, err)
	}
	t.Logf("using proxy: %s", maskProxy(proxy))

	email, err := PopEmail(emailsPath)
	if err != nil {
		t.Fatalf("pop email from %s: %v", emailsPath, err)
	}
	t.Logf("popped email: %s", email)

	firstName := gofakeit.FirstName()
	deviceName := fmt.Sprintf("%s's iPhone", firstName)
	t.Logf("device name: %s", deviceName)

	device, err := BuildDevice(DeviceConfig{
		DeviceName: deviceName,
		Proxy:      proxy,
	})
	if err != nil {
		t.Fatalf("build SEON device: %v", err)
	}

	token, err := FingerprintToken(device)
	if err != nil {
		t.Fatalf("generate SEON fingerprint token: %v", err)
	}
	t.Logf("seon token (truncated): %s...", truncate(token, 32))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, runErr := Run(ctx, FlowConfig{
		HyperKey:  HyperKey,
		Proxy:     proxy,
		Email:     email,
		SeonToken: token,
		FirstName: firstName,
		AppType:   appType,
	}, time.Duration(waitSeconds)*time.Second)

	status := "VALID"
	switch {
	case runErr != nil:
		status = "ERROR"
	case result.BannedAtInitial:
		status = "INVALID_INITIAL"
	case result.Banned:
		status = "INVALID"
	}

	if logErr := AppendResult(resultsPath, email, status, token, result.AccessToken, result.RefreshToken); logErr != nil {
		t.Logf("warning: append results: %v", logErr)
	}

	if runErr != nil {
		t.Fatalf("flow failed (status=%s, register=%d): %v\ninitial refresh: %s\npost-wait refresh: %s",
			status, result.RegisterStatus, runErr,
			result.RefreshBefore, result.RefreshAfter)
	}

	if result.BannedAtInitial {
		t.Fatalf("fingerprint rejected at initial refresh; account banned immediately\nbody: %s", result.RefreshBefore)
	}
	if result.Banned {
		t.Fatalf("fingerprint INVALID — account banned after %ds wait\nbody: %s", waitSeconds, result.RefreshAfter)
	}

	t.Logf("fingerprint VALID — account survived %ds wait", waitSeconds)
}

// ---------------------------------------------------------------------------
// env helpers
// ---------------------------------------------------------------------------

func envOrDefault(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(name string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// maskProxy hides the credentials portion of a proxy URL so they don't leak
// into test logs. Anything between the scheme and the @host:port boundary
// gets replaced with `***`.
func maskProxy(proxy string) string {
	if proxy == "" {
		return "direct"
	}
	at := strings.LastIndex(proxy, "@")
	if at < 0 {
		return proxy
	}
	scheme := strings.Index(proxy, "://")
	if scheme < 0 {
		return "***@" + proxy[at+1:]
	}
	return proxy[:scheme+3] + "***@" + proxy[at+1:]
}
