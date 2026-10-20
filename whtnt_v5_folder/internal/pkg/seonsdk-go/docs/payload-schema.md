# Payload Schema

## Overview

The decrypted SEON fingerprint payload is a JSON document with **69 top-level keys** on a standard fingerprint, an `experimental_fields` sub-dict (**25 keys** in the iPhone 14 Pro capture baseline, up to ~35 depending on conditional emissions), and optional `behaviour_results` (29 keys when behaviour monitoring was active) and `device_location` (9 keys when geolocation is enabled) sub-dicts.

Key counts cross-referenced against a real-device capture (iPhone 14 Pro, iOS 18.5, Polish locale) in `testdata/real_iphone14pro_18.5_PL.json`. Every structural finding below is verified against that capture.

All keys are sorted alphabetically in the JSON output (matching `NSJSONWritingSortedKeys`). Apple-specific byte behaviours we reproduce:

- **Indent**: 2 spaces per depth level.
- **Key separator**: `" : "` — space before AND after the colon (Apple's pretty-printed format, not Go's `": "`).
- **Forward slash escaping**: `/` in string values is emitted as `\/` (e.g., `"Europe\/Warsaw"`). Apple's NSJSONSerialization always escapes forward slashes; Go's `encoding/json` never does. See `payload/json.go:appleEscapeSlash`.
- **No HTML escape**: `<`, `>`, `&` kept literal (NSJSONSerialization does not HTML-escape).
- **Numbers**: NSNumber wrapping follows the underlying storage — `numberWithBool` → `true`/`false`; `numberWithInteger`/`numberWithUnsignedLongLong` → bare integers; floats through shortest-round-trip.
- **NSNull**: serialised as JSON `null`. Missing key ≠ null key — both cases exist in the payload.

## Payload Assembly Order (verified at `0x1b8ec` constructReport)

The payload is assembled in this order across 4 dispatch blocks:

**Main block (`0x1b8ec`):**
1. Run all 9 probes via `scanWithPerformanceTracking` (device, hardware, locale, media, network, os, telephony, accessibility, auth)
2. Optionally run geolocation and behaviour probes
3. `geolocation_config` = geoconfig.toDictionary
4. `experimental_fields` = merge all probes' getExperimentalFields (via `countByEnumeratingWithState:`) + `current_hash_provider` (numberWithUnsignedChar) + `implicit_device_hash` **only when freshly computed** (absent on returning sessions where device_hash comes from the keychain cache — verified at `0x1bc94`, capture showed `current_hash_provider: 1` with no `implicit_device_hash` key)
5. `source` = `"ios-" + bundleVersion`
6. `session_id` = sessionId (or NSNull if nil)
7. `device_hash` = computed via cascade (keychain-cached or freshly computed)
8. `setProbePerformances:` adds **`performance_probes`** — 10-probe × 3-phase (init/scan/experimental) dict, each phase as `"%.3f"` ms string or `null` when the phase didn't run (verified `0x1c8d0`, real capture emits this unconditionally)
9. `SET_KEYS` for each probe in order: DEVICE, HARDWARE, LOCALE, MEDIA, NETWORK, OS, TELEPHONY, ACCESSIBILITY, AUTH
10. `device_location` / `location_timestamp` (timestamp = `-1` on empty geo per `0x1c07c`)
11. `behaviour_results` (`null` when behaviour monitoring was not started)
12. `kernel_arch` = `cpu_type` (override if both present)
13. `version_code`
14. Fire DC request to `<sessionId>.seondnsresolve.com`

**DC response block (`0x1c450`):**
14. `performance_network_call` = DC timing
15. `dynamic_evaluator_results` = DC response (if non-nil)

**After-DC block (`0x1c65c`):**
16. `stored_errors` = error log
17. `port_scan_result` = port scan results

**Finalize block (`0x1c78c`):**
18. `timestamp` = epoch ms (numberWithInteger)
19. `performance_actual_processing` = total timing

