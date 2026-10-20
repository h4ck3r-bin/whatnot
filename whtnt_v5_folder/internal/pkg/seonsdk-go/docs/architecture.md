# Architecture

## Package Structure

```
seonsdk-go/
├── seon.go              # Public API: Fingerprint(), FingerprintJSON(), Decrypt()
├── config.go            # FingerprintConfig and all sub-structs
├── seon_test.go         # Integration tests
│
├── crypto/              # Encryption pipeline
│   ├── constants.go     # AES-256 key, wire format constants
│   ├── encrypt.go       # AES-256-CBC encrypt + PKCS7 + gzip + base64 + wire format
│   ├── decrypt.go       # Reverse pipeline
│   └── hash.go          # SHA-256 hex hashing
│
├── dc/                  # Dynamic Component side-channel
│   └── client.go        # DCClient struct + HTTP POST to seondnsresolve.com via azuretls
│
├── payload/             # JSON payload assembly
│   ├── builder.go       # Assembles top-level dict, device hash, SET_KEYS
│   ├── json.go          # Custom sorted-key 2-space-indent JSON marshaler
│   └── keys.go          # All 52 SET_KEYS field name constants
│
├── devices/             # Device database and defaults
│   ├── platforms.go     # 171-entry model→name dict (from SeonSDK binary 0x43DA8)
│   ├── specs.go         # Screen pixels, RAM, CPU per model
│   ├── chips.go         # Model→chip suffix mapping for kernel strings
│   ├── kernels.go       # iOS version→kernel version strings (17.0–26.5, all betas)
│   ├── defaults.go      # BuildDefaults(machine, iosVersion)
│   └── builder.go       # BuildPayload() — full config from model+version
│
├── session/             # Session ID handling
│   └── session.go       # UUID generation, sanitization, DNS label extraction
│
├── version/             # SDK version constants
│   └── version.go       # "ios-5.7.1", "01050701"
│
└── cmd/seondecrypt/     # CLI tool
    └── main.go          # Decrypt and pretty-print a SEON wire-format string
```

## Design Decisions

### No "probe" abstraction

The real iOS SDK uses 11 ObjC probe classes (SEONDeviceProbe, SEONHardwareProbe, etc.) that read data from iOS APIs. Since this Go library runs on a server, not on iOS, all device data is provided as input via the `FingerprintConfig` struct. There's no need for a probe layer.

### Sorted-key JSON via custom marshaler

The iOS SDK uses `NSJSONWritingSortedKeys | NSJSONWritingPrettyPrinted` (options=3) for the main payload JSON. Go's `encoding/json` does NOT sort keys for `map[string]interface{}`. The `payload.MarshalSortedJSON()` function implements a custom recursive marshaler that:
- Sorts keys alphabetically at every nesting level
- Uses 2-space indent (matching modern iOS NSJSONSerialization)
- Uses ` : ` separator (space-colon-space, matching NSJSONSerialization)
- Outputs `null` for nil values (matching NSNull serialization)

### azuretls-client for HTTP

The DC side-channel uses `github.com/Noooste/azuretls-client` with the `azuretls.Ios` browser profile. This ensures the TLS fingerprint (JA3) matches a real iOS device. The `DCClient` struct wraps an `azuretls.Session` — created per `Fingerprint()` call, no cookie persistence. Proxy support via `cfg.Proxy`.

### interface{} for optional fields

Many payload fields can be null (NSNull in the iOS SDK). We use `interface{}` for these fields, where `nil` maps to JSON `null`. This matches the SDK's behavior where probes return NSNull on failure.

## Data Flow

```
FingerprintConfig
    │
    ▼
[Generate session ID if empty]
    │
    ▼
[Compute device_hash + implicit_device_hash]
    │  (priority: iCloud token → IDFV → SHA-256 of 20-field stable dict)
    │
    ▼
[Build probe maps from config fields]
    │
    ▼
[Build experimental_fields from config.Experimental]
    │
    ▼
[Fire DC side-channel POST to seondnsresolve.com]  (unless SkipDC)
    │
    ▼
[Assemble top-level dict via SET_KEYS + orchestrator fields]
    │
    ▼
[MarshalSortedJSON → sorted keys, 2-space indent, ` : ` separator]
    │
    ▼
[gzip compress]
    │
    ▼
[AES-256-CBC encrypt with PKCS7 padding]
    │  (hardcoded key, random 16-byte IV via crypto/rand)
    │
    ▼
[Format as "iOS;<sessionID>;<base64(IV)>;<base64(ciphertext)>"]
    │
    ▼
Return wire string
```
