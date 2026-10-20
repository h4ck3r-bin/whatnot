package devices_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
)

// TestPickRealisticHashProvider_PopulationWeights confirms the picker honours
// the default weights for Whatnot's demographic (~95% chp=1, ~4% chp=0,
// ~1% chp=3, ~0% chp=2). chp=3 is held to ≤2% because empirically it
// triggered bans against SEON's risk model on 2026-04-30/05-01.
func TestPickRealisticHashProvider_PopulationWeights(t *testing.T) {
	rand.Seed(42)
	const trials = 50000
	counts := map[int]int{}
	for i := 0; i < trials; i++ {
		counts[devices.PickRealisticHashProvider()]++
	}
	checks := []struct {
		provider int
		expect   float64
		tol      float64
	}{
		{1, 0.95, 0.02},
		{0, 0.04, 0.015},
		{3, 0.01, 0.01},
	}
	for _, c := range checks {
		got := float64(counts[c.provider]) / float64(trials)
		if got < c.expect-c.tol || got > c.expect+c.tol {
			t.Errorf("chp=%d share %.3f outside [%.3f ± %.3f] — full counts %v",
				c.provider, got, c.expect, c.tol, counts)
		}
	}
	// chp=2 should be entirely absent at the new weights — we set its weight to 0.
	if counts[2] != 0 {
		t.Errorf("chp=2 emitted %d times despite weight=0 — picker should never return 2", counts[2])
	}
	// chp=4 must NEVER come out of the picker.
	if counts[4] != 0 {
		t.Errorf("chp=4 emitted %d times — picker must never return 4", counts[4])
	}
}

// TestHashProviderWeights_FallbackOnZero — degenerate weights default to chp=1.
func TestHashProviderWeights_FallbackOnZero(t *testing.T) {
	w := devices.HashProviderWeights{}
	for i := 0; i < 100; i++ {
		if got := w.Pick(); got != 1 {
			t.Fatalf("zero weights should fall back to chp=1, got %d", got)
		}
	}
}

// TestApplyHashProvider_MapsCorrectly verifies that ApplyHashProvider mutates
// PayloadOptions so a downstream BuildPayload would compute the desired chp.
func TestApplyHashProvider_MapsCorrectly(t *testing.T) {
	cases := []struct {
		provider           int
		wantICloudSignedIn bool
		wantReturning      bool
		wantICloudToken    bool // true ⇒ token is non-nil after Apply (or was non-nil)
	}{
		{0, true, false, true},  // iCloud fresh — keep/generate token
		{1, true, true, true},   // iCloud cached
		{2, false, false, false}, // UDID fresh — token cleared
		{3, false, true, false}, // UDID cached — token cleared
	}
	for _, tc := range cases {
		opts := &devices.PayloadOptions{}
		if err := devices.ApplyHashProvider(opts, tc.provider); err != nil {
			t.Fatalf("chp=%d: unexpected error: %v", tc.provider, err)
		}
		if opts.IcloudSignedIn != tc.wantICloudSignedIn {
			t.Errorf("chp=%d: IcloudSignedIn=%v, want %v", tc.provider, opts.IcloudSignedIn, tc.wantICloudSignedIn)
		}
		if opts.ReturningSession != tc.wantReturning {
			t.Errorf("chp=%d: ReturningSession=%v, want %v", tc.provider, opts.ReturningSession, tc.wantReturning)
		}
		if (opts.IcloudUbiquityToken != nil) != tc.wantICloudToken {
			// Note: chp=0/1 leave the field as-is (nil here in the test);
			// builder will auto-generate a token because IcloudSignedIn=true.
			// We only assert the *clear* behaviour for chp=2/3.
			if tc.provider == 2 || tc.provider == 3 {
				t.Errorf("chp=%d: IcloudUbiquityToken=%v, expected nil", tc.provider, opts.IcloudUbiquityToken)
			}
		}
	}
}

// TestApplyHashProvider_RejectsUnsupported — the picker won't return 4, but
// callers might pass it explicitly. We reject rather than silently corrupt.
func TestApplyHashProvider_RejectsUnsupported(t *testing.T) {
	opts := &devices.PayloadOptions{}
	if err := devices.ApplyHashProvider(opts, 4); err == nil {
		t.Fatal("chp=4 should error")
	}
	if err := devices.ApplyHashProvider(opts, -1); err == nil {
		t.Fatal("chp=-1 should error")
	}
	if err := devices.ApplyHashProvider(nil, 1); err == nil {
		t.Fatal("nil opts should error")
	}
}

// TestApplyHashProviderToConfig_CachedSuppressesImplicit verifies that for
// cached providers (1, 3) the helper sets DeviceHashCached=true so the
// emitter drops `implicit_device_hash` — matching what real iPhone with a
// populated keychain produces (IDA 0x1bc94).
func TestApplyHashProviderToConfig_CachedSuppressesImplicit(t *testing.T) {
	cases := []struct {
		provider     int
		wantCached   bool
		wantHasToken bool // true ⇒ Device.IcloudUbiquityToken is non-nil
	}{
		{0, false, true},
		{1, true, true},
		{2, false, false},
		{3, true, false},
	}
	for _, tc := range cases {
		cfg := &seonsdk.FingerprintConfig{}
		if err := devices.ApplyHashProviderToConfig(cfg, tc.provider); err != nil {
			t.Fatalf("chp=%d: %v", tc.provider, err)
		}
		if cfg.DeviceHashCached != tc.wantCached {
			t.Errorf("chp=%d: DeviceHashCached=%v, want %v", tc.provider, cfg.DeviceHashCached, tc.wantCached)
		}
		hasToken := cfg.Device.IcloudUbiquityToken != nil
		if hasToken != tc.wantHasToken {
			t.Errorf("chp=%d: token-present=%v, want %v", tc.provider, hasToken, tc.wantHasToken)
		}
	}
}