**Serialize block (`0x1b46c`):**
20. `performance_exec_time` = overall timing
21. JSON serialize with options=3 (NSJSONWritingPrettyPrinted | NSJSONWritingSortedKeys)
22. AES-256-CBC encrypt → wire format

## Top-Level Keys (67)

### From SET_KEYS (52 keys, copied from probe scan dicts)

#### KEYS_LOCALE (5)
| Key | Type | Source |
|---|---|---|
| `region_timezone` | string | NSDateFormatter ZZZZ format, GMT stripped (e.g. `"+02:00"`) |
| `region_language` | string | First component of `NSLocale.preferredLanguages[0]` split by `-_` |
| `region_country` | string | `NSLocale.currentLocale[NSLocaleCountryCode]` |
| `timezone_identifier` | string | `NSTimeZone.localTimeZone.name` (e.g. `"America/New_York"`) |
| `calendar_identifier` | string | `NSCalendar.currentCalendar.calendarIdentifier` |

#### KEYS_DEVICE (13)
| Key | Type | Source |
|---|---|---|
| `accessories_count` | int | Count of EAAccessoryManager connected accessories |
| `device_name` | string | **Hardware model name** from `devicePlatform` → `platformNames[utsname.machine]` (e.g. `"iPhone 15 Pro Max"`) — NOT the user's name |
| `device_adid` | string | IDFA from ASIdentifierManager (usually all-zeros post-iOS 14.5) |
| `device_orientation` | string/int | UIDevice orientation |
| `device_udid` | string | IDFV (UIDevice.identifierForVendor), cached in keychain |
| `ios_device_name` | string | **User-chosen device name** from `UIDevice.currentDevice.name` (e.g. `"John's iPhone"`) — NOT the hardware model |
| `ios_version` | string | `UIDevice.systemVersion` (e.g. `"18.0"`) |
| `pasteboard_hash` | string | SHA-256 of detected pasteboard patterns + changeCount |
| `screen_brightness` | int | `floor(UIScreen.brightness * 100)` (integer 0-100, NOT a float) |
| `screen_height` | int | **Pixel height** = `UIScreen.bounds.height * UIScreen.scale` (truncated to int32) — e.g. 2796 for iPhone 15 Pro Max |
| `screen_width` | int | **Pixel width** = `UIScreen.bounds.width * UIScreen.scale` (truncated to int32) — e.g. 1290 for iPhone 15 Pro Max |
| `icloud_ubiquity_token` | string/null | NSFileManager.ubiquityIdentityToken → hex bytes with spaces every 4 bytes (e.g. `"a1b2c3d4 e5f6a7b8 ..."`), `<>` stripped. Null if not signed into iCloud. Cached in keychain as `"icloud_token"`. |
| `is_screen_captured` | bool | `UIScreen.main.isCaptured` |

#### KEYS_HARDWARE (7)
| Key | Type | Source |
|---|---|---|
| `battery_level` | int | `floor(UIDevice.batteryLevel * 100)` → `numberWithInteger:` (0-100, or -1) |
| `battery_charging` | bool | `UIDevice.batteryState == 2` (Charging only — NOT Full) → `numberWithBool:`. Verified at `0x1e018`. |
| `cpu_type` | string | `hw.cputype` + `hw.cpusubtype` → `"ARM_64E"`, `"ARM_64V8"`, etc. (NSString, not wrapped) |
| `cpu_count` | int | `sysctlbyname("hw.ncpu")` → `numberWithInteger:` |
| `is_emulator` | bool | **Always false** (stub in the real SDK) → `numberWithBool:` |
| `physical_memory` | uint64 | `NSProcessInfo.physicalMemory` → **`numberWithUnsignedLongLong:`** (raw byte count, e.g. `8589934592` for 8 GB) |
| `system_uptime` | int | `time(NULL) - sysctl(KERN_BOOTTIME)` → `numberWithInteger:` (seconds since boot) |

