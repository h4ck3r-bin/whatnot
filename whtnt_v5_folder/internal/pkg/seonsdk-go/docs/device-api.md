# Device API — persistent virtual device

## Overview

`devices.NewDevice()` creates a persistent virtual iOS device handle. Each call to `Device.NewFingerprint()` returns a fresh `FingerprintConfig` with only realistic per-scan variance applied — stable identity fields stay frozen, matching real iOS SDK behavior where IDFV, iCloud token, and `device_hash` are cached in the keychain.

```go
device, err := devices.NewDevice(devices.DeviceOptions{
    Machine:        "iPhone16,2",
    IOSVersion:     "18.2",
    UserDeviceName: "Madie's iPhone",
    IcloudSignedIn: true,
})

// First generation — current_hash_provider=0 (fresh iCloud) or 2 (fresh IDFV)
cfg := device.NewFingerprint()
wire, _ := seonsdk.Fingerprint(*cfg)

// Later generation — current_hash_provider=1 (cached iCloud) or 3 (cached IDFV),
// same device_hash, fresh session_id, drifted battery/uptime, re-randomized
// brightness/volume/pasteboard, etc.
cfg = device.NewFingerprint()
wire, _ = seonsdk.Fingerprint(*cfg)
```

## Device / iOS version compatibility

`devices.NewDevice()` now rejects impossible combinations. A real iPhone cannot run an iOS version older than the one it shipped with, so generators that randomize `Machine` and `IOSVersion` independently produce dead-giveaway fingerprints like "iPhone 16 Plus on iOS 17.7.3".

```go
// Rejected — iPhone 16 Plus shipped with iOS 18.0
devices.NewDevice(devices.DeviceOptions{Machine: "iPhone17,4", IOSVersion: "17.7.3"})
// → impossible iOS version: iPhone17,4 cannot run iOS 17.7.3 (minimum supported: 18.0)

// Pick a valid version for a given device:
ios := devices.PickCompatibleIOSVersion("iPhone17,4")  // e.g. "18.7.4" or "26.0"
```

Minimum iOS versions (from `devices/specs.go`):

| Machine range | Device | Min iOS |
|---|---|---|
| iPhone11,2/4/6 | iPhone XS / XS Max (A12) | 12.0 |
| iPhone11,8 | iPhone XR (A12) | 12.0 |
| iPhone12,1/3/5 | iPhone 11 family (A13) | 13.0 |
| iPhone12,8 | iPhone SE 2nd Gen (A13) | 13.4 |
| iPhone13,x | iPhone 12 family (A14) | 14.1 / 14.2 |
| iPhone14,2–5 | iPhone 13 family (A15) | 15.0 |
| iPhone14,6 | iPhone SE 3rd Gen (A15) | 15.4 |
| iPhone14,7/8 | iPhone 14 / 14 Plus (A15) | 16.0 |
| iPhone15,2/3 | iPhone 14 Pro family (A16) | 16.0 |
| iPhone15,4/5 | iPhone 15 / 15 Plus (A16) | 17.0 |
| iPhone16,1/2 | iPhone 15 Pro family (A17 Pro) | 17.0 |
| iPhone17,1–4 | iPhone 16 family (A18/A18 Pro) | 18.0 |
| iPhone17,5 | iPhone 16e (A18) | 18.3 |
| iPhone18,1–4 | iPhone 17 / Air (A19 Pro) | 26.0 |

Every supported iOS version maps to one or more known build numbers via `devices.BuildNumberFor(iosVersion)` — sourced from ipsw.me and cross-checked against multiple iPhone models. These populate the `kernel_version` payload field (see `docs/ios-build-number.md`).

## Locale: SpeakerName for custom regions

`LocaleConfig.SpeakerName` is the localised word iOS uses for the built-in loudspeaker route — it flows into `experimental_fields.av_output_info.portName` and each `channels[*].channelName` (suffixed with ` 1` / ` 2`). Presets populate this automatically; custom `LocaleConfig` callers MUST supply it or the wire bytes will emit English `"Speaker"` next to a non-English `region_country`:

