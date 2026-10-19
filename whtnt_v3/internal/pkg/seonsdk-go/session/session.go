package session

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var dnsLabelRegex = regexp.MustCompile(`[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?`)

const sanitizeWhitelist = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func NewSessionID() string {
	return strings.ToUpper(uuid.New().String())
}

func NewRequestID() string {
	return strings.ToUpper(uuid.New().String())
}

func SanitizeSessionID(s string) string {
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(sanitizeWhitelist, c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// DNSLabel extracts the DNS subdomain label that gets prepended to
// `seondnsresolve.com` for the DC request. The label is lowercased —
// Apple's NSURL normalisation per RFC 3986 always lowercases the host
// portion of a URL before NSURLSession sends it, so a real iPhone POSTs
// `Host: <lowercase-uuid>.seondnsresolve.com` even though the session
// ID itself is uppercase. Go's net/http preserves URL host case as-is,
// so we must lowercase here to match the wire-level Host header real
// iPhones emit.
//
// The SDK's own regex (`[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?`
// at IDA 0x7e5c) runs BEFORE URL construction, so the matched label
// retains the sanitized session ID's case. The lowercasing happens
// implicitly at `NSURL URLWithString:` → `NSURLRequest.URL.host` on
// iOS. Reproducing that normalisation here keeps our outgoing Host
// header byte-identical to what a real iPhone sends.
func DNSLabel(sessionID string) string {
	sanitized := SanitizeSessionID(sessionID)
	match := dnsLabelRegex.FindString(sanitized)
	return strings.ToLower(match)
}
