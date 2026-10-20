# seonsdk-go

A Go reimplementation of the SEON iOS SDK (version 5.7.1). Produces encrypted fingerprint payloads that are wire-compatible with the real iOS SDK.

## Installation

```bash
go get status403.com/rsavv/seonsdk-go
```

## Quick Start

```go
package main

import (
    "fmt"
    seonsdk "status403.com/rsavv/seonsdk-go"
)

func main() {
    cfg := seonsdk.FingerprintConfig{
        SkipDC: true, // skip the DC side-channel for testing
        Device: seonsdk.DeviceConfig{
            DeviceName:    "iPhone",
            DeviceUdid:    "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
            IosDeviceName: "iPhone16,2",
            IosVersion:    "18.0",
            ScreenWidth:   float64(390),
            ScreenHeight:  float64(844),
        },
        Hardware: seonsdk.HardwareConfig{
            CpuType:        "ARM_64E",
            CpuCount:       6,
            PhysicalMemory: uint64(6442450944),
            IsEmulator:     false,
        },
        OS: seonsdk.OSConfig{
            KernelName:         "Darwin",
            KernelArch:         "arm64",
            KernelVersion:      "Darwin Kernel Version 24.0.0",
            IsJailbroken:       false,
            IsDebuggerAttached: false,
        },
    }

    wireString, err := seonsdk.Fingerprint(cfg)
    if err != nil {
        panic(err)
    }
    fmt.Println(wireString)
    // Output: iOS;<UUID>;<base64 IV>;<base64 ciphertext>
}
```

## Decrypting a Payload

```go
sessionID, payload, err := seonsdk.Decrypt(wireString)
if err != nil {
    panic(err)
}
fmt.Println("Session:", sessionID)
fmt.Println("Device Hash:", payload["device_hash"])
```

## CLI Decrypt Tool

```bash
go run ./cmd/seondecrypt "iOS;SESSION-ID;base64iv;base64ct"
```

## Quick Start with Device Database

Generate a full fingerprint from just a device model and iOS version:

```go
import "status403.com/rsavv/seonsdk-go/devices"

cfg, _ := devices.BuildPayload(devices.PayloadOptions{
    Machine:        "iPhone16,2",     // iPhone 15 Pro Max
    IOSVersion:     "18.0",
    UserDeviceName: "John's iPhone",
})

wireString, _ := seonsdk.Fingerprint(*cfg)
```

The builder auto-populates: hardware model name, pixel screen dimensions, RAM, CPU type, kernel version (with correct chip suffix), and realistic randomized values for brightness, battery, uptime, volume, and network interfaces.

## Proxy Support

```go
cfg := seonsdk.FingerprintConfig{
    Proxy: "http://user:pass@proxy.example.com:8080",
    // ...
}
```

## Geolocation Handling

The library replicates the real SDK's geolocation behavior:

- **Geo disabled** (default): `device_location: null`, `location_timestamp: 0`
- **Geo enabled + authorized**: provide `DeviceLocation` and `ExperimentalDeviceLocation`
- **Geo enabled + denied**: set `GeolocationConfig.GeolocationEnabled = true` but leave `DeviceLocation` nil — the SDK still generates the full fingerprint, just with `device_location: null`

See [Configuration Reference](config-reference.md#geolocation-behavior) for full details and code examples.

## Documentation

- [Architecture](architecture.md) — package structure, design decisions, data flow
- [Payload Schema](payload-schema.md) — complete JSON field inventory (67+ top-level keys)
- [Encryption Pipeline](encryption.md) — AES-256-CBC wire format, key, IDA verification
- [DC Side-Channel](dc-sidechannel.md) — seondnsresolve.com upload, headers, purpose
- [Configuration Reference](config-reference.md) — all FingerprintConfig fields, geolocation scenarios
- [Device Database](device-database.md) — platform names, specs, defaults builder, field name correction
- [Device API](device-api.md) — persistent `*Device`, `NewFingerprint()`, overrides
- [Behaviour Probe](behaviour.md) — `behaviour_results` simulator (sensor stats, at-ear FSM, attitude bucketing)
- [Location & Telephony](location-and-telephony.md) — `GenerateDeviceLocation`, `TelephonyForNetwork`
- [Locale coherence](locale.md) — `LocalePreset` enum + `ValidateLocale` for consistent region bundles
- [Device persistence](persistence.md) — `MarshalJSON` / `UnmarshalDevice` for bot-restart-safe accounts
- [SDK version bump](sdk-version.md) — updating `source` + `version_code` when Whatnot ships a new SeonSDK
- [iOS build number](ios-build-number.md) — why it's not captured and what you don't need to populate
- [Frida Capture](../frida/README.md) — real-device `behaviour_results` extraction via Frida Gadget