```go
custom := &seonsdk.LocaleConfig{
    RegionLanguage:     "cs",
    RegionCountry:      "CZ",
    TimezoneIdentifier: "Europe/Prague",
    RegionTimezone:     "+01:00",
    CalendarIdentifier: "gregorian",
    SpeakerName:        "Reproduktor", // Czech — UIKit's own localisation
}
```

Known-good values for the shipped presets:

| Preset | SpeakerName |
|---|---|
| `LocaleUKEnglish`, `LocaleUSEnglish`, `LocaleIrishEnglish`, `LocaleAustraliaEnglish`, `LocaleCanadaEnglish` | `Speaker` |
| `LocaleGermanGermany` | `Lautsprecher` |
| `LocaleFrenchFrance` | `Haut-parleur` |
| `LocaleSpanishSpain` | `Altavoz` |
| `LocaleItalianItaly` | `Altoparlante` |
| `LocalePolishPoland` | `Głośnik` *(verified)* |
| `LocaleDutchNetherlands` | `Luidspreker` |
| `LocaleJapaneseJapan` | `スピーカー` |
| `LocaleKoreanKorea` | `스피커` |

## Network subnet consistency

`nw_interfaces.en0` and `def_gw` now share the same /24. On a real home network the gateway is always the `.1` of your own subnet — randomizing them independently produces impossible combinations like `en0=192.168.5.221` with `def_gw=192.168.3.1`.

## Server-side DC evasion: `SkipDC` + `SimulateDCTimeout`

**The most important behavioral knob for server-side fingerprint generation.** Verified via IDA at `0x7a4c` (completion block) and `0x1c450` (payload merger):

- `dynamic_evaluator_results` is added to the payload **only** when the DC call succeeded with HTTP 200 + valid JSON body. If the request times out, the key is **omitted entirely**.
- SEON's backend cross-references the DC request (session_id + source IP + timing) against the fingerprint it receives via Whatnot. If the DC was made from your server IP but the fingerprint claims to be from a mobile device, SEON detects the mismatch and bans.

For server-side setups where DC reliably succeeds, **`SkipDC: true` is the correct behavior** — it replicates the "DC unreachable" wire pattern (no `dynamic_evaluator_results` key in the payload) that matches what happens on real devices behind hostile networks, and gives SEON no session log to cross-reference.

Combine with `SimulateDCTimeout: true` to make `performance_network_call` report `~2000 ms` instead of a simulated 50–400 ms value, matching the fixed 2s NSURLSession timeout the real SDK sets at `0x7918`:

```go
device, _ := devices.NewDevice(devices.DeviceOptions{
    Machine:           machine,
    IOSVersion:        ios,
    IcloudSignedIn:    true,
    SkipDC:            true,
    SimulateDCTimeout: true,  // perf_network_call = 2000.xxx ms
})
```

This produces:
- `dynamic_evaluator_results` → **omitted** from the payload
- `performance_network_call` → `"2000.xxx ms"` (matches real-device timeout pattern)
- `stored_errors` → empty; if you want it populated with the timeout log entry that the real SDK emits, override via `cfg.StoredErrors = seonsdk.BuildDCErrorLog(dc.ErrorRequestTimedOut)`

## DC failure → `stored_errors` (opt-in)

The real iOS SDK logs DC failures via `SEONErrorLogger` (verified at IDA `0x7a4c` and `0x9e98`). When the DC request times out, the resulting `stored_errors` entry is:

```json
"stored_errors": {
  "SEONNetworkProbe_sendDynamicComponentRequest:dcRequestCompletionBlock::The request timed out.": 1
}
```

**This is NOT auto-populated** — because changing the `stored_errors` default from `[]` to `{}` mid-stream invalidates the trust cluster your backend has already built up for fingerprints from this SDK. If you want to match the real SDK, pass it explicitly:

