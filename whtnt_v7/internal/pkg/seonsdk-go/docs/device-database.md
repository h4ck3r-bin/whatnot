# Device Database

## Overview

The `devices` package provides pre-computed device information that can be used to populate fingerprint fields without having a real iOS device. It includes:

1. **Platform Names** — 171-entry model→name lookup (extracted from the SeonSDK binary)
2. **Device Specs** — screen dimensions, RAM, CPU type per model
3. **Kernel Versions** — iOS version → Darwin kernel version mapping
4. **Defaults Builder** — auto-populate a config from just a model + iOS version

## Critical Field Name Correction

**Verified via IDA Pro at `0x1f778`–`0x1f950` (SEONDeviceProbe.scan disassembly):**

The `device_name` and `ios_device_name` fields have **SWAPPED semantics** from what the names suggest:

| JSON Key | iOS SDK Method | Actually Contains |
|---|---|---|
| `device_name` | `devicePlatform` (`0x1fbe0`) | **Hardware model name** (e.g. "iPhone 15 Pro Max") — from the `platformNames` dictionary lookup on `utsname.machine` |
| `ios_device_name` | `deviceName` (`0x1fc40`) | **User-chosen device name** (e.g. "John's iPhone") — from `UIDevice.currentDevice.name` |

This was verified by decompiling each block_invoke in the scan method and tracing which ObjC selector it calls:

```
Key "device_name"     → block at 0x1fbe0 → calls [self devicePlatform] → utsname.machine → platformNames[machine] → "iPhone 15 Pro Max"
Key "ios_device_name" → block at 0x1fc40 → calls [self deviceName]     → UIDevice.name → "John's iPhone"
```

The `platformNames` dictionary itself is an `NSConstantDictionary` at `0x43DA8` with 171 entries, dumped directly from the binary. See `devices/platforms.go` for the full listing.

## Complete SEONDeviceProbe.scan Key→Method Mapping

Verified from the disassembly at `0x1f760` (key CFString references paired with block_invoke function addresses):

