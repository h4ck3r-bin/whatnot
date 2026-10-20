package crypto

import (
	"crypto/sha256"
	"fmt"
)

func SHA256Hex(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