```go
import "status403.com/rsavv/seonsdk-go/dc"

cfg := device.NewFingerprint()
cfg.StoredErrors = seonsdk.BuildDCErrorLog(dc.ErrorRequestTimedOut)
// Or for non-200: seonsdk.BuildDCErrorLog(fmt.Sprintf(dc.ErrorRequestFailedFormat, 500))
```

Three DC error messages per IDA:
- `dc.ErrorRequestTimedOut` = `"The request timed out."` — `NSURLErrorTimedOut` localized description
- `dc.ErrorRequestFailedFormat` = `"Request failed - %d"` — HTTP non-200 response
- `dc.ErrorParseDCResponse` = `"Error parsing DC response JSON"` — malformed body

## Full field classification (verified via IDA)

Each field below has been traced from its `SET_KEYS` entry → `scanWithPerformanceTracking` block_invoke → ObjC method → underlying iOS API.

### Always frozen per Device (identity)

These are cached at device creation and never change across `NewFingerprint()` calls.

| Field | Source | IDA | Why frozen |
|---|---|---|---|
| `device_name` | `devicePlatform` → `platformNames[utsname.machine]` | `0x21140` | Hardware model string |
| `device_adid` | `deviceAdIdentifier` → `ASIdentifierManager.advertisingIdentifier` | `0x20c58` | Stable per-install; zeros post-ATT |
| `device_udid` | `deviceIdentifier` → keychain `"device_udid"` → IDFV | `0x20af0` | **Keychain-cached** |
| `ios_device_name` | `deviceName` → `UIDevice.currentDevice.name` | `0x20818` | User's name, rarely changes |
| `ios_version` | `deviceVersion` → `UIDevice.systemVersion` | `0x209fc` | Changes only on OS update |
| `screen_width` / `screen_height` | `getScreenWidth` / `getScreenHeight` → bounds × scale | `0x2055c`/`0x205d8` | Pixel resolution |
| `icloud_ubiquity_token` | `iCloudToken` → keychain `"icloud_token"` → `NSFileManager.ubiquityIdentityToken` | `0x20cfc` | **Keychain-cached** |
| `cpu_type` (`"ARM_64E"`), `cpu_count` (6 on every supported model), `is_emulator` (false) | Hardware probe | `0x1deb8`–`0x1e32c` | Hardware constants |
| `physical_memory` | Hardware probe — `sysctl hw.memsize` (uint64 bytes) | `0x1daf8` | **Firmware-reserved value, not nominal RAM × 1024³.** iPhone 14 Pro reports 5,940,199,424 for its 6 GB class. Pick via `memsize{3,4,6,8,12}GB` constants in `devices/specs.go`. |
| `kernel_name` (always `"Darwin"`), `kernel_arch` (always `"ARM_64E"`) | OS probe | `0x153d4`, `0x153ec` | Hardcoded Apple constants |
| `kernel_version` | OS probe — `sysctl kern.osversion` | `0x15be8` | **iOS build number string (e.g., `"22F76"`)**, not the full Darwin kernel version. Mapped via `devices.BuildNumberFor(iosVersion)`. |
| `is_jailbroken`, `is_ios_app_on_mac`, `is_debugger_attached` | OS probe | — | Clean-device booleans |
| `can_send_mail`, `can_send_text` | Media probe | `0x1eb88`/`0x1eba4` | Device capability |
| All `KEYS_LOCALE` (5) | Locale probe | `0xa1b0` | User locale settings |
| All `KEYS_ACCESSIBILITY` (8) | Accessibility probe | `0x5bc0` | User accessibility prefs |
| All `KEYS_AUTH` (2) | Auth probe | `0x19608` | Biometric/passcode state |
| `force_touch_support`, `display_gamut`, `native_bounds`, `is_arm64`, `is_display_zoom_enabled`, `has_invalid_code_sig`, `pid`, `is_react_native`, `is_flutter`, `is_root_fs_compromised`, `date_format`, `supported_biometry_type`, `nw_interfaces`, `def_gw` | Experimental | various | Stable device/app capability |
| `device_hash` | `constructReport` keychain `"device_hash"` | `0x1b8ec` | **Keychain-cached** |
| `implicit_device_hash` | Derived from stable 20-field dict | `0x1a8cc` | Stable inputs → stable hash |

