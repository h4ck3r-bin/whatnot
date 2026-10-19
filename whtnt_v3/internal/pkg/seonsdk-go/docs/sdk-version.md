# Bumping the SDK version (source + version_code)

Two payload fields carry the SEON SDK version:

| Payload field | Format | IDA site | Computed from |
|---|---|---|---|
| `source` | `"ios-<bundleVersion>"` | `0x1afd8` (`-[SEONFingerprint getSDKVersion]`) | `stringWithFormat:@"ios-%@", self.bundleVersion` |
| `version_code` | `"01" + major%100 + minor%100 + patch%100`, zero-padded | `0x1cb64` (`-[SEONFingerprint versionCode]`) | `stringWithFormat:@"%@%02d%02d%02d", @"01", MAJ, MIN, PAT` |
| DC payload `source` | same as top-level `source` | derived | same |

The `"01"` prefix on `version_code` is a hardcoded platform constant (iOS) — it never changes regardless of SDK version.

## Default baseline

`version.DefaultBundleVersion = "5.7.1"` — the SEON SDK currently shipped inside Whatnot (verified from the framework's `Info.plist CFBundleShortVersionString`).

## Per-device override

Set `FingerprintConfig.SDKVersion` or route through the Device helpers:

```go
cfg.SDKVersion = "5.7.0"
// → source        = "ios-5.7.0"
// → version_code  = "01050700"
// → DC payload.source = "ios-5.7.0"
```

Empty string = use library default.

Keep the three in sync. Emitting `source="ios-5.7.1"` alongside `version_code="01060000"` is trivially detectable; the library does this automatically when you go through the override, but manual tinkering on individual BuildInput fields can drift.

## How to bump the default when Whatnot ships a new SDK

1. Diff the new `Info.plist`:
   ```sh
   plutil -p Payload/Whatnot.app/Frameworks/SeonSDK.framework/Info.plist | grep CFBundleShortVersionString
   ```
2. Update `version/version.go`:
   ```go
   const DefaultBundleVersion = "5.7.0"
   ```
3. Re-run the test suite — `version/version_test.go` pins the expected `source` + `version_code` for the current default, and will flag the change.
4. If SEON changed the format (`%@%02d%02d%02d` or the `"01"` prefix), diff the decompile of `-[SEONFingerprint versionCode]` (address has historically been stable near `0x1cb64` across versions, but verify).

## Helpers

| Function | Purpose |
|---|---|
| `version.SourceFor(bundleVersion)` | Compute the `source` string from any version. |
| `version.VersionCodeFor(bundleVersion)` | Compute the `version_code` string from any version. |
| `version.Source` / `version.VersionCode` | Package-level defaults (computed once from `DefaultBundleVersion`). |

## Why no auto-version-detection?

SEON doesn't expose its SDK version anywhere queryable; the only way to know Whatnot has upgraded is to diff a newly released IPA. There's nothing to auto-detect.
