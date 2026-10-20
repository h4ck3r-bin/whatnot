# Location and Telephony Helpers

Helpers to populate `DeviceLocationData` and the telephony fields with realistic values, matching the NSDictionary shapes emitted by `SEONGeolocationProbe` (IDA `0x16d50` / `0x173d0`) and `SEONTelephonyProbe` (IDA `0xc728` / `0xc8b8`).

## Geolocation

### What the SDK emits

`device_location` (top-level, 9 keys from `-[SEONGeolocationProbe scan]` at `0x16d50`):

| Key | Type | SDK source |
|---|---|---|
| `latitude` | double (degrees) | `CLLocation.coordinate.latitude` |
| `longitude` | double (degrees) | `CLLocation.coordinate.longitude` |
| `accuracy` | double (meters) | `CLLocation.horizontalAccuracy` |
| `accuracy_auth_lvl` | int | `CLAccuracyAuthorization`: 0 = full, 1 = reduced |
| `status` | int | SEON-internal: 0 = no scan, 1 = success, 2 = timeout, etc. |
| `auth_status` | int | `CLAuthorizationStatus`: 0 = NotDetermined, 1 = Restricted, 2 = Denied, 3 = AuthorizedAlways, 4 = AuthorizedWhenInUse |
| `is_simulated` | bool | `location.simulated` (always false on non-jailbroken devices) |
| `is_cached` | bool | true if the location came from CLLocationManager cache |
| `timestamp` | int64 (unix-ms) | `location.timestamp.timeIntervalSince1970 * 1000` |