#### KEYS_MEDIA (4)
| Key | Type | Source |
|---|---|---|
| `audio_mute_status` | bool | **Always `false` on real devices.** Backed by readonly ivar `_audioMuteStatus` which nothing writes to. `SEONSharkfoodMuteSwitchDetector` class exists but `+shared` is never called — the detector is dead code. Verified at `0x1f000`. |
| `audio_volume_current` | int | `AVAudioSession.outputVolume * 100` (0-100) |
| `can_send_mail` | bool | `MFMailComposeViewController.canSendMail` |
| `can_send_text` | bool | `MFMessageComposeViewController.canSendText` |

#### KEYS_NETWORK (5)
| Key | Type | Source |
|---|---|---|
| `vpn_state` | string | VPN detection result |
| `proxy_address` | string/null | `"host:port"` if proxy configured |
| `proxy_state` | string | `"UNKNOWN"`, `"NOT_CONNECTED"`, or `"CONNECTED"` |
| `wifi_ssid` | string/null | `CNCopyCurrentNetworkInfo[SSID]` |
| `wifi_mac_address` | string/null | AP BSSID (misnomer — NOT the device MAC) |

#### KEYS_OS (6)
| Key | Type | Source |
|---|---|---|
| `kernel_version` | string | `sysctl(CTL_KERN, KERN_VERSION)` |
| `kernel_name` | string | `uname.sysname` (e.g. `"Darwin"`) |
| `kernel_arch` | string | **Overridden by `cpu_type`** at assembly time |
| `is_jailbroken` | bool | Composite: path check + fork + DYLD + write test |
| `is_ios_app_on_mac` | bool | `NSProcessInfo.isiOSAppOnMac` |
| `is_debugger_attached` | bool | `sysctl(KERN_PROC, KERN_PROC_PID)` → P_TRACED flag |

#### KEYS_TELEPHONY (2)
| Key | Type | Source |
|---|---|---|
| `network_config` | string | `"WIFI"`, `"LTE"`, `"5G (NR)"`, `"None"`, etc. |
| `is_on_call` | bool | CallKit active call check |

#### KEYS_ACCESSIBILITY (8)
| Key | Type | Source |
|---|---|---|
| `is_bold_text_enabled` | bool | UIAccessibility |
| `is_reduce_motion_enabled` | bool | UIAccessibility |
| `is_video_autoplay_enabled` | bool | UIAccessibility |
| `is_assisstive_touch_running` | bool | UIAccessibility (note: typo in key name is intentional — matches SDK) |
| `is_reduce_transparency_enabled` | bool | UIAccessibility |
| `is_darker_system_colors_enabled` | bool | UIAccessibility |
| `preferred_content_size` | string | SEON short code mapped from UIContentSizeCategory (e.g. `"L"`, `"XL"`, `"A_M"`) — NOT the raw UIKit constant |
| `mfi_hearing_aids_status` | int | MFi hearing aid pairing status |

#### KEYS_AUTH (2)
| Key | Type | Source |
|---|---|---|
| `is_passcode_enabled` | bool | `LAContext.canEvaluatePolicy:DeviceOwnerAuthentication` |
| `is_biometrics_enabled` | bool | `LAContext.canEvaluatePolicy:DeviceOwnerAuthenticationWithBiometrics` |

### Orchestrator-Added Keys (15)

