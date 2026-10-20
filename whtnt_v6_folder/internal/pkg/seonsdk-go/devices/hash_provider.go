package devices

import (
	"fmt"
	"math/rand"

	seonsdk "status403.com/rsavv/seonsdk-go"
)

// HashProviderWeights chooses how often each `current_hash_provider` value is
// emitted, mirroring real-world iPhone population statistics. Verified
// semantics from IDA 0x1cef8 (SEONFingerprint_getExperimentalJsonObject_merge):
//
//	0 = iCloud ubiquityIdentityToken (freshly fetched, hex-formatted)
//	1 = iCloud ubiquityIdentityToken (cached from keychain)
//	2 = device_udid = UIDevice.identifierForVendor (freshly fetched)
//	3 = device_udid = UIDevice.identifierForVendor (cached from keychain)
//	4 = fallback to implicit_device_hash (no iCloud, no IFV — extreme edge case)
//
// Most iPhones in production sit on chp=1 (iCloud signed in + SDK ran
// before). Chp=0 fires only on the very first SDK invocation on a device.
// Our previous default (always 0) made every fresh fingerprint look like a
// "first install" — a population-statistics tell when the same harness is
// emitting hundreds of new accounts a day.
//
// chp=4 is intentionally absent: real devices basically never end up there
// (every iPhone has a deterministic IFV, even with iCloud signed out).
type HashProviderWeights struct {
	ICloudCached float64 // chp=1
	UDIDCached   float64 // chp=3
	ICloudFresh  float64 // chp=0
	UDIDFresh    float64 // chp=2
}

// DefaultHashProviderWeights model a realistic iPhone population for an
// authenticated consumer app like Whatnot. Empirical observation against
// SEON's risk model on 2026-04-30/05-01 showed chp=3 (no-iCloud cached IFV)
// triggering bans even with otherwise-clean fingerprints — Whatnot's
// US/UK livestream-shopping demographic is overwhelmingly iCloud-signed-in,
// so a real iPhone reporting "I deliberately disabled iCloud" is rare and
// scored as low-trust.
//
// Tightened weights (vs the population-realistic 0.75/0.15/0.08/0.02 we
// originally shipped):
//
//   - 95% chp=1: cached iCloud — typical returning iCloud user, the modal
//     real-iPhone state for Whatnot's demographic. Suppresses
//     `implicit_device_hash` (matches IDA 0x1bc94 cached path).
//   - 4%  chp=0: fresh iCloud — first SDK run on an iCloud-signed-in device.
//     Emits `implicit_device_hash`. Plausible for users who just installed
//     Whatnot or recently signed back into iCloud.
//   - 1%  chp=3: cached IFV (no iCloud) — rare, privacy-conscious users.
//   - 0%  chp=2: fresh IFV (no iCloud) — basically never happens. Tap-tap
//     of "first launch + signed out of iCloud" is a vanishingly small slice.
//
// If you're targeting a different app whose demographic looks different,
// override by passing a custom HashProviderWeights{} to .Pick() or to
// NewDeviceWithHashProvider.
var DefaultHashProviderWeights = HashProviderWeights{
	ICloudCached: 0.95,
	ICloudFresh:  0.04,
	UDIDCached:   0.01,
	UDIDFresh:    0.00,
}

// PickRealisticHashProvider returns one of {0, 1, 2, 3} drawn from
// DefaultHashProviderWeights. The result can be passed straight to
// ApplyHashProvider to configure a PayloadOptions accordingly.
func PickRealisticHashProvider() int {
	return DefaultHashProviderWeights.Pick()
}

// Pick returns one of {0, 1, 2, 3} according to w. If w sums to zero or has
// any negative entries, the function falls back to chp=1 — the safest
// majority-population choice.
func (w HashProviderWeights) Pick() int {
	total := w.ICloudCached + w.UDIDCached + w.ICloudFresh + w.UDIDFresh
	if total <= 0 {
		return 1
	}
	r := rand.Float64() * total
	switch {
	case r < w.ICloudCached:
		return 1
	case r < w.ICloudCached+w.UDIDCached:
		return 3
	case r < w.ICloudCached+w.UDIDCached+w.ICloudFresh:
		return 0
	default:
		return 2
	}
}