### Volatile per fingerprint (re-read each generation)

| Field | Source | IDA | Behavior in Device API |
|---|---|---|---|
| `session_id` | `SEONFingerprint.sessionId` | — | Fresh UUID per call |
| `timestamp` | `NSDate` epoch ms | `0x1c7e0` | Real wall-clock now |
| `battery_level` | `floor(UIDevice.batteryLevel × 100)` | `0x1e168` | Time-aware drift (~0.3%/min unplugged, +0.5%/min plugged) |
| `battery_charging` | `batteryState == 2 (Charging)` | `0x1e018` | `pluggedIn && battery < 100` |
| `is_plugged_in` | `batteryState ∈ {2, 3}` (Charging or Full) | `0x1e094` | Tracks `SetPluggedIn()` state |
| `system_uptime` | `time() - sysctl(KERN_BOOTTIME)` | `0x1e230` | Monotonic: `initial + real_elapsed_seconds` |
| `screen_brightness` | `floor(UIScreen.brightness × 100)` | `0x215b4` | Re-randomized 30–80 per call |
| `audio_volume_current` | `round(AVAudioSession.outputVolume × 100)` | `0x1eb1c` | Re-randomized 0–100 per call |
| `audio_mute_status` | `self->_audioMuteStatus` (readonly ivar, never written) | `0x1f000` | **Always false** — the `SEONSharkfoodMuteSwitchDetector` class exists in the binary but `+shared` is never called, so the detector is never instantiated and the ivar stays at its zero-init value forever. Dead-code feature. Not actually volatile despite the detector framework's existence. |
| `pasteboard_hash` | `SHA256(detectedPatterns)` from `UIPasteboard.detectPatternsForPatterns` | `0x2161c` | Re-randomized per call |
| `is_screen_captured` | `UIScreen.isCaptured` | `0x20a9c` | Overridable per fingerprint |
| `device_orientation` | `UIWindowScene.interfaceOrientation` | `0x212ac` | Portrait default; landscape via override |
| `is_on_call` | `CallKit` | — | Always false for clean session |
| `user_interface_style` | `UITraitCollection.userInterfaceStyle` | `0x21f7c` | Re-randomized (70% Light / 30% Dark — simulates auto dark mode) |
| `thermal_state` | `NSProcessInfo.thermalState` | `0x20654` | Default 0 (Nominal); overridable |
| `is_low_power_enabled` | `NSProcessInfo.isLowPowerModeEnabled` | — | `true` when battery ≤ 20% && unplugged |
| `network_config` | Carrier/SEONReachability | `0xd028` | WIFI default |
| `vpn_state`, `proxy_state`, `wifi_ssid`, `wifi_mac_address`, `accessory_hash` | Network/Telephony/Media experimental | — | Default null |
| `radio_technology` | Telephony probe — `numberWithBool:isCellServiceAvailable` | `0xca30` | **Boolean**, not a string. `false` when WiFi-only or no SIM; `true` when any cellular service is reachable. NOT a `CTRadioAccessTechnology*` identifier. |
| `is_cellular_data_restricted`, `ap_is_hotspot` | Telephony / Network probe | `0xc9f0` / NetworkProbe | **Always emitted** as JSON bool (default `false`) |
| `vpn_protocol`, `proxy_type` | Network probe | `0x6b58` | **Always emitted**; null when no VPN, `"kCFProxyTypeNone"` when no proxy, real values when active (`"utun5"`, `"kCFProxyTypeHTTPS"`, etc.) |
| `proxy_autoconfig_settings` | Network probe | `0x6ca8` | Conditional — only when `proxy_type == kCFProxyTypeAutoConfigurationURL` |
| `av_output_info` | Media probe — wraps `AVAudioSession.currentRoute.outputs` | `0x1ea28` | **Always emitted**. Channel/port names localised per `LocaleConfig.SpeakerName` (Polish = `"Głośnik"`, German = `"Lautsprecher"`, etc. — see payload-schema.md § av_output_info). `portType`/`portUID` stay `"Speaker"` regardless. |
| `connected_accessories` | Device probe — `numberWithUnsignedInteger:count` | `0x201d4` | **Integer** (not array). 0 when no external accessories. |
| `performance_probes` | 10-probe × 3-phase timing dict | `0x1c8d0` | **Always emitted**. Each phase is `"%.3f"` ms string or `null` when the phase didn't run. Top-level, NOT inside `experimental_fields`. |