| Key | Type | Source |
|---|---|---|
| `geolocation_config` | object | `{geolocationEnabled, geolocationServiceTimeoutMs, maxGeoLocationCacheAgeSec, prefetchEnabled}` |
| `experimental_fields` | object | Merged from all probes' `getExperimentalFields` + `current_hash_provider` + `implicit_device_hash` |
| `source` | string | Always `"ios-5.7.1"` |
| `session_id` | string | UUID generated per fetch |
| `device_hash` | string | 64-char lowercase hex SHA-256 |
| `device_location` | object/null | Geo scan result (9 keys) or null |
| `location_timestamp` | int | Epoch ms from geo timestamp, or 0 |
| `behaviour_results` | object/null | Behaviour stats (29 keys) or null |
| `version_code` | string | Always `"01050701"` |
| `stored_errors` | array or object | Real SDK type: `NSMutableDictionary` copy from `-[SEONErrorLogger getErrorLog]` at `0xa0ac`. Empty state on a real device is `{}` (object), with keys `"<class>_<method>:<message>"` (format at `0x9e98`) and counts as values. This SDK **defaults to `[]`** for backward compatibility — override via `FingerprintConfig.StoredErrors` to send a real-SDK-style dict. Helper `seonsdk.BuildDCErrorLog(message)` produces the entry the real SDK emits when the DC call fails (timeout: `"SEONNetworkProbe_sendDynamicComponentRequest:dcRequestCompletionBlock::The request timed out."`). |
| `port_scan_result` | array | Array of category indices `[0,1,5,6,7,8]` |
| `dynamic_evaluator_results` | string | DC response `data` field (if present) |
| `timestamp` | int | Current epoch ms |
| `performance_actual_processing` | string | `"X.XXX ms"` — simulated iOS probe scan (800–2500 ms) + network time. Can be overridden via `FingerprintConfig.PerformanceActualProcessing`. |
| `performance_exec_time` | string | `"X.XXX ms"` — always slightly higher than `performance_actual_processing` (adds 5–20 ms for JSON sort + AES + gzip, matching the real SDK where exec_time is captured after serialize). |
| `performance_network_call` | string | `"X.XXX ms"` — real DC round-trip time when DC runs; simulated 50–400 ms (10% chance up to 700 ms) when `SkipDC=true` or DC fails. |

## `experimental_fields` Sub-Dict

| Key | Source Probe | Type |
|---|---|---|
| `force_touch_support` | Device | string (`"Unknown"`, `"Unavailable"`, `"Available"`) |
| `display_gamut` | Device | string |
| `user_interface_style` | Device | string |
| `thermal_state` | Device | int |
| `native_bounds` | Device | object `{native_width, native_height, native_origin_x, native_origin_y}` |
| `is_display_zoom_enabled` | Device | bool |
| `connected_accessories` | Device | array |
| `accessory_hash` | Device | string (SHA-256, only if accessory detected) |
| `is_plugged_in` | Hardware | bool |
| `is_arm64` | Hardware | bool |
| `date_format` | Locale | string (e.g. `"MMM d, y"`) |
| `is_low_power_enabled` | OS | bool |
| `is_react_native` | OS | bool (`NSClassFromString("RCTBridge") != nil`) |
| `is_root_fs_compromised` | OS | bool (root FS writability check) |
| `has_invalid_code_sig` | OS | bool (`fcntl(F_ADDSIGS=59, NULL)` — always false due to SEON bug) |
| `pid` | OS | int (actually `getppid()` — parent PID) |
| `is_flutter` | OS | bool (`NSClassFromString("FlutterEngine")`) |
| `is_cellular_data_restricted` | Telephony | bool |
| `radio_technology` | Telephony | string (raw CTRadioAccessTechnology) |
| `vpn_protocol` | Network | string |
| `proxy_type` | Network | string |
| `proxy_autoconfig_settings` | Network | object (only if PAC configured) |
| `ap_is_hotspot` | Network | bool |
| `nw_interfaces` | Network | object `{ifname: ip}` |
| `def_gw` | Network | string (default gateway IP) |
| `av_output_info` | Media | object (audio output port info) |
| `supported_biometry_type` | Auth | string (`"FaceID"`, `"TouchID"`, `"Unsupported"`, etc.) |
| `current_hash_provider` | Synthesized | int (0-4) |
| `implicit_device_hash` | Synthesized | string (64-char hex) |
| `experimental_device_location` | Geo | object (9 fields, only if geo enabled) |

