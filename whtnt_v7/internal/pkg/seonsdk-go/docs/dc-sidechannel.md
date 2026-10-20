# DC Side-Channel (Dynamic Component)

## Overview

Every `Fingerprint()` call (unless `SkipDC: true`) fires a small HTTP POST to SEON's "Dynamic Component" endpoint. This is a DNS-resolution side-channel that lets SEON's backend verify the client's network path.

## Request

**URL**: `https://<sessionID>.seondnsresolve.com/ios`

The subdomain is derived from the session ID:
1. `SanitizeSessionID(sid)` — strips chars outside `[A-Za-z0-9_-]`
2. Regex extract first match of `[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?`
3. Result is a DNS-label-safe string (max 63 chars)

For a standard UUID like `"8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6"`, the entire UUID becomes the subdomain.

**Method**: `POST`

**Headers** (in this exact order):
1. `User-Agent: Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.11 (KHTML, like Gecko) Chrome/23.0.1271.95 Safari/537.11`
2. `Accept: */*`
3. `Content-Length: <auto>` (added by HTTP transport, not set explicitly)

No `Content-Type` header is set.

**Body**: Pretty-printed JSON (options=1, NSJSONWritingPrettyPrinted):
```json
{
  "data" : "iOS;<sessionID>;<base64(IV)>;<base64(ciphertext)>"
}
```

Where the encrypted payload inside `"data"` decrypts to:
```json
{
  "source" : "ios-5.7.1",
  "request_id" : "<fresh UUID>"
}
```

- `source`: hardcoded SDK version tag, always `"ios-5.7.1"` for SeonSDK 5.7.1
- `request_id`: a fresh UUID generated per `Fingerprint()` call, distinct from the session ID

**Timeout**: 2 seconds (both request and resource)

**TLS Profile**: iOS browser profile (via `github.com/Noooste/azuretls-client` with `azuretls.Ios` navigator)

## Response

The server returns JSON like:
```json
{
  "data": "<some string>"
}
```

The `data` field (if present) is stored as `dynamic_evaluator_results` in the final fingerprint payload. This is SEON's runtime extensibility hook — the server can send back arbitrary evaluation context.

The iOS SDK also sets a `dnsCheckSuccess` boolean on the response object based on HTTP status (true if 200, false otherwise), but **no code ever reads it**. It's vestigial.

## Purpose

The per-session subdomain forces the device's DNS resolver to make a fresh query to SEON's authoritative DNS. SEON can then correlate:
1. DNS query from resolver IP `R` at time `T0`
2. HTTP POST from client IP `C` at time `T1`
3. Full fingerprint (arriving later via the host app's backend) with the same session ID

If `R` and `C` don't match (e.g., HTTP through a VPN but DNS through the local resolver), the fingerprint can be flagged as suspicious.

## User-Agent

The hardcoded User-Agent is Chrome 23 on Windows 7 — an intentionally outdated browser string. Verified from CFSTR constant at `0x77e8` in the SeonSDK binary.

This is likely chosen to look like generic background HTTP noise rather than an identifiable SDK request. It's NOT the device's actual Safari User-Agent.

## Proxy Support

## DCClient

The DC side-channel is managed by a `dc.DCClient` struct wrapping an `azuretls.Session`:

```go
// Internal usage (Fingerprint() creates and closes this automatically):
dcClient, err := dc.NewDCClient("http://proxy:8080")  // proxy is optional ("")
defer dcClient.Close()
resp, err := dcClient.SendDCRequest(sessionID)
```

No cookies are persisted — the azuretls session does not use a cookie jar.

## Proxy Support

```go
cfg := seonsdk.FingerprintConfig{
    Proxy: "http://user:pass@proxy.example.com:8080",
    // or socks5://...
}
```

Set `SkipDC: true` to disable the DC request entirely.

## IDA Verification

| Detail | IDA Address | Value |
|---|---|---|
| URL pattern | `0x7780` | `CFSTR("https://%@.seondnsresolve.com/ios")` |
| User-Agent | `0x77e8` | Chrome 23 / Windows 7 (full string) |
| Accept | `0x7800` | `CFSTR("*/*")` |
| Body key | `0x780c` | `CFSTR("data")` |
| Body JSON options | `0x7890` | `1` (PrettyPrinted only) |
| Request timeout | `0x7918` | `2.0` |
| Resource timeout | `0x7924` | `2.0` |
| No Content-Type | (absence) | Only 3 `setValue:forHTTPHeaderField:` calls in function |
