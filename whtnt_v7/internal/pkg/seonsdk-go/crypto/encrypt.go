package crypto

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - (len(data) % blockSize)
	padding := bytes.Repeat([]byte{byte(padLen)}, padLen)
	return append(data, padding...)
}

// GzipCompress produces a gzip frame byte-identical to what -[NSData gzipDeflate]
// emits on iOS (verified against IDA 0x222c8: deflateInit2_ with windowBits=31,
// level=-1/Z_DEFAULT_COMPRESSION, zlib version "1.2.12"). Apple's zlib writes:
//
//	MTIME = 0  (set by deflateInit2_ with windowBits=31 + no gz_header explicitly)
//	XFL   = 0  (level 6 is neither fastest nor max)
//	OS    = 3  (UNIX — the OS byte baked into Apple's zlib build)
//
// Go's default gzip.Writer instead writes MTIME=time.Now() + OS=0xFF (unknown),
// which are visible inside the AES ciphertext after SEON's backend decrypts.
// Matching the iOS byte sequence removes a cheap tell if SEON ever validates
// gzip framing. The Header-override fields are documented in compress/gzip.
func GzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	w.ModTime = time.Time{} // serialized as MTIME=0 per compress/gzip docs
	w.OS = 3                // UNIX, matches Apple's zlib build
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func GzipDecompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func Encrypt(plaintext []byte) (iv []byte, ciphertext []byte, err error) {
	block, err := aes.NewCipher(AESKey)
	if err != nil {
		return nil, nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	iv = make([]byte, IVSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, fmt.Errorf("rand.Read IV: %w", err)
	}

	padded := pkcs7Pad(plaintext, AESBlockSize)
	ciphertext = make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	return iv, ciphertext, nil
}

func EncodePayload(sessionID string, jsonBytes []byte) (string, error) {
	compressed, err := GzipCompress(jsonBytes)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}

	iv, ct, err := Encrypt(compressed)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	ivB64 := base64.StdEncoding.EncodeToString(iv)
	ctB64 := base64.StdEncoding.EncodeToString(ct)

	return fmt.Sprintf("%s%s%s%s%s%s%s",
		WireFormatPlatform, WireFormatSeparator,
		sessionID, WireFormatSeparator,
		ivB64, WireFormatSeparator,
		ctB64), nil
}

// EncodePayloadNoGzip is the wire-format encoder for SEON DC server responses.
// Unlike outbound fingerprint payloads, the DC response is NOT gzipped before
// AES-CBC: the legitimate corpus shows plaintext like
// `{"request_id":"<uuid>","device_ip":"<ip>"}` (81 bytes) → 96-byte ciphertext.
// Use this when synthesizing a `dynamic_evaluator_results` value the iOS SDK
// would have received from the live DC endpoint.
func EncodePayloadNoGzip(sessionID string, jsonBytes []byte) (string, error) {
	iv, ct, err := Encrypt(jsonBytes)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	ivB64 := base64.StdEncoding.EncodeToString(iv)
	ctB64 := base64.StdEncoding.EncodeToString(ct)

	return fmt.Sprintf("%s%s%s%s%s%s%s",
		WireFormatPlatform, WireFormatSeparator,
		sessionID, WireFormatSeparator,
		ivB64, WireFormatSeparator,
		ctB64), nil
}
