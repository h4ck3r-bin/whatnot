package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"strings"
)

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > AESBlockSize || padLen > len(data) {
		return nil, fmt.Errorf("invalid padding length %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, fmt.Errorf("invalid padding byte at %d", i)
		}
	}
	return data[:len(data)-padLen], nil
}

func Decrypt(iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(AESKey)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	if len(ciphertext)%AESBlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d not multiple of block size", len(ciphertext))
	}

	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	return pkcs7Unpad(plaintext)
}

// decodeToPlaintext performs the shared part of the pipeline: split the wire
// string, base64-decode the IV and ciphertext, and AES-CBC decrypt. The result
// is whatever the sender encrypted — gzipped for fingerprints and DC requests,
// raw JSON for DC responses.
func decodeToPlaintext(wireString string) (sessionID string, decrypted []byte, err error) {
	parts := strings.SplitN(wireString, WireFormatSeparator, 4)
	if len(parts) != 4 {
		return "", nil, fmt.Errorf("expected 4 parts separated by '%s', got %d", WireFormatSeparator, len(parts))
	}
	if parts[0] != WireFormatPlatform {
		return "", nil, fmt.Errorf("expected platform '%s', got '%s'", WireFormatPlatform, parts[0])
	}

	sessionID = parts[1]

	iv, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, fmt.Errorf("base64 decode IV: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", nil, fmt.Errorf("base64 decode ciphertext: %w", err)
	}

	decrypted, err = Decrypt(iv, ct)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt: %w", err)
	}
	return sessionID, decrypted, nil
}

// IsGzipped reports whether b starts with the gzip magic number.
func IsGzipped(b []byte) bool {
	return len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b
}

func DecodePayload(wireString string) (sessionID string, jsonBytes []byte, err error) {
	sessionID, compressed, err := decodeToPlaintext(wireString)
	if err != nil {
		return "", nil, err
	}

	jsonBytes, err = GzipDecompress(compressed)
	if err != nil {
		if !IsGzipped(compressed) {
			return "", nil, fmt.Errorf(
				"gunzip: %w (payload is not gzipped — DC RESPONSES are AES-only; use DecodeAny)", err)
		}
		return "", nil, fmt.Errorf("gunzip: %w", err)
	}

	return sessionID, jsonBytes, nil
}

// DecodeAny decrypts any SEON wire string, gunzipping only when the decrypted
// bytes actually carry a gzip header.
//
// The direction matters: fingerprint payloads and DC REQUESTS are
// gzip-then-AES, but DC RESPONSES are AES only — the seondnsresolve.com server
// echoes back the request IV and encrypts plain JSON with no compression.
// Feeding a DC response to DecodePayload fails with "gzip: invalid header".
// Verified against testdata/freshwhatnot/wire/dc{1,2,3}_resp.txt.
//
// wasGzipped reports which form was found, so callers can assert on direction.
func DecodeAny(wireString string) (sessionID string, jsonBytes []byte, wasGzipped bool, err error) {
	sessionID, decrypted, err := decodeToPlaintext(wireString)
	if err != nil {
		return "", nil, false, err
	}
	if !IsGzipped(decrypted) {
		return sessionID, decrypted, false, nil
	}
	jsonBytes, err = GzipDecompress(decrypted)
	if err != nil {
		return "", nil, true, fmt.Errorf("gunzip: %w", err)
	}
	return sessionID, jsonBytes, true, nil
}
