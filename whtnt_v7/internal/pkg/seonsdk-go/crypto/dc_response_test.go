package crypto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func corpusWire(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "freshwhatnot", "wire", name+".txt"))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.TrimSpace(string(b))
}

// TestDCResponsesAreNotGzipped pins the asymmetry that made DecodePayload fail
// on captured DC responses: requests are gzip+AES, responses are AES only.
func TestDCResponsesAreNotGzipped(t *testing.T) {
	for _, name := range []string{"dc1_resp", "dc2_resp", "dc3_resp"} {
		sessionID, plaintext, wasGzipped, err := DecodeAny(corpusWire(t, name))
		if err != nil {
			t.Fatalf("%s: DecodeAny: %v", name, err)
		}
		if wasGzipped {
			t.Errorf("%s: reported gzipped, want AES-only", name)
		}
		if sessionID == "" {
			t.Errorf("%s: empty session id", name)
		}
		var body struct {
			RequestID string `json:"request_id"`
			DeviceIP  string `json:"device_ip"`
		}
		if err := json.Unmarshal(plaintext, &body); err != nil {
			t.Fatalf("%s: response is not JSON: %v", name, err)
		}
		if body.RequestID == "" || body.DeviceIP == "" {
			t.Errorf("%s: incomplete DC response %s", name, plaintext)
		}
		// DecodePayload must still reject it, with a message that points at
		// the cause rather than a bare gzip error.
		if _, _, err := DecodePayload(corpusWire(t, name)); err == nil {
			t.Errorf("%s: DecodePayload unexpectedly succeeded", name)
		} else if !strings.Contains(err.Error(), "not gzipped") {
			t.Errorf("%s: unhelpful error %q", name, err)
		}
	}
}

// TestDCRequestsAreGzipped is the other half of the asymmetry.
func TestDCRequestsAreGzipped(t *testing.T) {
	for _, name := range []string{"dc1_req", "dc2_req", "dc3_req"} {
		_, plaintext, wasGzipped, err := DecodeAny(corpusWire(t, name))
		if err != nil {
			t.Fatalf("%s: DecodeAny: %v", name, err)
		}
		if !wasGzipped {
			t.Errorf("%s: reported AES-only, want gzip+AES", name)
		}
		var body struct {
			RequestID string `json:"request_id"`
			Source    string `json:"source"`
		}
		if err := json.Unmarshal(plaintext, &body); err != nil {
			t.Fatalf("%s: request is not JSON: %v", name, err)
		}
		if body.Source != "ios-5.7.1" {
			t.Errorf("%s: source = %q, want ios-5.7.1", name, body.Source)
		}
	}
}

// TestDCRequestResponsePairing confirms the server echoes the request_id, so
// the corpus request/response files really are matched pairs.
func TestDCRequestResponsePairing(t *testing.T) {
	pairs := [][2]string{{"dc1_req", "dc1_resp"}, {"dc2_req", "dc2_resp"}, {"dc3_req", "dc3_resp"}}
	for _, p := range pairs {
		var req, resp struct {
			RequestID string `json:"request_id"`
		}
		_, rb, _, err := DecodeAny(corpusWire(t, p[0]))
		if err != nil {
			t.Fatal(err)
		}
		_, sb, _, err := DecodeAny(corpusWire(t, p[1]))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rb, &req); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(sb, &resp); err != nil {
			t.Fatal(err)
		}
		if req.RequestID != resp.RequestID {
			t.Errorf("%s/%s: request_id %q vs %q", p[0], p[1], req.RequestID, resp.RequestID)
		}
	}
}

// TestFingerprintsStillDecodeViaDecodePayload guards against DecodeAny
// regressing the main path.
func TestFingerprintsStillDecodeViaDecodePayload(t *testing.T) {
	for _, name := range []string{"fp_register", "fp_setphone_1", "fp_setphone_2"} {
		wire := corpusWire(t, name)
		id1, viaPayload, err := DecodePayload(wire)
		if err != nil {
			t.Fatalf("%s: DecodePayload: %v", name, err)
		}
		id2, viaAny, gz, err := DecodeAny(wire)
		if err != nil {
			t.Fatalf("%s: DecodeAny: %v", name, err)
		}
		if !gz {
			t.Errorf("%s: fingerprint reported as not gzipped", name)
		}
		if id1 != id2 || string(viaPayload) != string(viaAny) {
			t.Errorf("%s: DecodePayload and DecodeAny disagree", name)
		}
	}
}
