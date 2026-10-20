package whtnt

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	fhttp "github.com/bogdanfinn/fhttp"
)

const (
	hyperDebugProxy = "http://127.0.0.1:8888"

	hyperDebugReset  = "\033[0m"
	hyperDebugDim    = "\033[2m"
	hyperDebugCyan   = "\033[36m"
	hyperDebugYellow = "\033[33m"
)

var (
	hyperDebugMu      sync.Mutex
	hyperDebugCounter atomic.Uint64
)

type hyperDebugTransport struct {
	base http.RoundTripper
}

func newProxiedHTTPClient(proxyURL string) *http.Client {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(parsed),
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
		},
	}
}

func newHyperDebugHTTPClient() *http.Client {
	proxyURL, err := url.Parse(hyperDebugProxy)
	if err != nil {
		proxyURL = nil
	}

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &hyperDebugTransport{base: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			ForceAttemptHTTP2:   false,
			TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
			MaxIdleConns:        0,
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     100,
			IdleConnTimeout:     30 * time.Second,
		}},
	}
}

func (t *hyperDebugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id := hyperDebugCounter.Add(1)
	body := readAndRestoreHTTPBody(req)
	hyperDebugBlock(fmt.Sprintf("HYPER HTTP REQUEST #%d", id),
		fmt.Sprintf("%s %s", req.Method, req.URL.String()),
		formatHTTPHeaders(map[string][]string(req.Header)),
		"Body:",
		formatDebugBody(body),
	)

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		hyperDebugBlock(fmt.Sprintf("HYPER HTTP ERROR #%d", id), err.Error())
		return nil, err
	}

	respBody := readAndRestoreHTTPResponseBody(resp)
	hyperDebugBlock(fmt.Sprintf("HYPER HTTP RESPONSE #%d", id),
		fmt.Sprintf("Status: %s", resp.Status),
		formatHTTPHeaders(map[string][]string(resp.Header)),
		"Body:",
		formatDebugBody(respBody),
	)

	return resp, nil
}

func readAndRestoreHTTPBody(req *http.Request) []byte {
	if req == nil || req.Body == nil {
		return nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return []byte("<failed to read request body: " + err.Error() + ">")
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func readAndRestoreHTTPResponseBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []byte("<failed to read response body: " + err.Error() + ">")
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func (s *KasadaSolver) debugRequest(title string, req *fhttp.Request) {
	if !s.hyperDebugEnabled() || req == nil {
		return
	}
	hyperDebugBlock("KASADA REQUEST: "+title,
		fmt.Sprintf("%s %s", req.Method, req.URL.String()),
		formatHTTPHeaders(map[string][]string(req.Header)),
		"Body:",
		formatRequestBody(req.Body),
	)
}

func (s *KasadaSolver) debugResponse(title string, resp *httpResponse, err error) {
	if !s.hyperDebugEnabled() {
		return
	}
	if err != nil {
		hyperDebugBlock("KASADA ERROR: "+title, err.Error())
		return
	}
	if resp == nil {
		hyperDebugBlock("KASADA RESPONSE: "+title, "<nil response>")
		return
	}
	hyperDebugBlock("KASADA RESPONSE: "+title,
		fmt.Sprintf("Status: %s (%d)", resp.Status, resp.StatusCode),
		formatHTTPHeaders(map[string][]string(resp.Header)),
		"Body:",
		formatDebugBody(resp.Body),
	)
}

func (s *KasadaSolver) debugJSON(title string, value any) {
	if !s.hyperDebugEnabled() {
		return
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		s.debugText(title, fmt.Sprintf("<json marshal failed: %s>", err))
		return
	}
	hyperDebugBlock(title, string(raw))
}

func (s *KasadaSolver) debugBytes(title string, value []byte) {
	if !s.hyperDebugEnabled() {
		return
	}
	hyperDebugBlock(title, formatDebugBody(value))
}

func (s *KasadaSolver) debugText(title string, value string) {
	if !s.hyperDebugEnabled() {
		return
	}
	hyperDebugBlock(title, value)
}

func (s *KasadaSolver) debugError(title string, err error) {
	if !s.hyperDebugEnabled() || err == nil {
		return
	}
	hyperDebugBlock(title, err.Error())
}

func (s *KasadaSolver) sleepAfterHyperDebugError() {
	if !s.hyperDebugEnabled() {
		return
	}
	hyperDebugBlock("HYPER DEBUG PAUSE", "Sleeping 20s after Hyper error.")
	time.Sleep(20 * time.Second)
}

func (s *KasadaSolver) hyperDebugEnabled() bool {
	return s != nil && s.config != nil && s.config.HyperDebug
}

func kasadaDebugRequestLabel(rawURL string, attempt int) string {
	label := "request"
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "/fp"):
		label = "/fp"
	case strings.Contains(lower, "/tl"):
		label = "/tl"
	case strings.Contains(lower, "/mfc"):
		label = "/mfc"
	case strings.Contains(lower, ".js"):
		label = "ips.js"
	case strings.Contains(lower, "/register"):
		label = "register page"
	}
	return fmt.Sprintf("%s attempt %d/%d", label, attempt, kasadaRetryAttempts)
}

