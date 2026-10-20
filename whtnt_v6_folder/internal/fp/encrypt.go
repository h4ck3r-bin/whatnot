package fp

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

const seonFingerprintAESKeyHex = "2d4a614e645267556b58703273357638792f423f4528482b4d6250655368566d"

func padPKCS7(data []byte, blockSize int) []byte {
	p := blockSize - (len(data) % blockSize)

	padding := make([]byte, p)

	for i := range padding {
		padding[i] = byte(p)
	}

	return append(data, padding...)
}

func EncryptAndGenerateFingerprint(sessionID SessionID, data any, shouldGzip bool) (string, error) {
	return EncryptAndGenerateFingerprintWithPrefix("Android", sessionID, data, shouldGzip)
}

func EncryptAndGenerateFingerprintWithPrefix(prefix string, sessionID SessionID, data any, shouldGzip bool) (string, error) {
	raw, err := json.Marshal(data)

	if err != nil {
		return "", err
	}

	var buf bytes.Buffer

	if shouldGzip {
		gz := gzip.NewWriter(&buf)
		if _, err = gz.Write(raw); err != nil {
			return "", err
		}

		gz.Close()
	} else {
		buf.Write(raw)
	}

	iv := make([]byte, aes.BlockSize)

	if _, err = io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	raw = buf.Bytes()
	padded := padPKCS7(raw, aes.BlockSize)

	key, err := hex.DecodeString(seonFingerprintAESKeyHex)

	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)

	if err != nil {
		return "", err
	}

	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	session := fmt.Sprintf("%s;%s;%s;%s",
		prefix,
		sessionID,
		base64.StdEncoding.EncodeToString(iv),
		base64.StdEncoding.EncodeToString(ct),
	)

	return session, nil
}
