# iOS version and build number

**Summary: SEON emits BOTH the marketing iOS version AND the iOS build number. The build number goes into `kernel_version`, not a separate field.**

## Two version strings, two payload slots

An iPhone running iOS 18.5 exposes two version identifiers to apps:

| Source | Example | Apple API | Payload field |
|---|---|---|---|
| Marketing version | `"18.5"` | `UIDevice.current.systemVersion` | `ios_version` |
| Build number | `"22F76"` | `sysctl kern.osversion` | `kernel_version` |

Both are captured by SEON. Verified against an iPhone 14 Pro / iOS 18.5 real-device capture:

```json
{
  "ios_version": "18.5",
  "kernel_name": "Darwin",
  "kernel_version": "22F76",
  "kernel_arch": "ARM_64E"
}
```

## IDA evidence

`-[SEONOSProbe kernel_version]` at `0x15be8` issues:

```c
v10 = 0x4100000001LL;                         // { CTL_KERN=1, KERN_OSVERSION=65 }
sysctl((int *)&v10, 2, NULL, &len, NULL, 0);  // probe length
sysctl((int *)&v10, 2, buf,  &len, NULL, 0);  // read into buf
return [NSString stringWithUTF8String:buf];
```

`KERN_OSVERSION` (sysctl id 65, oid `kern.osversion`) returns the **iOS build number** — not the full `Darwin Kernel Version N.M.P …` string. That full string is what `kern.version` (sysctl id 4) returns; SEON's SDK does not use `KERN_VERSION`.

So we emit the build number directly as `kernel_version`. Earlier versions of this SDK emitted the full Darwin string, which was an immediate tell (no real iPhone does that).

## Build number table

`devices/build_numbers.go` maps marketing version → known build IDs (source: ipsw.me, cross-checked across iPhone 11 → iPhone 17). Example:

```go
"18.5":   {"22F76"},
"18.6":   {"22G86"},
"18.6.1": {"22G90"},
"18.6.2": {"22G100"},
"26.0":   {"23A341", "23A345"},  // iPhone 15+ vs iPhone 17 launch
...
```

`BuildNumberFor("18.5")` returns `"22F76"` deterministically. Versions with multiple known builds return one at random so different devices with the same iOS version don't collapse onto a single string.

## Cross-check rules

- `ios_version` × `device_name` (machine): impossible pairings rejected at `NewDevice` time (`ValidateIOSVersion` in `devices/compatibility.go`). iPhone 16 Pro + iOS 17 → error.
- `ios_version` × `kernel_version` (build): automatically consistent because `BuildDefaults()` pipes the same `iosVersion` string through both `BuildNumberFor` and the compatibility check.
- `kernel_arch` / `kernel_name`: always `"ARM_64E"` / `"Darwin"` on every supported iPhone.

## What DOESN'T go in the payload

Keys the SDK *doesn't* emit, even though Apple APIs exist:

- `kern.version` (full Darwin kernel string) — never populated. Don't send it.
- `hw.cputype` as a CTType — `cpu_type` is the hardcoded string `"ARM_64E"`, not the numeric CT type.
- `hw.machinemodel` — not emitted. `device_name` is `DeviceName` (model marketing name like `"iPhone 14 Pro"`).

If a future SEON SDK adds the full Darwin kernel version, we'd see a new `sysctl(..., KERN_VERSION, ...)` call near `0x15be8`. None exists in 5.6.3.