## `device_location` Sub-Dict (9 keys)

| Key | Type |
|---|---|
| `latitude` | float |
| `longitude` | float |
| `accuracy` | float |
| `accuracy_auth_lvl` | int (0=full, 1=reduced, -1=unknown) |
| `status` | int (0=ok, 1=error, 2=timeout) |
| `auth_status` | int (0=whenInUse, 1=always, 2=restricted, 3=denied, -1=notDetermined) |
| `is_simulated` | bool |
| `is_cached` | bool |
| `timestamp` | int (epoch ms) |

## `behaviour_results` Sub-Dict (29 keys)

4 channels x 6 stats = 24 numeric fields:
- Channels: `user_acc`, `rot_rate`, `gravity`, `mag_field`
- Stats per channel: `min_<ch>`, `max_<ch>`, `mean_<ch>`, `median_<ch>`, `var_<ch>`, `p95_<ch>`

Plus 5 metadata fields:
| Key | Type |
|---|---|
| `mag_declination` | float |
| `heading_changes` | object (time-keyed dict) |
| `orientation_changes` | object (time-keyed dict) |
| `monitoring_duration` | int (milliseconds) |
| `most_freq_attitude` | object `{w, x, y, z}` quaternion |

## Device Hash Computation (verified at `0x1a8cc` and `0x1ae54`)

Priority cascade (from `computeHashFor_20_field_stable_dict`):
1. `icloud_ubiquity_token` is not null/NSNull → `device_hash = SHA-256(token_hex_string)`
2. `device_udid` is not null/NSNull → `device_hash = SHA-256(idfv_string)`
3. Both unavailable → `device_hash = implicit_device_hash`, `setCurrentDeviceHashProvider(4)`

### `current_hash_provider` values (verified via IDA)

| Value | Set At | Meaning |
|---|---|---|
| 0 | `iCloudToken` `0x21074` | Fresh iCloud token (just read from NSFileManager) |
| 1 | `iCloudToken` `0x20f9c` | Cached iCloud token (read from keychain) |
| 2 | `deviceIdentifier` `0x20c0c` | Fresh IDFV (just read from UIDevice) |
| 3 | `deviceIdentifier` `0x20b5c` | Cached IDFV (read from keychain) |
| 4 | `computeHash` `0x1af64` | Implicit hash fallback (neither token nor UDID available) |

Providers 0-3 are set during probe scanning (before hash computation). Provider 4 is the only one set in the hash computation itself.

### 20-field stable dict (keys verified at `0x1a8cc`)

`device_name`, `ios_device_name`, `cpu_type`, `cpu_count`, `user_interface_style`, `is_biometrics_enabled`, `date_format`, `physical_memory` (formatted as `"X.XX GB"` via `getPhysicalMemoryString:`), `region_language`, `region_country`, `calendar_identifier`, `can_send_mail`, `can_send_text`, `is_darker_system_colors_enabled`, `is_bold_text_enabled`, `is_reduce_motion_enabled`, `is_video_autoplay_enabled`, `is_reduce_transparency_enabled`, `preferred_content_size`, `screen_size` (= `width * height` as integer via `getScreenSizeForDeviceHashFromWidth:andHeight:`).

## Port Scan Categories

| Category | Ports | Target |
|---:|---|---|
| 0 | 7070 | Charles Proxy |
| 1 | 57531, 5938 | TeamViewer |
| 5 | 9100 | Instrumentation |
| 6 | 22 | SSH (jailbreak) |
| 7 | 44 | (uncommon) |
| 8 | 27042 | Frida-server |

All scanned on `127.0.0.1` only. Result is an array of category indices that had at least one open port.

## Real-Device Findings (iPhone 14 Pro / iOS 18.5 / Polish locale)

All findings below are pinned against a captured wire payload decrypted into `testdata/real_iphone14pro_18.5_PL.json`.

### Fields with non-obvious semantics