`experimental_device_location` (9 keys from `getExperimentalFields` at `0x173d0`) — optional extra detail: `altitude`, `floor_level`, `vertical_accuracy`, `speed`, `speed_accuracy`, `course`, `course_accuracy`, `is_produced_by_accessory`, `ellipsoidal_altitude`. These go into `FingerprintConfig.Experimental.ExperimentalDeviceLocation` when you want to fill them (most apps don't bother).

### Usage

**Easiest path — auto-populated via the Device.** Set once at device creation; every `NewFingerprint()` picks up a fresh jittered reading.

```go
dev, _ := devices.NewDevice(devices.DeviceOptions{
    Machine: "iPhone16,2", IOSVersion: "18.2",
    Location: &devices.LocationOptions{Preset: devices.CityLondon},
})

cfg := dev.NewFingerprint() // cfg.DeviceLocation already populated
```

**Per-call override.** Pass `FingerprintOverrides.Location` to swap the fix for one call, or `SuppressLocation: true` to block the device default.

```go
cfg := dev.NewFingerprint(devices.FingerprintOverrides{
    Location: &devices.LocationOptions{Preset: devices.CityTokyo},
})
cfg := dev.NewFingerprint(devices.FingerprintOverrides{
    SuppressLocation: true,
})
```

**Manual path.** If you aren't using the Device wrapper, build the `*seonsdk.DeviceLocationData` yourself:

```go
// Simplest: city preset picks coords, everything else is sensible default.
cfg.DeviceLocation = devices.GenerateDeviceLocation(devices.LocationOptions{
    Preset: devices.CityLondon,
})

// Or explicit coordinates with jitter for per-account variation.
cfg.DeviceLocation = devices.GenerateDeviceLocation(devices.LocationOptions{
    Latitude:      52.4862,         // Birmingham
    Longitude:     -1.8904,
    JitterDegrees: 0.005,            // ~500 m random
    Accuracy:      8.5,              // horizontal accuracy in meters
})

// Force-uncached (rare — apps usually get cached fixes).
notCached := false
cfg.DeviceLocation = devices.GenerateDeviceLocation(devices.LocationOptions{
    Preset:   devices.CityTokyo,
    IsCached: &notCached,
})
```

### Built-in city presets

| Preset | Coordinates |
|---|---|
| `CityLondon` | 51.5074, -0.1278 |
| `CityManchester` | 53.4808, -2.2426 |
| `CityEdinburgh` | 55.9533, -3.1883 |
| `CityNewYork` | 40.7128, -74.0060 |
| `CityLosAngeles` | 34.0522, -118.2437 |
| `CityWarsaw` | 52.2297, 21.0122 |
| `CityTokyo` | 35.6762, 139.6503 |
| `CityBerlin` | 52.5200, 13.4050 |
| `CityParis` | 48.8566, 2.3522 |
| `CityMadrid` | 40.4168, -3.7038 |
| `CitySydney` | -33.8688, 151.2093 |

Default jitter is ±0.01° (≈ 1.1 km) around the anchor.

### Default field values

When `LocationOptions` fields are left zero:

| Field | Default | Rationale |
|---|---|---|
| `Accuracy` | 5.0 m | Typical iPhone GPS indoor lock. Outdoor is 2–10 m. |
| `AuthStatus` | 4 (`AuthorizedWhenInUse`) | Most users pick "While Using" when prompted — the safest default for a normal app. |
| `AccuracyAuthLvl` | 0 (`FullAccuracy`) | Default before iOS 14 precise-location toggle opts out. |
| `Status` | 1 | Success. |
| `IsSimulated` | false | True only on jailbroken devices or Xcode simulators. |
| `IsCached` | true (via pointer) | SEON scans complete faster than CoreLocation delivers a fresh fix. |
| `TimestampMs` | now − random(5 s … 5 min) | Plausible cached-reading age. |

## Telephony

### What the SDK emits

Top-level `telephony` (2 keys from `-[SEONTelephonyProbe scan]` at `0xc728`, merged into the outer payload via `SET_KEYS` at constructReport_workQueue):

| Key | Type | SDK source |
|---|---|---|
| `network_config` | string | `-[SEONTelephonyProbe networkConfiguration]` at `0xd028` — maps CTRadioAccessTechnology + WiFi reachability to one of 14 short strings. |
| `is_on_call` | bool | `-[SEONTelephonyProbe isOnPhoneCall]` at `0xce44` — `CTCallCenter.currentCalls.count > 0`. |

`experimental_fields` entries from `getExperimentalFields` at `0xc8b8`:

| Key | Type | Source |
|---|---|---|
| `is_cellular_data_restricted` | bool | `CTCellularData.restrictedState`. |
| `radio_technology` | string? | Raw `CTRadioAccessTechnology*` constant (e.g. `CTRadioAccessTechnologyLTE`), or nil on WiFi-only. |

### The 14 network_config strings

From the switch in `networkConfiguration` at IDA `0xd028`:

| `network_config` | Condition |
|---|---|
| `"WIFI"` | Reachability status == 1 (WiFi). |
| `"GPRS"` | `CTRadioAccessTechnologyGPRS` |
| `"EDGE"` | `CTRadioAccessTechnologyEdge` |
| `"CDMA"` | `CTRadioAccessTechnologyCDMA1x` |
| `"WCDMA"` | `CTRadioAccessTechnologyWCDMA` |
| `"HSDPA"` | `CTRadioAccessTechnologyHSDPA` |
| `"HSUPA"` | `CTRadioAccessTechnologyHSUPA` |
| `"EVDO_0"` | `CTRadioAccessTechnologyCDMAEVDORev0` |
| `"EVDO_A"` | `CTRadioAccessTechnologyCDMAEVDORevA` |
| `"EVDO_B"` | `CTRadioAccessTechnologyCDMAEVDORevB` |
| `"EHRPD"` | `CTRadioAccessTechnologyeHRPD` |
| `"LTE"` | `CTRadioAccessTechnologyLTE` |
| `"5G (NRNSA)"` | `CTRadioAccessTechnologyNRNSA` (5G Non-Standalone) |
| `"5G (NR)"` | `CTRadioAccessTechnologyNR` (5G Standalone) |
| `"None"` | nil (SIM not available or no service) |

### Usage

**Easiest path — auto-populated via the Device.** Defaults to `NetworkWiFi` when `DeviceOptions.Network` is left at its zero value.

```go
dev, _ := devices.NewDevice(devices.DeviceOptions{
    Machine: "iPhone16,2", IOSVersion: "18.2",
    Network: devices.NetworkLTE,
})

cfg := dev.NewFingerprint()
// cfg.Telephony.NetworkConfig    == "LTE"
// cfg.Experimental.RadioTechnology == "CTRadioAccessTechnologyLTE"
```

**Per-call override.** Pass `FingerprintOverrides.Network` pointing to a different state:

```go
state5G := devices.Network5GNSA
cfg := dev.NewFingerprint(devices.FingerprintOverrides{Network: &state5G})
```

**Manual path.** If you aren't using the Device wrapper, call the helper directly:

```go
import (
    seonsdk "status403.com/rsavv/seonsdk-go"
    "status403.com/rsavv/seonsdk-go/devices"
)

cfg := &seonsdk.FingerprintConfig{ /* ... */ }
devices.TelephonyForNetwork(devices.NetworkLTE).Apply(cfg)

// Or build the Telephony struct and inspect before applying.
t := devices.TelephonyForNetwork(devices.Network5GNSA)
t.IsOnCall = false
t.IsCellularDataRestricted = false
t.Apply(cfg)
```

### High-level network states

| State | `network_config` | `radio_technology` |
|---|---|---|
| `NetworkWiFi` (default) | `"WIFI"` | nil |
| `NetworkLTE` | `"LTE"` | `"CTRadioAccessTechnologyLTE"` |
| `Network5GNSA` | `"5G (NRNSA)"` | `"CTRadioAccessTechnologyNRNSA"` |
| `Network5GSA` | `"5G (NR)"` | `"CTRadioAccessTechnologyNR"` |
| `NetworkHSPA` | `"HSDPA"` | `"CTRadioAccessTechnologyHSDPA"` |
| `Network3G` | `"WCDMA"` | `"CTRadioAccessTechnologyWCDMA"` |
| `NetworkEDGE` | `"EDGE"` | `"CTRadioAccessTechnologyEdge"` |
| `NetworkNone` | `"None"` | nil |

### Realistic distribution

To pick a plausible non-WiFi network with weights tuned to modern iPhone population:

```go
state := devices.RandomCellularNetwork()
// 45% LTE, 40% 5G NSA, 10% 5G SA, 3% HSPA, 2% 3G
```

## Returning-session keychain semantics (item 10 clarification)

The SDK stores exactly three keys in the iOS keychain (verified via xrefs to `storeStringIntoKeychain:for:` at `0x1f298`):

1. `device_hash` — written by constructReport (`0x1be18`) after computeHashForOS:device:... builds the implicit hash for the first time.
2. `device_udid` — written by `-[SEONDeviceProbe deviceIdentifier]` (`0x20bf4`).
3. `icloud_token` — written by `-[SEONDeviceProbe iCloudToken]` (`0x2105c`).

There is NO `first_launch` or `last_launch` key anywhere in the SDK. My earlier review erroneously listed these as a gap. The correct behaviour — "does this device read its identifiers from the keychain or compute them fresh?" — is already captured by the `FingerprintConfig.ReturningSession` flag. When `false`, `hashProvider` is 0 (fresh iCloud) or 2 (fresh IDFV); when `true`, `hashProvider` flips to 1 or 3 to signal a cached read.

Set `DeviceOptions.SimulateReturningSession` / `ReturningSession` on subsequent calls if you want to simulate a device that has already launched the app once.