// ApplyHashProvider configures opts so that BuildPayload's resulting
// FingerprintConfig will compute the requested current_hash_provider value.
//
// The mapping enforces the IDA-verified rules at 0x1ae78–0x1af74:
//
//	provider=0 (iCloud fresh):    IcloudSignedIn=true,  ReturningSession=false
//	provider=1 (iCloud cached):   IcloudSignedIn=true,  ReturningSession=true
//	provider=2 (UDID fresh):      IcloudSignedIn=false, ReturningSession=false, IcloudUbiquityToken=nil
//	provider=3 (UDID cached):     IcloudSignedIn=false, ReturningSession=true,  IcloudUbiquityToken=nil
//
// This helper does NOT toggle DeviceHashCached on FingerprintConfig (that
// flag lives on the assembled config, not on PayloadOptions). For
// chp=1 / chp=3 you ALSO want cfg.DeviceHashCached=true after BuildPayload to
// drop `implicit_device_hash` from the wire — real iPhone captures with
// cached providers never emit it (IDA 0x1bc94: getImplicitDeviceHash returns
// nil when computeHashFor was skipped). ApplyHashProviderToConfig handles that.
//
// Returns an error for unsupported provider values.
func ApplyHashProvider(opts *PayloadOptions, provider int) error {
	if opts == nil {
		return fmt.Errorf("ApplyHashProvider: nil opts")
	}
	switch provider {
	case 0:
		opts.IcloudSignedIn = true
		opts.ReturningSession = false
	case 1:
		opts.IcloudSignedIn = true
		opts.ReturningSession = true
	case 2:
		opts.IcloudSignedIn = false
		opts.IcloudUbiquityToken = nil
		opts.ReturningSession = false
	case 3:
		opts.IcloudSignedIn = false
		opts.IcloudUbiquityToken = nil
		opts.ReturningSession = true
	default:
		return fmt.Errorf("ApplyHashProvider: unsupported provider %d (must be 0, 1, 2, or 3)", provider)
	}
	return nil
}

// NewRealisticDevice creates a Device whose first NewFingerprint() call will
// emit a population-realistic current_hash_provider value. It wraps NewDevice
// with PickRealisticHashProvider + the persistent-state plumbing required so
// that even when the caller only ever runs ONE NewFingerprint() per Device
// (the typical Whatnot harness pattern: one device = one new account), the
// emitted chp is drawn from the real iPhone population — not pinned to chp=0.
//
// Without this helper, NewDevice + NewFingerprint always produces chp=0 for
// the first call (since Device.generationCount is 0, ReturningSession=false).
// That makes every freshly-generated fingerprint look like a "first install",
// which is a population-statistics tell SEON's risk model can score against.
//
// The DeviceOptions.IcloudSignedIn / IcloudUbiquityToken / DeviceHashCached
// fields are OVERRIDDEN by this helper to be coherent with the picked
// provider:
//
//	chp=0 → IcloudSignedIn=true,  DeviceHashCached=false (token kept/generated)
//	chp=1 → IcloudSignedIn=true,  DeviceHashCached=true  (token kept/generated)
//	chp=2 → IcloudSignedIn=false, IcloudUbiquityToken=nil, DeviceHashCached=false
//	chp=3 → IcloudSignedIn=false, IcloudUbiquityToken=nil, DeviceHashCached=true
//
// Pass DefaultHashProviderWeights via NewDeviceWithHashProvider if you want
// to control the weighting yourself.
func NewRealisticDevice(opts DeviceOptions) (*Device, error) {
	return NewDeviceWithHashProvider(opts, PickRealisticHashProvider())
}

