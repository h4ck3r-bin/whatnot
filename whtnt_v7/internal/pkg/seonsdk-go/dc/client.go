package dc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	http "github.com/bogdanfinn/fhttp"

	tls_client "github.com/bogdanfinn/tls-client"

	"status403.com/rsavv/seonsdk-go/crypto"
	"status403.com/rsavv/seonsdk-go/payload"
	"status403.com/rsavv/seonsdk-go/session"
	"status403.com/rsavv/seonsdk-go/version"
)

const (
	UserAgent    = "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.11 (KHTML, like Gecko) Chrome/23.0.1271.95 Safari/537.11"
	DCURLPattern = "https://%s.seondnsresolve.com/ios"
	DCTimeout    = 2 * time.Second

	// DefaultAcceptLanguage is what NSURLSession on a US-English iPhone sends by
	// default (derived from [NSLocale preferredLanguages]). Override per-request
	// via DCClient.AcceptLanguage if the simulated device is non-English.
	DefaultAcceptLanguage = "en-US,en;q=0.9"

	// AcceptEncoding mirrors what iOS NSURLSession injects automatically on
	// iOS 13+ when the caller doesn't set Accept-Encoding itself. The `br`
	// (Brotli) entry is iOS-specific and distinguishes genuine NSURLSession
	// traffic from generic HTTP clients that stop at `gzip, deflate`.
	AcceptEncoding = "gzip, deflate, br"
)

type Response struct {
	JSONResponse string
	Success      bool

	// RequestID is the UUID generated for the DC request payload. In SDK 5.7.1+
	// this same UUID is also emitted as a top-level `request_id` field in the
	// fingerprint payload.
	RequestID string

	// ErrorMessage mirrors what the real iOS SDK logs in stored_errors when the DC call
	// fails. Verified against IDA at 0x7a4c (SEONNetworkProbe sendDynamicComponentRequest
	// completion block). Possible values:
	//   - "The request timed out."          (NSURLErrorTimedOut, localized description)
	//   - "Request failed - <status>"       (HTTP non-200 response)
	//   - "Error parsing DC response JSON"  (JSON decode failure)
	//   - ""                                (success or no DC call attempted)
	ErrorMessage string
}

// Error message constants matching the real iOS SDK's error format.
const (
	// ErrorRequestTimedOut is the `localizedDescription` of `NSURLErrorTimedOut` on iOS
	// en-US locale. The real SDK passes this through verbatim to the error logger.
	ErrorRequestTimedOut = "The request timed out."

	// ErrorRequestFailedFormat is used when the DC endpoint returns a non-200 status.
	// Verified at 0x7cdc: stringWithFormat:@"Request failed - %ld", statusCode.
	ErrorRequestFailedFormat = "Request failed - %d"

	// ErrorParseDCResponse is logged if the DC response body isn't valid JSON.
	// Verified at 0x7c44.
	ErrorParseDCResponse = "Error parsing DC response JSON"
)

// DCClient holds the *template* for each DC call. It intentionally does NOT hold
// a long-lived tls_client.HttpClient: IDA 0x7910 + 0x7940 shows the real SDK
// builds a fresh NSURLSessionConfiguration + NSURLSession per dispatch — one
// request, then discarded — so every fingerprint's DC probe gets a brand-new
// TLS handshake and no session-ticket / h2-connection reuse between fingerprints.
//
// If the caller supplies a HTTPClient (for custom fingerprint overrides or shared
// proxy authentication) we reuse it as-is — they've opted in to connection reuse.
type DCClient struct {
	proxy          string
	httpClient     tls_client.HttpClient
	AcceptLanguage string
	// Source overrides the `source` value the DC payload advertises.
	// Empty = package default (version.Source). Set to
	// version.SourceFor(bundleVersion) to match a non-default SDK version.
	Source string
}

func NewDCClient(httpClient tls_client.HttpClient, proxy string) (*DCClient, error) {
	return &DCClient{
		proxy:          proxy,
		httpClient:     httpClient,
		AcceptLanguage: DefaultAcceptLanguage,
	}, nil
}

// Close is a no-op. The per-call session lifecycle is managed inside
// SendDCRequest; a caller-provided httpClient is owned by the caller.
func (c *DCClient) Close() {}

func BuildDCPayload(sessionID string) (map[string]interface{}, string) {
	return BuildDCPayloadWithSource(sessionID, version.Source)
}

// BuildDCPayloadWithSource lets callers override the `source` field
// so it matches the SDK version chosen for this fingerprint. Typical use:
// `BuildDCPayloadWithSource(sid, version.SourceFor(device.SDKVersion))`.
func BuildDCPayloadWithSource(sessionID, source string) (map[string]interface{}, string) {
	requestID := session.NewRequestID()
	return map[string]interface{}{
		"source":     source,
		"request_id": requestID,
	}, requestID
}

