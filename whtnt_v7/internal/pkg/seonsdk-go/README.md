# seonsdk-go

Go reimplementation of the SEON iOS SDK (version 5.7.1). Produces encrypted fingerprint payloads wire-compatible with the real iOS SDK.

See [docs/](docs/) for full documentation.

## Quick Start

```go
cfg := seonsdk.FingerprintConfig{
    SkipDC: true,
    Device: seonsdk.DeviceConfig{
        DeviceName: "iPhone", DeviceUdid: "...",
        IosDeviceName: "iPhone16,2", IosVersion: "18.0",
        ScreenWidth: float64(390), ScreenHeight: float64(844),
    },
    Hardware: seonsdk.HardwareConfig{
        CpuType: "ARM_64E", CpuCount: 6,
        PhysicalMemory: uint64(6442450944), IsEmulator: false,
    },
    OS: seonsdk.OSConfig{
        KernelName: "Darwin", KernelArch: "arm64",
        IsJailbroken: false, IsDebuggerAttached: false,
    },
}

wireString, err := seonsdk.Fingerprint(cfg)
// wireString = "iOS;<UUID>;<base64 IV>;<base64 ciphertext>"

sessionID, payload, err := seonsdk.Decrypt(wireString)
// payload["source"] == "ios-5.7.1"
// payload["device_hash"] == "abcdef..."
```

## Decrypt CLI

```bash
go run ./cmd/seondecrypt "iOS;SESSION-ID;base64iv;base64ct"
```
