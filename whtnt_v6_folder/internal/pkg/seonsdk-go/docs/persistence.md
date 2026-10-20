# Device persistence

A `*Device` is an in-memory simulation of a real iPhone's keychain + drift state. For long-lived bot accounts, you want that state to survive process restarts so the next fingerprint continues the same identity (`device_hash`, IDFV, iCloud token, `ReturningSession` semantics, behaviour seed, battery drift anchor) instead of looking like a fresh first-launch every time the bot restarts.

## API

```go
// Serialise
data, err := dev.MarshalJSON()
// → []byte — safe to store in a file, database, Redis, etc.

// Restore
restored, err := devices.UnmarshalDevice(data)
// DC calls allocate a fresh azuretls session per call, matching the real
// SDK's per-fingerprint NSURLSession lifecycle (IDA 0x7910/0x7940).
// If you need to attach a shared session (cookie jar, custom auth):
restored.SetHTTPClient(mySession)
```

`dev.MarshalJSON()` is safe to call concurrently with `NewFingerprint()` — it takes the same internal mutex.

## What's preserved

- **Identity** — Machine, IOSVersion, DeviceUdid, DeviceAdid, IcloudUbiquityToken, DeviceName, UserDeviceName.
- **Keychain-cached derivations** — `device_hash` is deterministic from these identity fields, so the restored device emits the same hash.
- **Per-device state** — Locale, dateFormat, accessibility flags, network-interface IP pair, preferred content size, supported biometry type.
- **Runtime knobs** — proxy, SkipDC, SimulateDCTimeout, DCAcceptLanguage, Behaviour / Location / Network options.
- **Drift anchors** — `createdAt` (used for battery + uptime elapsed calculations), `initialBattery`, `initialUptimeSec`, `pluggedIn`.
- **`GenerationCount`** — preserved verbatim, so the first post-restore fingerprint correctly reports `ReturningSession = true` (→ hashProvider 1/3 cached, not 0/2 fresh).

## What's NOT preserved

- `httpClient` (azuretls session) — not JSON-serialisable. Restored devices start with a nil session, and `SendDCRequest` builds a fresh one per call (matching the real SDK). Use `SetHTTPClient` only if you explicitly want connection reuse.
- Internal sync.Mutex — zero-value is valid, reconstruction is implicit.

## Typical bot layout

```go
// Account creation:
dev, _ := devices.NewDevice(devices.DeviceOptions{/* ... */})
state, _ := dev.MarshalJSON()
db.SaveDeviceState(accountID, state)

// Every subsequent action on that account:
state, _ := db.LoadDeviceState(accountID)
dev, _ := devices.UnmarshalDevice(state)
cfg := dev.NewFingerprint()
wire, _ := seonsdk.Fingerprint(*cfg)
// ... (optional) re-save if you want to persist the incremented GenerationCount:
newState, _ := dev.MarshalJSON()
db.SaveDeviceState(accountID, newState)
```

## Versioning

The persisted JSON carries a `version` field. `UnmarshalDevice` refuses to read a future version — so you can bump the layout safely without silently producing garbage fingerprints from old persisted state. If you need to migrate across a version bump, add a migration helper (or just regenerate the device state — it's cheap).

Current version: `1`.

## Cross-machine portability

The serialised state is pure JSON; it's portable across OS, architecture, and Go versions. Two caveats:

1. **Device catalog must match.** If the persisted `machine` string (e.g. `"iPhone18,4"`) isn't in your running binary's `devices/specs.go`, unmarshal errors. Bump the seonsdk-go version if Apple releases new hardware.
2. **`createdAt` matters.** Battery drift is computed from `time.Since(createdAt)`. If you persist a device, wait a month, then restore, the battery level will reflect a month of drift. If that's not what you want, re-roll by calling `NewDevice` instead.
