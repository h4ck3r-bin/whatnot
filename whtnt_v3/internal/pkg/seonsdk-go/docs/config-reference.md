# Configuration Reference

## FingerprintConfig

The top-level configuration struct that drives `Fingerprint()` and `FingerprintJSON()`.

```go
type FingerprintConfig struct {
    SessionID string    // Auto-generated UUID if empty
    SkipDC    bool      // Skip the DC side-channel request
    Proxy     string    // Proxy URL for the DC request (e.g. "http://host:port", "socks5://host:port")

    Locale        LocaleConfig
    Device        DeviceConfig
    Hardware      HardwareConfig
    Media         MediaConfig
    Network       NetworkConfig
    OS            OSConfig
    Telephony     TelephonyConfig
    Accessibility AccessibilityConfig
    Auth          AuthConfig

    GeolocationConfig *GeolocationConfigValues  // nil = use defaults
    DeviceLocation    *DeviceLocationData        // nil = device_location: null
    BehaviourResults  *BehaviourData             // nil = behaviour_results: null

    Experimental ExperimentalConfig

    StoredErrors            interface{}  // array of error objects, or nil
    PortScanResult          []int        // category indices, e.g. [6, 8] for SSH + Frida
    DynamicEvaluatorResults interface{}  // string from DC response, usually nil

    // Override performance timing strings (optional — realistic values auto-generated if empty)
    PerformanceActualProcessing string  // e.g. "1854.746 ms"
    PerformanceExecTime         string
    PerformanceNetworkCall      string
    TimestampOverride           *int64  // override epoch ms timestamp (for deterministic testing)
}
```

## Field Types

Most fields use `interface{}` to allow `nil` → JSON `null`. Set them to the appropriate Go type:

| JSON type | Go type to use |
|---|---|
| string | `string` (empty string → `""`, not null) |
| int | `int`, `int64`, `uint64` |
| float | `float64` |
| bool | `bool` |
| null | `nil` |
| object | `map[string]interface{}` |
| array | `[]interface{}` |

## Sub-Structs

### LocaleConfig
```go
type LocaleConfig struct {
    RegionTimezone      string  // e.g. "+02:00"
    RegionLanguage      string  // e.g. "en"
    RegionCountry       string  // e.g. "US"
    TimezoneIdentifier  string  // e.g. "America/New_York"
    CalendarIdentifier  string  // e.g. "gregorian"
}
```

### DeviceConfig
```go
type DeviceConfig struct {
    AccessoriesCount     interface{}  // int
    DeviceName           string       // e.g. "iPhone"
    DeviceAdid           string       // IDFA (usually all-zeros)
    DeviceOrientation    interface{}  // string or int
    DeviceUdid           string       // IDFV
    IosDeviceName        string       // e.g. "iPhone16,2"
    IosVersion           string       // e.g. "18.0"
    PasteboardHash       string       // 64-char hex SHA-256
    ScreenBrightness     interface{}  // float64
    ScreenHeight         interface{}  // float64 (used for screen_size computation)
    ScreenWidth          interface{}  // float64
    IcloudUbiquityToken  interface{}  // string or nil
    IsScreenCaptured     interface{}  // bool
}
```

### HardwareConfig
```go
type HardwareConfig struct {
    BatteryLevel    interface{}  // int (0-100 or -1)
    BatteryCharging interface{}  // bool
    CpuType         string       // e.g. "ARM_64E"
    CpuCount        interface{}  // int
    IsEmulator      interface{}  // bool (always false in real SDK)
    PhysicalMemory  interface{}  // uint64 (bytes) — also used for hash formatting
    SystemUptime    interface{}  // int64 (seconds since boot)
}
```

### OSConfig
```go
type OSConfig struct {
    KernelVersion      string       // e.g. "Darwin Kernel Version 24.0.0"
    KernelName         string       // e.g. "Darwin"
    KernelArch         string       // e.g. "arm64" (overridden by cpu_type in final payload)
    IsJailbroken       interface{}  // bool
    IsIosAppOnMac      interface{}  // bool
    IsDebuggerAttached interface{}  // bool
}
```

### GeolocationConfigValues (defaults)
```go
type GeolocationConfigValues struct {
    GeolocationEnabled          bool   // default: false
    GeolocationServiceTimeoutMs int64  // default: 3000
    MaxGeoLocationCacheAgeSec   int64  // default: 60
    PrefetchEnabled             bool   // default: false
}
```