| # | Key | Block | ObjC Method | Return Type |
|---:|---|---|---|---|
| 0 | `accessories_count` | `SEONDeviceProbe_bootstrap` | `accessories_count` | int |
| 1 | `device_name` | `0x1fbe0` | `devicePlatform` | string (hardware model) |
| 2 | `device_adid` | `0x1fbf8` | `deviceAdIdentifier` | string (IDFA UUID) |
| 3 | `device_orientation` | `0x1fc10` | `deviceOrientation` | string |
| 4 | `device_udid` | `0x1fc28` | `deviceIdentifier` | string (IDFV from keychain) |
| 5 | `ios_device_name` | `0x1fc40` | `deviceName` | string (user's chosen name) |
| 6 | `ios_version` | `0x1fc58` | `deviceVersion` | string (e.g. "18.0") |
| 7 | `pasteboard_hash` | `0x1fc70` | `pasteboard` | string (SHA-256) |
| 8 | `screen_brightness` | `0x1fc88` | `screenBrightness` | int (numberWithInteger) |
| 9 | `screen_height` | `0x1fcc8` | `getScreenHeight` | int (numberWithInt) |
| 10 | `screen_width` | `0x1fd08` | `getScreenWidth` | int (numberWithInt) |
| 11 | `icloud_ubiquity_token` | `0x1fd48` | `iCloudToken` | string or null |
| 12 | `is_screen_captured` | `0x1fd60` | `isScreenCaptured` | bool (numberWithBool) |

### Type details from block_invoke decompilations

- `screen_brightness` uses `numberWithInteger:` (NSInteger → int64), not float
- `screen_height` and `screen_width` use `numberWithInt:` (int32)
- `is_screen_captured` uses `numberWithBool:`
- `accessories_count` is computed during bootstrap, not scan

## Platform Names Dictionary

The full 171-entry `platformNames` dictionary was extracted from the `NSConstantDictionary` at `0x43DA8` in the SeonSDK binary. It maps `utsname.machine` identifiers (e.g. `"iPhone16,2"`) to human-readable names (e.g. `"iPhone 15 Pro Max"`).

The dictionary is stored in `devices/platforms.go` as `PlatformNames`.

Usage:
```go
import "status403.com/rsavv/seonsdk-go/devices"

name := devices.DevicePlatformName("iPhone16,2")
// → "iPhone 15 Pro Max"

name = devices.DevicePlatformName("UnknownModel")
// → "UnknownModel" (falls back to raw machine string)
```

## Device Specs

Screen dimensions, RAM, and CPU info for common iPhone models are in `devices/specs.go`.

```go
spec := devices.LookupSpec("iPhone16,2")
// spec.ScreenWidth = 430, spec.ScreenHeight = 932
// spec.PhysicalMemory = 8589934592 (8 GB)
// spec.CpuType = "ARM_64E", spec.CpuCount = 6
```

## Defaults Builder

Auto-populate device defaults from just a model identifier and iOS version:

```go
defaults := devices.BuildDefaults("iPhone16,2", "18.0")
// defaults.DeviceName = "iPhone 15 Pro Max"
// defaults.ScreenWidth = 430, defaults.ScreenHeight = 932
// defaults.PhysicalMemory = 8589934592
// defaults.CpuType = "ARM_64E"
// defaults.KernelVersion = "Darwin Kernel Version 24.0.0: ..."
// defaults.PhysicalMemoryFormatted = "8.00 GB"
// defaults.ScreenSize = 400760
// defaults.NativeBounds = {native_width: 1290, native_height: 2796, ...}
```

## Randomization Helpers

For fields that vary per-session:

```go
devices.RandomBrightness()    // 0.3 to 0.8
devices.RandomBatteryLevel()  // 20 to 100
devices.RandomSystemUptime()  // 1 hour to 7 days (seconds)
devices.RandomAudioVolume()   // 0 to 100
```

## Kernel Version Mapping

iOS version to Darwin kernel version strings are in `devices/kernels.go` (`KernelVersions` map).

Key format: the iOS version as reported by `UIDevice.systemVersion` (e.g. `"17.0"`, `"18.3.1"`). Betas and RCs report the same version number as the release, so all variants for a given version share one key. The value is an array of all known kernel strings for that version (betas, RCs, release).

The kernel version string format (verified from `sysctl({CTL_KERN=1, KERN_VERSION=65})` at `0x15c18`):
```
Darwin Kernel Version X.Y.Z: <date>; root:xnu-<build>~<variant>/RELEASE_ARM64_T<chip>
```

### Chip-aware kernel strings

The `RELEASE_ARM64_T<chip>` suffix varies per device hardware. The `devices/chips.go` file maps device models to chip identifiers:

| Chip | Apple Silicon | Example Devices |
|---|---|---|
| T8010 | A10 Fusion | iPhone 7 |
| T8015 | A11 Bionic | iPhone 8, X |
| T8020 | A12 Bionic | iPhone XS, XR |
| T8030 | A13 Bionic | iPhone 11 |
| T8101 | A14 Bionic | iPhone 12 |
| T8110 | A15 Bionic | iPhone 13, 14, SE3 |
| T8120 | A16 Bionic | iPhone 14 Pro, 15 |
| T8122 | A17 Pro | iPhone 15 Pro |
| T8130 | A18 | iPhone 16, 16 Plus |
| T8140 | A18 Pro | iPhone 16 Pro |
| T8150 | A19 Pro | iPhone 17 Pro |

`KernelVersionForDevice(iosVersion, machine)` picks the release kernel string and replaces the chip suffix to match the target device:

```go
kv := devices.KernelVersionForDevice("18.0", "iPhone16,2")
// → "Darwin Kernel Version 24.0.0: ... /RELEASE_ARM64_T8122"
//                                                     ^^^^^
//                                          A17 Pro for iPhone 15 Pro Max
```

### OS fields summary (verified via IDA)

| Field | Source | IDA Address | Value |
|---|---|---|---|
| `kernel_name` | `uname().sysname` | `0x15d58` | Always `"Darwin"` |
| `kernel_arch` | `uname().machine` | `0x15df0` | `"arm64"` (overridden by `cpu_type` in final payload) |
| `kernel_version` | `sysctl(CTL_KERN, KERN_VERSION)` | `0x15c18` | Full Darwin version string |

## Hardware Probe Type Verification (via IDA)

Each hardware probe field's NSNumber wrapper type was verified from the block_invoke decompilations:

| Field | Block | NSNumber Method | Go Type |
|---|---|---|---|
| `battery_level` | `SEONHardwareProbe_bootstrap` | `batteryLevel` → `numberWithInteger:` (from `floor(UIDevice.batteryLevel * 100)`) | `int` (0-100 or -1) |
| `battery_charging` | `SEONHardwareProbe_scan` block | `isBatteryCharging` → `numberWithBool:` | `bool` |
| `cpu_type` | `block_invoke` (0x1da60) | `cpuType` → returns `NSString` directly | `string` |
| `cpu_count` | `block_invoke_2` (0x1da78) | `numCores` → `numberWithInteger:` | `int` |
| `is_emulator` | `block_invoke_3` (0x1dab8) | `is_emulator` → `numberWithBool:` (always false) | `bool` |
| `physical_memory` | `block_invoke_4` (0x1daf8) | `physicalMemory` → **`numberWithUnsignedLongLong:`** | `uint64` (raw bytes) |
| `system_uptime` | `block_invoke_5` (0x1db38) | `systemUpTime` → `numberWithInteger:` | `int64` (seconds) |

Key finding: `physical_memory` uses `numberWithUnsignedLongLong:` with the raw byte count from `NSProcessInfo.physicalMemory`. For an 8 GB device: `8589934592` in the JSON. NOT a formatted string.

## Device Identifier Generation (IDA-verified)

The `devices` package provides generators for all three SEON device identifiers, each verified against the IDA disassembly.

### device_udid (IDFV)

Verified at `0x20b10` (`deviceIdentifier`):
- Reads keychain `"device_udid"` first (hashProvider=3)
- Falls back to `[UIDevice.currentDevice.identifierForVendor UUIDString]` (hashProvider=2)
- Caches the UUID in the keychain for persistence across app reinstalls
- Format: uppercase UUID string, e.g. `"A1B2C3D4-E5F6-7890-ABCD-EF1234567890"`

```go
udid := devices.RandomDeviceUdid()
// → "3F8A2C17-B4D9-4E61-8A5C-F2D0E1B97634"
```

### device_adid (IDFA)

Verified at `0x20c74` (`deviceAdIdentifier`):
- Calls `[ASIdentifierManager.sharedManager.advertisingIdentifier UUIDString]`
- Post-ATT (iOS 14.5+): returns `"00000000-0000-0000-0000-000000000000"` when tracking denied
- Pre-ATT or tracking granted: returns a real uppercase UUID
- Format: uppercase UUID string

```go
adid := devices.RandomDeviceAdid(false)  // post-ATT, tracking denied
// → "00000000-0000-0000-0000-000000000000"

adid = devices.RandomDeviceAdid(true)    // tracking granted
// → "7C91E4F2-3A8B-4D5E-9F12-6B0C8D3E7A45"
```

### icloud_ubiquity_token

Verified at `0x20d28` (`iCloudToken`):
- Reads `[NSFileManager.defaultManager ubiquityIdentityToken]`
- If nil/null/"(null)" → returns nil (user not signed into iCloud)
- Otherwise formats the token bytes as hex with `"%02.2hhx"`, inserting a space when `(i & 3) == 0 && i > 0`
- Strips `<` and `>` characters from the result
- Caches in keychain under `"icloud_token"` (hashProvider=0 fresh, 1 cached)
- Format: lowercase hex with spaces every 4 bytes, e.g. `"a1b2c3d4 e5f6a7b8 90abcdef 12345678 ..."`

```go
token := devices.RandomIcloudUbiquityToken()
// → "3f8a2c17 b4d94e61 8a5cf2d0 e1b97634 7c91e4f2 3a8b4d5e 9f126b0c 8d3e7a45"

// Or format custom bytes:
formatted := devices.FormatIcloudToken(myBytes)
```

### Device Hash Priority (verified at payload.ComputeDeviceHash)

The hash priority cascade uses these identifiers:
1. **iCloud token** (hashProvider=0): `SHA256(icloudUbiquityToken)` — most stable, survives app reinstall
2. **IDFV** (hashProvider=2): `SHA256(deviceUdid)` — stable per-vendor, resets on full uninstall
3. **Implicit hash** (hashProvider=4): `SHA256(JSON(20-field stable dict))` — fallback

## Default Payload Builder

`devices.BuildPayload()` generates a complete `FingerprintConfig` from just a model + iOS version:

```go
cfg, err := devices.BuildPayload(devices.PayloadOptions{
    Machine:        "iPhone16,2",
    IOSVersion:     "18.0",
    UserDeviceName: "John's iPhone",
    IcloudSignedIn: true,  // auto-generate iCloud token
})

wireString, err := seonsdk.Fingerprint(*cfg)
```

Auto-generates realistic values for all runtime-variable fields:

| Field | Generator | Range |
|---|---|---|
| `screen_brightness` | `RandomBrightness() * 100` | 30–80 (integer) |
| `battery_level` | `RandomBatteryLevel()` | 20–100 |
| `system_uptime` | `RandomSystemUptime()` | 3600–604800 (1h–7d in seconds) |
| `audio_volume_current` | `RandomAudioVolume()` | 0–100 |
| `pasteboard_hash` | `FormatPasteboardHash()` | random SHA-256 |
| `device_udid` | `RandomDeviceUdid()` | uppercase UUID (IDFV) |
| `device_adid` | `RandomDeviceAdid(tracking)` | all-zeros or uppercase UUID |
| `icloud_ubiquity_token` | `RandomIcloudUbiquityToken()` | hex with spaces every 4 bytes (if `IcloudSignedIn`) |
| `nw_interfaces` | random local IPs | `192.168.x.x` |
| `def_gw` | random gateway | `192.168.x.1` |

Fields NOT randomized (set to realistic defaults, all verified via IDA):

| Field | Default | IDA Source |
|---|---|---|
| `device_adid` | `"00000000-0000-0000-0000-000000000000"` | Post-ATT default (IDFA unavailable) |
| `device_orientation` | `"Portrait"` | `deviceOrientationWithStatusBarOrientation:` at `0x21230` — valid values: `"Portrait"`, `"Landscape Left"`, `"Landscape right"`, `"NOT FOUND"` |
| `is_emulator` | `false` | Always false (stub at `0x1e32c`) |
| `is_screen_captured` | `false` | `UIScreen.main.isCaptured` |
| `is_jailbroken` | `false` | Composite check |
| `is_debugger_attached` | `false` | sysctl P_TRACED |
| `is_ios_app_on_mac` | `false` | `NSProcessInfo.isiOSAppOnMac` |
| `can_send_mail` | `true` | `MFMailComposeViewController.canSendMail` |
| `can_send_text` | `true` | `MFMessageComposeViewController.canSendText` |
| `is_passcode_enabled` | `true` | LAContext policy 2 |
| `is_biometrics_enabled` | `true` | LAContext policy 1 |
| `force_touch_support` | `"Unavailable"` | `getForceTouchCapability` at `0x21e40` — valid: `"Unknown"`, `"Unavailable"`, `"Available"` (modern iPhones have no 3D Touch) |
| `display_gamut` | `"P3"` | `getCurrentDisplayGamut` at `0x21edc` — valid: `"Unspecified"`, `"sRGB"`, `"P3"` |
| `user_interface_style` | `"Light"` | `getUserInterfaceStyle` at `0x21f7c` — valid: `"Unspecified"`, `"Light"`, `"Dark"` |
| `thermal_state` | `0` | `getDeviceThermalState` at `0x20658` — raw int: 0=Nominal, 1=Fair, 2=Serious, 3=Critical |
| `preferred_content_size` | `"L"` | `getPreferredContentSize` at `0x6374` — **SEON uses short codes, NOT raw UIKit constants**. Valid: `"XS"`, `"S"`, `"M"`, `"L"`, `"XL"`, `"XXL"`, `"XXXL"`, `"A_M"`, `"A_L"`, `"A_XL"`, `"A_XXL"`, `"A_XXXL"`, `"Unspecified"`, `"Unknown"` |
| `mfi_hearing_aids_status` | `0` | `getPairedEar` at `0x6694` — mapping: 0=None, 1=Left, 2=Right, 3=Both, -1=Unknown |
| `supported_biometry_type` | `"FaceID"` | `getSupportedBiometryType` at `0x19c00` — valid: `"FaceID"`, `"TouchID"`, `"Unsupported"`, `"Unknown"`, `"Not Enrolled"`, `"Biometry N/A"`, `"Passcode not set"` |
| `pid` | `1` | `getppid()` — 1 = launchd (normal iOS app parent) |
| `date_format` | `"MMM d, y"` | `locale_date_format` at `0xa540` — NSDateFormatter medium-style for en_US locale |
| `network_config` | `"WIFI"` | `networkConfiguration` at `0xd028` — valid: `"WIFI"`, `"LTE"`, `"5G (NR)"`, `"None"`, etc. |
| `vpn_state` | `"NOT_CONNECTED"` | Verified strings at `0x2dc26` |
| `proxy_state` | `"NOT_CONNECTED"` | Verified strings at `0x2dc26` |
