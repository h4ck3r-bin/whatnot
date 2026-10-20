// Package devices is the high-level entry point for generating SEON
// fingerprints from the Go SDK. It wraps the low-level primitives in
// status403.com/rsavv/seonsdk-go/payload and status403.com/rsavv/seonsdk-go/dc
// behind a single *Device handle that carries stable identity (IDFV, IDFA,
// iCloud token, device_hash) across calls, mirroring how a real iOS
// keychain persists those values for the real SDK.
//
// # The two entry points
//
//   - NewDevice + Device.NewFingerprint: use for stateful simulation. The
//     Device is a virtual iPhone — identity is frozen at creation, but each
//     NewFingerprint() call produces a fresh FingerprintConfig with realistic
//     per-scan variance (battery drift, uptime tick, re-randomised brightness
//     / volume / pasteboard hash / UI style, etc.). This is the path most
//     integrations should use.
//
//   - BuildPayload: one-shot configuration builder for stateless generation
//     (testing, ad-hoc fingerprint production, legacy call sites). Produces
//     the same FingerprintConfig shape but without the Device's auto-
//     populated location/telephony/behaviour machinery.
//
// # Auto-populated fields (via the Device path only)
//
// Setting DeviceOptions.Location, DeviceOptions.Network, or
// DeviceOptions.BehaviourMonitoring causes NewFingerprint() to fill the
// corresponding payload blocks automatically:
//
//	Location             → cfg.DeviceLocation (9 fields)
//	                        cfg.Experimental.ExperimentalDeviceLocation (9 fields)
//	                        cfg.GeolocationConfig.GeolocationEnabled = true
//	Network              → cfg.Telephony.NetworkConfig
//	                        cfg.Experimental.RadioTechnology
//	BehaviourMonitoring  → cfg.BehaviourResults (30 keys)
//
// Per-call overrides live on FingerprintOverrides and follow "override wins"
// semantics so a single *Device can emit fingerprints for login (with
// behaviour), address mutations (without behaviour), or any other Whatnot
// surface without rebuilding the Device.
//
// # IDA-verified correspondences
//
// Every defaulted value in this package is pinned to a specific SEON SDK
// 5.6.3 address via inline comments. The most load-bearing references:
//
//	IDFV keychain store  → 0x20af0 (deviceIdentifier)
//	iCloud token store   → 0x20cfc (iCloudToken)
//	device_hash store    → 0x1be18 (constructReport)
//	hashProvider cascade → 0/1/2/3/4 matching 0x1af64–0x21074
//	Accept-Language      → resolveAcceptLanguage follows [NSLocale preferredLanguages]
//	Battery semantics    → batteryCharging (0x1e018) vs isPluggedIn (0x1e094)
//	audio_mute_status    → always false (dead code, verified at 0x1f000)
//	wifi_{ssid,mac}      → nil stub for iOS 14+ non-entitled apps (0x8094/0x86e4)
//
// See docs/device-api.md, docs/behaviour.md, and docs/location-and-telephony.md
// for the complete field map, semantics, and usage examples.
package devices