### ExperimentalConfig
```go
type ExperimentalConfig struct {
    ForceTouchSupport           interface{}  // int
    DisplayGamut                interface{}  // string (e.g. "P3")
    UserInterfaceStyle          interface{}  // string (e.g. "Light", "Dark")
    ThermalState                interface{}  // int (0-3)
    NativeBounds                interface{}  // map with native_width/height/origin_x/origin_y
    IsDisplayZoomEnabled        interface{}  // bool
    ConnectedAccessories        interface{}  // array
    AccessoryHash               interface{}  // string (64-char hex)
    IsPluggedIn                 interface{}  // bool
    IsArm64                     interface{}  // bool
    DateFormat                  string       // e.g. "MMM d, y" (localized medium-style date pattern)
    IsLowPowerEnabled           interface{}  // bool
    IsReactNative               interface{}  // bool
    IsRootFsCompromised         interface{}  // bool
    HasInvalidCodeSig           interface{}  // bool
    Pid                         interface{}  // int (actually parent PID via getppid())
    IsFlutter                   interface{}  // bool
    IsCellularDataRestricted    interface{}  // bool
    RadioTechnology             interface{}  // string (raw CTRadioAccessTechnology)
    VpnProtocol                 interface{}  // string
    ProxyType                   interface{}  // string
    ProxyAutoconfigSettings     interface{}  // map (only for PAC proxies)
    ApIsHotspot                 interface{}  // bool
    NwInterfaces                interface{}  // map {ifname: ip_address}
    DefGw                       interface{}  // string (default gateway IP)
    AvOutputInfo                interface{}  // map (audio output port info)
    SupportedBiometryType       interface{}  // string
    ExperimentalDeviceLocation  interface{}  // map (9 geo fields, only if geo enabled)
}
```

## Helper Functions

### FormatPhysicalMemory

```go
payload.FormatPhysicalMemory(bytes uint64) string
// 4294967296 → "4.00 GB"
// 6442450944 → "6.00 GB"
```

Uses 1024-based (GiB) calculation with 2 decimal places and commas normalized to periods. Matches `NSByteCountFormatter` with `UseGB + CountStyleMemory + zeroPadsFractionDigits`.

### ComputeScreenSize

```go
payload.ComputeScreenSize(width, height float64) uint64
// 390, 844 → 329160
```

Simple `width * height` as unsigned integer. Matches `getScreenSizeForDeviceHashFromWidth:andHeight:`.

### DefaultGeolocationConfig

```go
seonsdk.DefaultGeolocationConfig() *GeolocationConfigValues
// Returns {false, 3000, 60, false}
```

Default values verified from the SEONGeolocationConfig init at `0xa934` + constant table at `0x2D180`.

## Geolocation Behavior

The real iOS SDK handles geolocation in three scenarios. The Go library replicates all three:

### Scenario 1: Geo disabled (default)

```go
cfg := seonsdk.FingerprintConfig{
    // GeolocationConfig left nil → defaults applied (geolocationEnabled: false)
    // DeviceLocation left nil
}
```

Payload output:
```json
{
  "geolocation_config" : {
    "geolocationEnabled" : false,
    "geolocationServiceTimeoutMs" : 3000,
    "maxGeoLocationCacheAgeSec" : 60,
    "prefetchEnabled" : false
  },
  "device_location" : null,
  "location_timestamp" : 0
}
```

`experimental_device_location` is **absent** from `experimental_fields`.

### Scenario 2: Geo enabled and authorized

```go
cfg := seonsdk.FingerprintConfig{
    GeolocationConfig: &seonsdk.GeolocationConfigValues{
        GeolocationEnabled:          true,
        GeolocationServiceTimeoutMs: 3000,
        MaxGeoLocationCacheAgeSec:   60,
        PrefetchEnabled:             false,
    },
    DeviceLocation: &seonsdk.DeviceLocationData{
        Latitude:  37.7749,
        Longitude: -122.4194,
        Accuracy:  float64(10),
        // ... other fields
    },
    Experimental: seonsdk.ExperimentalConfig{
        ExperimentalDeviceLocation: map[string]interface{}{
            "altitude": float64(15),
            "floor_level": nil,
            // ... other 9 geo experimental fields
        },
    },
}
```

Payload output:
```json
{
  "geolocation_config" : { "geolocationEnabled" : true, ... },
  "device_location" : { "latitude" : 37.7749, "longitude" : -122.4194, ... },
  "location_timestamp" : 1713225600000,
  "experimental_fields" : {
    "experimental_device_location" : { "altitude" : 15, ... },
    ...
  }
}
```

### Scenario 3: Geo enabled but denied/restricted/not determined