func (c *DCClient) SendDCRequest(sessionID string) (*Response, error) {
	source := c.Source
	if source == "" {
		source = version.Source
	}
	dcPayload, requestID := BuildDCPayloadWithSource(sessionID, source)

	// Inner payload: the real SDK serializes with NSJSONSerialization options=3
	// (PrettyPrinted | SortedKeys) at 0x5194. That's the byte stream that gets
	// gzipped + AES-CBC encrypted, so its exact formatting ends up in the
	// ciphertext length/padding pattern. Use MarshalSortedJSON to match.
	dcJSON, err := payload.MarshalSortedJSON(dcPayload)
	if err != nil {
		return &Response{Success: false}, fmt.Errorf("marshal DC payload: %w", err)
	}

	encrypted, err := crypto.EncodePayload(sessionID, dcJSON)
	if err != nil {
		return &Response{Success: false}, fmt.Errorf("encrypt DC payload: %w", err)
	}

	dnsLabel := session.DNSLabel(sessionID)
	url := fmt.Sprintf(DCURLPattern, dnsLabel)

	// Outer wrapper: the real SDK uses NSJSONSerialization options=1
	// (PrettyPrinted only) at 0x7684. Apple's pretty writer emits `"key" : value`
	// with a space BEFORE and after the colon. Standard Go json.MarshalIndent
	// emits `"key": value` (no leading space) — distinguishable byte-for-byte.
	// MarshalSortedJSON produces the Apple format; with a single key the
	// sorting vs non-sorting distinction is a no-op.
	bodyJSON, err := payload.MarshalSortedJSON(map[string]interface{}{"data": encrypted})
	if err != nil {
		return &Response{Success: false}, fmt.Errorf("marshal DC body: %w", err)
	}

	s, sessionErr := c.buildSession()
	if sessionErr != nil {
		return &Response{Success: false}, sessionErr
	}
	// c.httpClient is owned by the caller; only close the session we built.
	if c.httpClient == nil {
		defer s.CloseIdleConnections()
	}

	acceptLang := c.AcceptLanguage
	if acceptLang == "" {
		acceptLang = DefaultAcceptLanguage
	}

	headers := http.Header{
		"Accept":          {"*/*"},
		"Content-Type":    {"application/x-www-form-urlencoded"},
		"Accept-Language": {acceptLang},
		"Accept-Encoding": {AcceptEncoding},
		"User-Agent":      {UserAgent},
		"Priority":        {"u=3"},
		http.HeaderOrderKey: {
			"accept", "content-type", "accept-language",
			"accept-encoding", "user-agent", "priority", "content-length",
		},
	}

	type dcPostResult struct {
		resp *http.Response
		err  error
	}
	resCh := make(chan dcPostResult, 1)
	go func() {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
		req.Header = headers
		resp, err := s.Do(req)
		resCh <- dcPostResult{resp, err}
	}()

	var resp *http.Response
	select {
	case r := <-resCh:
		resp = r.resp
		err = r.err
	case <-time.After(DCTimeout):
		// Force-close the session to interrupt the in-flight request. When
		// the goroutine later drains, its result is discarded.
		if c.httpClient == nil {
			s.CloseIdleConnections()
		}
		return &Response{Success: false, RequestID: requestID, ErrorMessage: ErrorRequestTimedOut}, nil
	}
	if err != nil {
		return &Response{Success: false, RequestID: requestID, ErrorMessage: ErrorRequestTimedOut}, nil
	}

	if resp.StatusCode != 200 {
		return &Response{
			Success:      false,
			RequestID:    requestID,
			ErrorMessage: fmt.Sprintf(ErrorRequestFailedFormat, resp.StatusCode),
		}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Response{
			Success:      false,
			RequestID:    requestID,
			ErrorMessage: ErrorParseDCResponse,
		}, nil
	}

	var respData map[string]interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		return &Response{Success: true, RequestID: requestID, ErrorMessage: ErrorParseDCResponse}, nil
	}

	jsonResp := ""
	if data, ok := respData["data"]; ok {
		if s, ok := data.(string); ok {
			jsonResp = s
		}
	}

	return &Response{
		JSONResponse: jsonResp,
		RequestID:    requestID,
		Success:      true,
	}, nil
}

// buildSession returns a tls_client.HttpClient to use for one DC request. When
// the caller provided a session via NewDCClient we reuse it (they've opted in
// to long-lived connections); otherwise we build a fresh one per call so the
// TLS handshake, cookies, and h2 connection state all start from zero —
// matching NSURLSession's one-shot behavior at IDA 0x7910 / 0x7940.
//
// The TLS profile is IOSNSURLSessionProfile (defined in ios_profile.go), which
// reproduces the exact ClientHello captured from a real iPhone via rvictl.
//
// Note: we deliberately do NOT pass tls_client.WithForceHttp1(). That option
// rewrites the ALPN extension in the ClientHello to advertise only "http/1.1"
// (utls u_parrots.go:3264), which would diverge from the captured iPhone
// fingerprint that advertises "h2, http/1.1" — JA3 is unaffected (ALPN is not
// in the JA3 hash) but JA4 and any byte-level matchers would notice. The DC
// endpoint speaks HTTP/1.1, so the protocol negotiation lands on HTTP/1.1
// anyway via ALPN — we just want the wire bytes to look right going in.
func (c *DCClient) buildSession() (tls_client.HttpClient, error) {
	if c.httpClient != nil {
		return c.httpClient, nil
	}

	var options []tls_client.HttpClientOption
	options = append(options,
		tls_client.WithTimeoutSeconds(2),
		tls_client.WithClientProfile(IOSNSURLSessionProfile),
	)
	if c.proxy != "" {
		options = append(options, tls_client.WithProxyUrl(c.proxy))
	}

	s, _ := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)

	return s, nil
}