// NewDeviceWithHashProvider is the explicit-provider variant of
// NewRealisticDevice. Use it when you want a specific chp value (e.g.
// chp=1 for "guaranteed cached-iCloud" sessions) instead of drawing from
// the population weights.
//
// For cached providers (chp=1 or 3), this helper pre-bumps the Device's
// generation count so the FIRST NewFingerprint() call emits ReturningSession
// = true. Without that, the first call would emit chp=0/2 (fresh) regardless
// of the supplied provider value.
func NewDeviceWithHashProvider(opts DeviceOptions, provider int) (*Device, error) {
	switch provider {
	case 0:
		opts.IcloudSignedIn = true
		opts.DeviceHashCached = false
	case 1:
		opts.IcloudSignedIn = true
		opts.DeviceHashCached = true
	case 2:
		opts.IcloudSignedIn = false
		opts.IcloudUbiquityToken = nil
		opts.DeviceHashCached = false
	case 3:
		opts.IcloudSignedIn = false
		opts.IcloudUbiquityToken = nil
		opts.DeviceHashCached = true
	default:
		return nil, fmt.Errorf("NewDeviceWithHashProvider: unsupported provider %d (must be 0, 1, 2, or 3)", provider)
	}
	d, err := NewDevice(opts)
	if err != nil {
		return nil, err
	}
	if provider == 1 || provider == 3 {
		// Pre-bump so the next NewFingerprint() lands on ReturningSession=true.
		// Bumping to 1 means the first NewFingerprint() will increment to 2,
		// at which point gen > 1 and ReturningSession fires.
		d.mu.Lock()
		d.generationCount = 1
		d.mu.Unlock()
	}
	return d, nil
}

// ApplyHashProviderToConfig adjusts an already-assembled FingerprintConfig to
// produce the requested current_hash_provider value at emit time. Unlike
// ApplyHashProvider (which works on PayloadOptions before assembly), this
// mutates Device.IcloudUbiquityToken / ReturningSession / DeviceHashCached
// directly so the next BuildPayload(*FingerprintConfig) call uses them.
//
//	provider=0: ensures IcloudUbiquityToken non-nil, ReturningSession=false, DeviceHashCached=false
//	provider=1: ensures IcloudUbiquityToken non-nil, ReturningSession=true,  DeviceHashCached=true
//	provider=2: clears  IcloudUbiquityToken,         ReturningSession=false, DeviceHashCached=false
//	provider=3: clears  IcloudUbiquityToken,         ReturningSession=true,  DeviceHashCached=true
//
// DeviceHashCached=true on cached providers suppresses `implicit_device_hash`
// emission, matching what real iPhones produce when the SEON keychain entry
// is already populated (IDA 0x1bc94). Without that, validate-fingerprints
// flags the payload CRITICAL.
//
// Returns an error for unsupported provider values.
func ApplyHashProviderToConfig(cfg *seonsdk.FingerprintConfig, provider int) error {
	if cfg == nil {
		return fmt.Errorf("ApplyHashProviderToConfig: nil cfg")
	}
	switch provider {
	case 0:
		if cfg.Device.IcloudUbiquityToken == nil {
			cfg.Device.IcloudUbiquityToken = RandomIcloudUbiquityToken()
		}
		cfg.ReturningSession = false
		cfg.DeviceHashCached = false
	case 1:
		if cfg.Device.IcloudUbiquityToken == nil {
			cfg.Device.IcloudUbiquityToken = RandomIcloudUbiquityToken()
		}
		cfg.ReturningSession = true
		cfg.DeviceHashCached = true
	case 2:
		cfg.Device.IcloudUbiquityToken = nil
		cfg.ReturningSession = false
		cfg.DeviceHashCached = false
	case 3:
		cfg.Device.IcloudUbiquityToken = nil
		cfg.ReturningSession = true
		cfg.DeviceHashCached = true
	default:
		return fmt.Errorf("ApplyHashProviderToConfig: unsupported provider %d (must be 0, 1, 2, or 3)", provider)
	}
	return nil
}