| Field | Type in payload | Notes |
|---|---|---|
| `kernel_version` | string | iOS build number from `sysctl kern.osversion` — e.g., `"22F76"`. NOT the full Darwin kernel version. See `docs/ios-build-number.md`. |
| `physical_memory` | int64 | Real `hw.memsize` value, NOT nominal `N × 1024³`. iPhone 14 Pro reports `5,940,199,424` for its 6 GB class. See `memsize{3,4,6,8,12}GB` constants in `devices/specs.go`. |
| `location_timestamp` | int64 | `-1` when geo scan is empty or geolocation disabled (via `NSConstantIntegerNumber(-1)` at `off_43D90`). `0` or the scan's timestamp otherwise. |
| `icloud_ubiquity_token` | string | 20 bytes as lowercase hex in 5 space-separated groups of 8: `"XXXXXXXX XXXXXXXX XXXXXXXX XXXXXXXX XXXXXXXX"`. Verified: `"94051589 5b5e1c27 79c2ca45 74bae595 b8fff830"`. |
| `timezone_identifier` | string | `/` escaped as `\/` per NSJSONSerialization rules. Stored as `"Europe/Warsaw"` in Go, serialised as `"Europe\/Warsaw"` on the wire. |
| `performance_*` | string | `"%.3f"` formatted ms, NO trailing ` ms` unit. From `+[SEONUtil formatMilliseconds:]` at `0x5b6c`. |

### `experimental_fields` always-emitted keys (`0x1cd44` merge)

The Network / Telephony / Media / Device / OS / LocalAuth / Geolocation probes each expose a `getExperimentalFields` dict that is merged into `experimental_fields`. Every getSafe-wrapped call returns either the value or `NSNull` (→ JSON `null`), so the key is always present when the probe's experimental phase ran.

Fields that are ALWAYS emitted on a standard fingerprint (25 keys, in alphabetical order — matches real capture):

`ap_is_hotspot, av_output_info, connected_accessories, current_hash_provider, date_format, def_gw, display_gamut, force_touch_support, has_invalid_code_sig, is_arm64, is_cellular_data_restricted, is_display_zoom_enabled, is_flutter, is_low_power_enabled, is_plugged_in, is_react_native, is_root_fs_compromised, native_bounds, nw_interfaces, pid, proxy_type, radio_technology, thermal_state, user_interface_style, vpn_protocol`

### `experimental_fields` conditional keys

| Key | Condition | Source |
|---|---|---|
| `accessory_hash` | `self->accessoryHash` set | DeviceProbe `0x2004c` |
| `experimental_device_location` | geolocation enabled | constructReport `0x1bd08` |
| `implicit_device_hash` | hash freshly computed (provider ∈ 0/2/4) | constructReport `0x1bc94` |
| `proxy_autoconfig_settings` | `proxy_type == kCFProxyTypeAutoConfigurationURL` | NetworkProbe `0x6ca8` |
| `supported_biometry_type` | auth probe's experimental phase ran | `0x197c0` — absent in our capture where `performance_probes["8"]["2"] == null` |

### `av_output_info` shape + localisation

`-[SEONMediaProbe getCurrentAvOuputInfo]` wraps `AVAudioSession.sharedInstance.currentRoute.outputs` and emits one port-dict per output. Built-in speaker produces:

```json
{
  "channels": [
    {"channelLabel": 4294967295, "channelName": "<Speaker> 1", "channelNumber": 1, "owningPortUID": "Speaker"},
    {"channelLabel": 4294967295, "channelName": "<Speaker> 2", "channelNumber": 2, "owningPortUID": "Speaker"}
  ],
  "dataSources": [],
  "portName": "<Speaker>",
  "portType": "Speaker",
  "portUID": "Speaker"
}
```

`channelLabel = 4294967295` = `kAudioChannelLabel_Unknown` (`UInt32 -1`). `portType` and `portUID` are Core Audio constants — the literal string `"Speaker"` on every locale. Only `channelName` and `portName` are localised by iOS:

| Locale | `portName` / `channelName` root |
|---|---|
| en-* | `Speaker` |
| pl-PL | `Głośnik` *(verified via capture)* |
| de-DE | `Lautsprecher` |
| fr-FR | `Haut-parleur` |
| es-ES | `Altavoz` |
| it-IT | `Altoparlante` |
| nl-NL | `Luidspreker` |
| ja-JP | `スピーカー` |
| ko-KR | `스피커` |

Suffix pattern: `<name> 1`, `<name> 2` with a single space before the digit.

Settable via `LocaleConfig.SpeakerName` (presets populate automatically; custom locales must supply explicitly or the English fallback will betray a non-English `region_country`).

### `performance_probes` shape

Emitted at `0x1c8d0` via `setProbePerformances:`. Structure:

```json
"performance_probes": {
  "0": {"0": "<init>", "1": "<scan>", "2": "<experimental>"}, // device
  "1": {...},  // hardware
  "2": {...},  // locale
  "3": {...},  // media
  "4": {...},  // network
  "5": {...},  // os
  "6": {...},  // telephony
  "7": {...},  // accessibility  — "2" (experimental) is always null (getExperimentalFields returns nil at 0x609c)
  "8": {...},  // authentication  — "2" often null
  "9": {...}   // geolocation    — "1" and "2" null when geo disabled
}
```

Each phase is a `"%.3f"`-formatted millisecond string (`+[SEONUtil formatMilliseconds:]`) or JSON `null` when the phase never ran (backing NSDate offset is negative, per `0xb270`/`0xb2cc`).

### `dynamic_evaluator_results` — requires a live DC call

Top-level field, present ONLY when a DC request actually ran and returned
a non-empty `jsonResponse` body. Populated at IDA `0x1c4c0` inside the DC
response block:

```
[dst setObject:response.jsonResponse forKey:@"dynamic_evaluator_results"]
```

The value is a second wire-format string (`iOS;<sid>;<iv>;<ct>`), i.e., an
encrypted payload SEON's backend returns to its own SDK — a nested
encrypted blob that the SDK does not decrypt locally, just forwards.

Behaviour in our SDK:

| Setting | Emits `dynamic_evaluator_results`? |
|---|---|
| `SkipDC: false` + DC call succeeds | Yes — forwarded from SEON's response |
| `SkipDC: false` + DC call fails (timeout / 5xx) | No |
| `SkipDC: true` + `SimulateDCTimeout: true` | No — mimics "DC always times out" pattern |
| `SkipDC: true` + `SimulateDCTimeout: false` | No |

A fingerprint emitted with `SkipDC: true` on a Whatnot-like flow is
structurally distinguishable from a real iPhone's fingerprint: the real
device never reaches that code path because DC always runs. If SEON's
scoring cross-references `dynamic_evaluator_results` presence against the
`performance_network_call` value (>1500 ms implies a timeout, which should
suppress the field), `SkipDC: true` becomes a tell. Running with
`SkipDC: false` is structurally correct but requires SEON's DC endpoint
to be reachable and accepting your encrypted blob.

### Byte-level Apple quirks we reproduce

| Quirk | Where |
|---|---|
| `" : "` not `": "` | `payload/json.go:writeMap` |
| `\/` escape on `/` | `payload/json.go:appleEscapeSlash` |
| No HTML escape on `<>&` | `json.Encoder.SetEscapeHTML(false)` |
| Empty NSDict → `{\n\n}` with blank line | `payload/json.go:writeMap` |
| Empty NSArray → `[\n\n]` with blank line | `payload/json.go:writeArray` |
| gzip MTIME=0, OS=3 (UNIX) | `crypto/encrypt.go:GzipCompress` |
| AES-256-CBC + PKCS7, 16-byte random IV | `crypto/encrypt.go:Encrypt` |