### Keychain-cached (frozen per device, regenerated after reinstall on a real device)

Exactly 3 values are cached in the real iOS keychain:
- `device_hash` (0x1b8ec keychain lookup)
- `device_udid` / IDFV (0x20af0)
- `icloud_token` (0x20cfc)

Our `Device` struct freezes all 3 at creation.

### `current_hash_provider` values (verified addresses)

| Value | Set at | Meaning |
|---|---|---|
| 0 | `0x21074` (iCloudToken fresh) | First call, iCloud signed in |
| 1 | `0x20f9c` (iCloudToken cached) | 2nd+ call, iCloud signed in |
| 2 | `0x20c0c` (deviceIdentifier fresh) | First call, no iCloud |
| 3 | `0x20b5c` (deviceIdentifier cached) | 2nd+ call, no iCloud |
| 4 | `0x1af64` (computeHash fallback) | Neither available (unused in our SDK since UDID is always generated) |

`Device.NewFingerprint()` flips from fresh (0/2) on the first call to cached (1/3) on subsequent calls automatically.

## Battery drift behavior

```
unplugged: battery' = clamp(initial - 0.3 * elapsed_minutes + jitter, 1, 100)
plugged:   battery' = clamp(initial + 0.5 * elapsed_minutes + jitter, 1, 100)
```

