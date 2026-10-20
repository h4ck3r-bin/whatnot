# freshwhatnot corpus

Ground-truth SEON material extracted from `freshwhatnot.har`, a manual capture of
the real Whatnot iOS app. This is the **only** reference corpus — do not mix in
other captures.

Capture device / app:

| | |
|---|---|
| Model | `iPhone18,2` (iPhone 17 Pro Max) |
| iOS | `26.5.2` (build `23F84`) |
| App | `Whatnot v26.32.5 (22)` |
| SEON SDK | `ios-5.7.1` / `version_code 01050701` |
| Captured | 2026-08-08 10:27–10:29 UTC (one app session, 116 s) |

## Contents

| File | HAR entry | What |
|---|---|---|
| `fp_register.json` | 223 | `POST /api/v2/register` — decrypted `session` |
| `fp_setphone_1.json` | 254 | `SetPhoneNumberMutationV2` — decrypted `session` |
| `fp_setphone_2.json` | 266 | `SetPhoneNumberMutationV2` — decrypted `session` |
| `wire/fp_*.txt` | — | the same three, as captured wire strings |
| `wire/dc{1,2,3}_req.txt` | 222/253/265 | DC request body `data` field |
| `wire/dc{1,2,3}_resp.txt` | 222/253/265 | DC response body `data` field |

The `.json` files are Apple's **exact plaintext bytes** — `NSJSONSerialization`
with `SortedKeys | PrettyPrinted`, 2-space indent, `" : "` separator, no trailing
newline. Do not reformat them; several tests diff against these bytes.

## Load-bearing facts

- **Floats are `%.17g`.** All 110 distinct float literals across the three
  payloads match `%.17g`; only 44/110 match Go's shortest round-trip.
- **DC responses are not gzipped.** Requests are gzip+AES-CBC; responses are
  AES-CBC only, and the server echoes the request IV.
- **`proxy_address` / `proxy_state` / `proxy_type` are capture artifacts** of the
  mitmproxy used to record the HAR. They are not device truth — do not copy them.
- Only 11 of 132 flattened fields vary across the three payloads, but all three
  come from a single app session, so "stable here" means *session*-stable, not
  necessarily *device*-stable.