In the real iOS SDK, when the user denies geolocation:
1. An error is recorded (code 1008/1009/1010) but the fingerprint is **NOT aborted**
2. The geo probe returns no data → `device_location: null`, `location_timestamp: 0`
3. `experimental_device_location` is NOT added to `experimental_fields`
4. All other probes still run normally
5. The encrypted payload is still generated and returned alongside the error

To replicate this in Go — enable geo in config but provide no location data:

```go
cfg := seonsdk.FingerprintConfig{
    GeolocationConfig: &seonsdk.GeolocationConfigValues{
        GeolocationEnabled: true,
        // ... rest of geo config
    },
    DeviceLocation: nil,  // no location data → mimics denied
    // Experimental.ExperimentalDeviceLocation left nil → absent
}
```

Payload output:
```json
{
  "geolocation_config" : { "geolocationEnabled" : true, ... },
  "device_location" : null,
  "location_timestamp" : 0
}
```

This exactly matches the real SDK behavior when geolocation is denied. Verified from decompilation of `getFingerprintBase64:` at `0x1b170`:
- Authorization status is checked via `getGeolocationAuthorizationStatusFrom:`
- On denial (status=3): error 1008 is stored, then `constructReport` is called anyway
- In `constructReport`: geo scan returns empty dict → `device_location = NSNull`, `location_timestamp = 0`

### Geolocation Error Codes (from the real SDK)

| Code | Condition | Message |
|---|---|---|
| 1008 | `CLAuthorizationStatusDenied` | "The user denied the use of location services for the app or they have disabled it globally in the Settings." |
| 1009 | `CLAuthorizationStatusNotDetermined` | "The user has not chosen whether the app can use location services." |
| 1010 | `CLAuthorizationStatusRestricted` | "The app is not authorized to use location services due to active restrictions." |
| 1011 | Location fetch completed but no location returned | "Failed to get location for device." |
| 1012 | Location fetch timed out | "The location service has timed out." |

### Geolocation `auth_status` Field Mapping

When location IS provided, the `auth_status` field in `device_location` uses SEON's custom mapping (NOT Apple's raw enum values):

| SEON `auth_status` | Meaning | Apple `CLAuthorizationStatus` |
|---:|---|---:|
| 0 | Authorized — When In Use | 4 (`kCLAuthorizationStatusAuthorizedWhenInUse`) |
| 1 | Authorized — Always | 3 (`kCLAuthorizationStatusAuthorizedAlways`) |
| 2 | Restricted | 1 (`kCLAuthorizationStatusRestricted`) |
| 3 | Denied | 2 (`kCLAuthorizationStatusDenied`) |
| -1 | Not Determined / Unknown | 0 (`kCLAuthorizationStatusNotDetermined`) |

### Geolocation `accuracy_auth_lvl` Field Mapping

| Value | Meaning |
|---:|---|
| 0 | Full accuracy |
| 1 | Reduced accuracy (iOS 14+ approximate location) |
| -1 | Unknown / no location manager |

## Performance Timing Simulation

On a real iPhone, SEON probe scanning takes 800–2500 ms (UIKit queries, sysctl, keychain, MFMailComposeViewController instantiation, etc). The Go SDK assembles its dict from pre-populated values in microseconds, so realistic iOS-like timings are simulated to produce fingerprints indistinguishable from real device payloads.

### Auto-generated ranges

| Field | Source | Range |
|---|---|---|
| `performance_actual_processing` | `simulateProbeProcessingMs() + network_time` | 850–2900 ms (typical 1100–2200 ms) |
| `performance_exec_time` | `actual + simulateSerializeOverheadMs()` | `actual + 5–20 ms` (always exceeds actual — matches real SDK where exec_time is captured after serialize/encrypt) |
| `performance_network_call` | real DC round-trip OR `simulateNetworkCallMs()` | 50–400 ms (10% chance of slower call up to 700 ms) |

### Behavior

- **When `SkipDC=false`** (default): the real DC round-trip time to `<sessionId>.seondnsresolve.com` is measured and used for `performance_network_call`.
- **When `SkipDC=true` or DC fails**: realistic simulated network time is used.
- **User overrides**: if `FingerprintConfig.PerformanceActualProcessing`, `PerformanceExecTime`, or `PerformanceNetworkCall` is non-empty, the user value is used verbatim.

### Implementation

See `timing.go` in the SDK root. The simulation functions:
- `simulateProbeProcessingMs()` — 800–2500 ms uniform
- `simulateNetworkCallMs()` — 50–400 ms base, with 10% probability of an additional 0–300 ms
- `simulateSerializeOverheadMs()` — 5–20 ms uniform