// TestApplyHashProviderToConfig_RoundTripThroughBuilder is the integration
// check: pick a provider, apply it to a built FingerprintConfig, emit the
// fingerprint, and confirm experimental_fields.current_hash_provider matches.
func TestApplyHashProviderToConfig_RoundTripThroughBuilder(t *testing.T) {
	rand.Seed(7)
	for _, want := range []int{0, 1, 2, 3} {
		cfg, err := devices.BuildPayload(devices.PayloadOptions{
			Machine:        "iPhone15,2",
			IOSVersion:     "18.7.6",
			IcloudSignedIn: true, // start with a token so chp=2/3 must clear it
			SkipDC:         true, // tests must not hit the network
		})
		if err != nil {
			t.Fatalf("BuildPayload: %v", err)
		}
		if err := devices.ApplyHashProviderToConfig(cfg, want); err != nil {
			t.Fatalf("chp=%d: %v", want, err)
		}
		raw, err := seonsdk.FingerprintJSON(*cfg)
		if err != nil {
			t.Fatalf("seonsdk.FingerprintJSON: %v", err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		exp, _ := out["experimental_fields"].(map[string]interface{})
		got, _ := exp["current_hash_provider"].(float64)
		if int(got) != want {
			t.Errorf("chp round-trip: applied %d, emitted %v (exp keys: %v)", want, exp["current_hash_provider"], keys(exp))
		}
		_, hasImplicit := exp["implicit_device_hash"]
		// Cached providers MUST NOT emit implicit_device_hash.
		if (want == 1 || want == 3) && hasImplicit {
			t.Errorf("chp=%d (cached) emitted implicit_device_hash — would be flagged CRITICAL by validate-fingerprints", want)
		}
		// Fresh providers SHOULD emit implicit_device_hash (DeviceHashCached=false).
		if (want == 0 || want == 2) && !hasImplicit {
			t.Errorf("chp=%d (fresh) missing implicit_device_hash — real SDK emits it on the fresh-compute path", want)
		}
	}
}

// TestNewDeviceWithHashProvider_FirstFingerprintMatches matches the actual
// user pattern: NewDevice with explicit hash-provider, NewFingerprint, emit.
// The FIRST emitted fingerprint must already carry the requested chp,
// without the caller having to call NewFingerprint twice or hand-mutate.
func TestNewDeviceWithHashProvider_FirstFingerprintMatches(t *testing.T) {
	rand.Seed(11)
	for _, want := range []int{0, 1, 2, 3} {
		device, err := devices.NewDeviceWithHashProvider(devices.DeviceOptions{
			Machine:    "iPhone15,2",
			IOSVersion: "18.7.6",
			SkipDC:     true,
		}, want)
		if err != nil {
			t.Fatalf("chp=%d NewDeviceWithHashProvider: %v", want, err)
		}
		cfg := device.NewFingerprint()
		raw, err := seonsdk.FingerprintJSON(*cfg)
		if err != nil {
			t.Fatalf("chp=%d FingerprintJSON: %v", want, err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("chp=%d unmarshal: %v", want, err)
		}
		exp, _ := out["experimental_fields"].(map[string]interface{})
		got, _ := exp["current_hash_provider"].(float64)
		if int(got) != want {
			t.Errorf("chp=%d: emitted %v on first NewFingerprint() — pre-bump must land us on ReturningSession=true for cached providers", want, exp["current_hash_provider"])
		}
		_, hasImplicit := exp["implicit_device_hash"]
		if (want == 1 || want == 3) && hasImplicit {
			t.Errorf("chp=%d (cached): implicit_device_hash present", want)
		}
		if (want == 0 || want == 2) && !hasImplicit {
			t.Errorf("chp=%d (fresh): implicit_device_hash absent", want)
		}
	}
}

// TestNewRealisticDevice_PopulationDistribution simulates the "one device = one
// fingerprint" Whatnot harness pattern at scale: 5000 device/fingerprint
// pairs. The emitted current_hash_provider distribution should match
// DefaultHashProviderWeights, NOT collapse to chp=0 (the without-helper bug).
func TestNewRealisticDevice_PopulationDistribution(t *testing.T) {
	rand.Seed(13)
	const trials = 5000
	counts := map[int]int{}
	for i := 0; i < trials; i++ {
		device, err := devices.NewRealisticDevice(devices.DeviceOptions{
			Machine:    "iPhone15,2",
			IOSVersion: "18.7.6",
			SkipDC:     true,
		})
		if err != nil {
			t.Fatalf("trial %d: %v", i, err)
		}
		cfg := device.NewFingerprint()
		raw, _ := seonsdk.FingerprintJSON(*cfg)
		var out map[string]interface{}
		_ = json.Unmarshal(raw, &out)
		exp, _ := out["experimental_fields"].(map[string]interface{})
		chp, _ := exp["current_hash_provider"].(float64)
		counts[int(chp)]++
	}
	// Loose tolerance — small deviations are normal at 5k trials.
	checks := []struct {
		provider int
		expect   float64
		tol      float64
	}{
		{1, 0.95, 0.025},
		{0, 0.04, 0.02},
		{3, 0.01, 0.015},
	}
	for _, c := range checks {
		got := float64(counts[c.provider]) / float64(trials)
		if got < c.expect-c.tol || got > c.expect+c.tol {
			t.Errorf("chp=%d emitted-share %.3f outside [%.3f ± %.3f] (counts %v)",
				c.provider, got, c.expect, c.tol, counts)
		}
	}
}

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