func hyperDebugBlock(title string, lines ...string) {
	hyperDebugMu.Lock()
	defer hyperDebugMu.Unlock()

	fmt.Printf("%s\n", strings.Repeat("=", 96))
	fmt.Printf("%s[HYPER-DEBUG] %s%s\n", hyperDebugCyan, title, hyperDebugReset)
	fmt.Printf("%s\n", strings.Repeat("-", 96))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			fmt.Println()
			continue
		}
		fmt.Printf("%s%s%s\n", hyperDebugDim, line, hyperDebugReset)
	}
	fmt.Printf("%s%s%s\n", hyperDebugYellow, strings.Repeat("=", 96), hyperDebugReset)
}

func formatHTTPHeaders(headers map[string][]string) string {
	if len(headers) == 0 {
		return "Headers: <none>"
	}
	order, orderKey := extractHeaderOrder(headers)
	rendered := make(map[string]bool, len(headers))

	var b strings.Builder
	if len(order) > 0 {
		b.WriteString("Header order:\n")
		for _, key := range order {
			b.WriteString("  ")
			b.WriteString(key)
			b.WriteByte('\n')
		}
		b.WriteString("Headers:\n")
		for _, orderedKey := range order {
			actualKey, values, ok := findHeaderValues(headers, orderedKey)
			if !ok {
				continue
			}
			rendered[strings.ToLower(actualKey)] = true
			writeHeaderValues(&b, actualKey, values)
		}
	} else {
		b.WriteString("Headers:\n")
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		if isHeaderOrderKey(key) || strings.EqualFold(key, orderKey) {
			continue
		}
		if rendered[strings.ToLower(key)] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeHeaderValues(&b, key, headers[key])
	}
	return strings.TrimRight(b.String(), "\n")
}

func extractHeaderOrder(headers map[string][]string) ([]string, string) {
	for key, values := range headers {
		if isHeaderOrderKey(key) {
			return values, key
		}
	}
	return nil, ""
}

func isHeaderOrderKey(key string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(key)), ":")
	return normalized == "header-order"
}

func findHeaderValues(headers map[string][]string, wanted string) (string, []string, bool) {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	for key, values := range headers {
		if isHeaderOrderKey(key) {
			continue
		}
		if strings.ToLower(strings.TrimSpace(key)) == wanted {
			return key, values, true
		}
	}
	return "", nil, false
}

func writeHeaderValues(b *strings.Builder, key string, values []string) {
	for _, value := range values {
		b.WriteString("  ")
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
}

func formatRequestBody(body any) string {
	switch value := body.(type) {
	case nil:
		return "<empty>"
	case []byte:
		return formatDebugBody(value)
	case string:
		return formatDebugBody([]byte(value))
	case *bytes.Buffer:
		if value == nil {
			return "<empty>"
		}
		return formatDebugBody(value.Bytes())
	case bytes.Buffer:
		return formatDebugBody(value.Bytes())
	case io.Reader:
		return fmt.Sprintf("<stream body: %T>", value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func formatDebugBody(body []byte) string {
	if len(body) == 0 {
		return "<empty>"
	}
	if isReadableBody(body) {
		return string(body)
	}
	return fmt.Sprintf("<binary body: %d bytes, base64>\n%s", len(body), base64.StdEncoding.EncodeToString(body))
}

func isReadableBody(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	for _, r := range string(body) {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 32 {
			return false
		}
	}
	return true
}