Toggle with `device.SetPluggedIn(true|false)`. `battery_charging` becomes `pluggedIn && battery < 100` (to match the real SDK's `batteryState == 2 (Charging)` semantic, which returns false when Full).

## Overrides per fingerprint

```go
plug := true
cfg := device.NewFingerprint(devices.FingerprintOverrides{
    PluggedIn:            &plug,
    OrientationLandscape: true,
    IsScreenCaptured:     &trueVal,
    ThermalState:         &thermalInt,
    Behaviour:            &devices.BehaviourOptions{...}, // see below
})
```

### Per-call behaviour monitoring toggle

The same `*Device` can generate fingerprints with or without `behaviour_results` depending on whether the caller supplies a `FingerprintOverrides.Behaviour`. This matches Whatnot's real usage pattern — only 3 of 13 SEON call sites (login, phone-signup entry, phone-onboarding teardown) run behaviour monitoring; the other 10 (address mutations, sign-up attribution, etc.) don't.

Semantics:

| `DeviceOptions.BehaviourMonitoring` | `FingerprintOverrides.Behaviour` | Result |
|---|---|---|
| nil | nil | no behaviour |
| nil | `&{Enabled: true, …}` | behaviour for this call |
| nil | `&{Enabled: false}` | no behaviour |
| `&{Enabled: true}` | nil | behaviour (device default) |
| `&{Enabled: true}` | `&{Enabled: false}` | no behaviour (override) |
| `&{Enabled: true}` | `&{Enabled: true, Activity: Walking}` | behaviour, different activity |

Full usage guide: [behaviour.md](behaviour.md).

## Location and telephony — auto-populated

Location and telephony are auto-populated on every `NewFingerprint()`, symmetric with behaviour monitoring. You don't need to call any helper manually — just set the fields on `DeviceOptions`.

```go
dev, _ := devices.NewDevice(devices.DeviceOptions{
    Machine:    "iPhone16,2",
    IOSVersion: "18.2",
    Location:   &devices.LocationOptions{Preset: devices.CityLondon},
    Network:    devices.NetworkLTE,
})

cfg := dev.NewFingerprint()
// cfg.DeviceLocation             → auto-populated London coords w/ fresh jitter per call
// cfg.Telephony.NetworkConfig    → "LTE"
// cfg.Experimental.RadioTechnology → "CTRadioAccessTechnologyLTE"
```

### Semantics at a glance

| `DeviceOptions.Location` | `Overrides.Location` | `Overrides.SuppressLocation` | Result |
|---|---|---|---|
| nil | nil | — | no location |
| nil | `&LocationOptions{...}` | false | location for this call |
| `&LocationOptions{A}` | nil | false | `A` |
| `&LocationOptions{A}` | `&LocationOptions{B}` | false | `B` (override) |
| `&LocationOptions{A}` | nil | **true** | no location (override) |

| `DeviceOptions.Network` | `Overrides.Network` | Result |
|---|---|---|
| zero value | nil | `NetworkWiFi` |
| `NetworkLTE` | nil | `NetworkLTE` |
| `NetworkLTE` | `&Network5GNSA` | `Network5GNSA` (override) |

Jitter is re-rolled every call — two fingerprints from the same device will have slightly different lat/lon, matching the real SDK's behaviour where every `fetchNewLocation` reads CoreLocation fresh.

Full helper docs (city presets, fallback defaults, every `network_config` string, etc.): [location-and-telephony.md](location-and-telephony.md).

## Relationship with `BuildPayload`

`devices.BuildPayload()` is retained for one-shot usage. It returns the same `FingerprintConfig` shape as `Device.NewFingerprint()` but without any device state — each call produces independent fingerprints.

Use `BuildPayload` for stateless generation, `NewDevice` + `NewFingerprint` when you want multiple fingerprints from the same virtual device.

### Migration note for `BuildPayload` callers

The auto-population features (`Location`, `Network`, `BehaviourMonitoring`) added to `DeviceOptions` are **only** active on the `Device.NewFingerprint()` path. `BuildPayload` does not read them. If you're on `BuildPayload` and want the new goodies, either:

**(a) Switch to `NewDevice`** (recommended — same API shape, extra capabilities):

```go
// Before:
cfg, _ := devices.BuildPayload(devices.PayloadOptions{
    Machine: "iPhone16,2", IOSVersion: "18.2",
})

// After:
dev, _ := devices.NewDevice(devices.DeviceOptions{
    Machine: "iPhone16,2", IOSVersion: "18.2",
    Location: &devices.LocationOptions{Preset: devices.CityLondon},
    Network:  devices.NetworkLTE,
})
cfg := dev.NewFingerprint()
```

**(b) Keep `BuildPayload` and layer the helpers manually** — the original helpers are still exported:

```go
cfg, _ := devices.BuildPayload(devices.PayloadOptions{/* ... */})
cfg.DeviceLocation = devices.GenerateDeviceLocation(devices.LocationOptions{Preset: devices.CityLondon})
cfg.Experimental.ExperimentalDeviceLocation = devices.GenerateExperimentalDeviceLocation(devices.LocationOptions{Preset: devices.CityLondon})
devices.TelephonyForNetwork(devices.NetworkLTE).Apply(cfg)
if cfg.GeolocationConfig == nil {
    cfg.GeolocationConfig = seonsdk.DefaultGeolocationConfig()
}
cfg.GeolocationConfig.GeolocationEnabled = true
```

`BuildPayload` will stay supported for stateless use cases (tests, quick scripts). No plans to deprecate.
