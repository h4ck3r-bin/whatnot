# Encryption Pipeline

## Overview

The SEON SDK encrypts the fingerprint JSON through a multi-stage pipeline before returning it as a wire-format string.

## Pipeline

```
JSON bytes (sorted keys, 2-space indent, pretty-printed)
    │
    ▼
gzip compress (zlib deflate, level=default, windowBits=31 = gzip format)
    │
    ▼
PKCS7 pad to AES block size (16 bytes)
    │
    ▼
AES-256-CBC encrypt
    │  Key: -JaNdRgUkXp2s5v8y/B?E(H+MbPeShVm  (32 bytes, hardcoded)
    │  IV:  16 bytes from crypto/rand (SecRandomCopyBytes equivalent)
    │
    ▼
Base64 encode IV and ciphertext (standard base64, no line breaks)
    │
    ▼
Format: "iOS;<sessionID>;<base64(IV)>;<base64(ciphertext)>"
```

## AES Key

The AES-256 key is hardcoded in the iOS SDK binary. It is the same key in every install of SeonSDK 6.0 worldwide:

```
-JaNdRgUkXp2s5v8y/B?E(H+MbPeShVm
```

Hex: `2d4a614e645267556b58703273357638792f423f4528482b4d6250655368566d`

In the iOS binary, this key is obfuscated via a 14-round XOR-rotate-salt algorithm and deobfuscated at runtime. Our Go implementation stores it as a literal since obfuscation provides no security benefit when the algorithm is known.

The AES library used by the iOS SDK is **tiny-AES-c** by kokke (confirmed by recovering 13 named primitives from the LLVM coverage instrumentation: `_KeyExpansion`, `_Cipher`, `_AddRoundKey`, etc.).

## Wire Format

```
iOS;<sessionID>;<base64(IV)>;<base64(ciphertext)>
```

- **Platform prefix**: always `"iOS"`
- **Session ID**: a UUID string like `"8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6"`
- **IV**: 16 bytes, base64-encoded (standard encoding, no padding omission)
- **Ciphertext**: variable length, base64-encoded

All four parts are separated by semicolons (`;`).

## JSON Serialization

The main payload JSON uses `NSJSONWritingPrettyPrinted | NSJSONWritingSortedKeys` (options=3):
- **Sorted keys**: alphabetical at all nesting levels
- **Pretty-printed**: 2-space indent (modern iOS 17+/26 behavior)
- **Key-value separator**: ` : ` (space-colon-space)
- **Null values**: serialized as `null`

The DC side-channel body JSON uses options=1 (PrettyPrinted only, not sorted), but since it only has one key (`"data"`), sorting is irrelevant.

## Decryption (Python)

```python
from Crypto.Cipher import AES
from Crypto.Util.Padding import unpad
import base64, gzip

KEY = b"-JaNdRgUkXp2s5v8y/B?E(H+MbPeShVm"

def decrypt_seon(payload: str) -> bytes:
    _ios, session_id, iv_b64, ct_b64 = payload.split(";", 3)
    iv = base64.b64decode(iv_b64)
    ct = base64.b64decode(ct_b64)
    pt = AES.new(KEY, AES.MODE_CBC, iv).decrypt(ct)
    return gzip.decompress(unpad(pt, 16))
```

## Decryption (Go)

```go
sessionID, jsonBytes, err := crypto.DecodePayload(wireString)
```

Or using the high-level API:

```go
sessionID, payloadMap, err := seonsdk.Decrypt(wireString)
```

## Verification

All encryption parameters verified via IDA Pro decompilation:

| Parameter | IDA Address | Value |
|---|---|---|
| AES key (obfuscated) | `0x102ac` | 32 MOV instructions → deobfuscated to the key above |
| Wire format string | `0x2da8f` | `CFSTR("iOS;%@;%@;%@")` |
| Base64 options | `0x53a0, 0x53c8` | `options:0` (standard, no line breaks) |
| JSON options (main) | `0x5218` | `options:3` (PrettyPrinted + SortedKeys) |
| JSON options (DC body) | `0x7890` | `options:1` (PrettyPrinted only) |
| Gzip windowBits | `0x22340` | `31` (MAX_WBITS + 16 = gzip format) |
| Gzip level | `0x22338` | `-1` (Z_DEFAULT_COMPRESSION) |
| IV source | `0x54cc` | `SecRandomCopyBytes(kSecRandomDefault, 16, buf)` |
| PKCS7 block size | `0x10818` | `16` |
