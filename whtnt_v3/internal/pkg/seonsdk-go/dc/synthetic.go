package dc

import (
	"fmt"

	"status403.com/rsavv/seonsdk-go/crypto"
	"status403.com/rsavv/seonsdk-go/session"
)

// BuildSyntheticSuccess returns a *Response that mimics what the live SEON DC
// endpoint replies with when the request_id round-trip succeeds.
//
// The legitimate corpus shows that the server's reply, after decryption, is
// always the plaintext JSON body
//
//	{"request_id":"<UUID>","device_ip":"<external-ip>"}
//
// AES-CBC encrypted (NOT gzipped) under the same shared key as outbound
// payloads, then wrapped in the standard `iOS;<sessionID>;<IV>;<CT>` wire
// format. The iOS SDK stores that wire string verbatim into the outer
// fingerprint's `dynamic_evaluator_results` field.
//
// Use this on server-side runners that cannot reach (or do not want to reach)
// the real DC endpoint. The shape, byte ordering, and padding all match a
// genuine successful DC roundtrip — far more iOS-realistic than emitting a
// timeout. deviceIP should be the public-facing egress IP the SEON server
// would have observed for this client (typically the proxy IP, the device's
// WAN IP, or an empty string if you genuinely don't know — but NOT
// "127.0.0.1" or another loopback / RFC-1918 address that the real DC
// endpoint could not have seen).
func BuildSyntheticSuccess(sessionID, deviceIP string) (*Response, error) {
	// Hand-craft the inner JSON so the byte layout matches what the live DC
	// server emits exactly:
	//   - keys in {request_id, device_ip} order, NOT alphabetical
	//   - no inter-key spaces (NSJSONSerialization options=0)
	//   - no trailing newline
	// Decrypted legit captures all parse to 81 bytes for IPv4 device_ip — the
	// math is 15+36+15+len(ip)+2.
	requestID := session.NewRequestID()
	innerJSON := []byte(fmt.Sprintf(`{"request_id":"%s","device_ip":"%s"}`, requestID, deviceIP))

	wire, err := crypto.EncodePayloadNoGzip(sessionID, innerJSON)
	if err != nil {
		return nil, fmt.Errorf("encrypt synthetic DC payload: %w", err)
	}

	return &Response{
		JSONResponse: wire,
		RequestID:    requestID,
		Success:      true,
	}, nil
}
