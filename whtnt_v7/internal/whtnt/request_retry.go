package whtnt

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

const (
	requestRetryAttempts           = 3
	requestRetryDelay              = 10 * time.Second
	proxyUnauthorizedRetryAttempts = 5
	proxyUnauthorizedRetryDelay    = 60 * time.Second
	kasadaRetryAttempts            = 3
)

func (m *Module) doRequestWithRetry(label string, request *http.Request) (*httpResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("%s request failed: client session is not available", label)
	}
	if m.client == nil {
		if err := m.rebuildHTTPClientSameProxy(fmt.Sprintf("%s missing client recovery", label)); err != nil {
			return nil, fmt.Errorf("%s request failed: recover missing client: %w", label, err)
		}
	}
	if request == nil {
		return nil, fmt.Errorf("%s request failed: request is nil", label)
	}

	var lastErr error
	var lastResponse *httpResponse
	originalProxy := ""
	switchedStripeDirect := false
	if m.client != nil {
		originalProxy = m.client.GetProxy()
	}
	defer func() {
		if switchedStripeDirect && m.client != nil && strings.TrimSpace(originalProxy) != "" {
			if err := m.client.SetProxy(originalProxy); err != nil && m.log != nil {
				m.log.ManagerError("Failed to restore original proxy after Stripe request: " + err.Error())
			}
		}
	}()

	requestURL := request.URL.String()

	for attempt := 1; attempt <= proxyUnauthorizedRetryAttempts; attempt++ {
		if m.client == nil {
			if err := m.rebuildHTTPClientSameProxy(fmt.Sprintf("%s missing client retry", label)); err != nil {
				return nil, fmt.Errorf("%s request failed: recover missing client retry: %w", label, err)
			}
		}
		cloned := cloneRequest(request)
		rawResp, err := m.client.Do(cloned)
		var res *httpResponse
		if err == nil {
			res, err = readResponse(rawResp)
		}
		if err == nil {
			m.autosaveSessionIfChanged(label)
		}
		proxyUnauthorized := isProxyUnauthorized(err, res)
		if err == nil && !proxyUnauthorized && !shouldRetryStatus(res) {
			return res, nil
		}
		lastErr = err
		lastResponse = res
		if proxyUnauthorized {
			lastErr = proxyUnauthorizedError(label, err, res)
			if attempt >= proxyUnauthorizedRetryAttempts {
				break
			}
			delay := m.proxyUnauthorizedRetryDelay()
			logProxyUnauthorizedRetry(m, label, attempt, proxyUnauthorizedDetail(err, res), delay)
			if delay > 0 {
				time.Sleep(delay)
			}
			if rotateErr := m.rebuildHTTPClientForRetry(fmt.Sprintf("%s proxy unauthorized retry", label), lastErr); rotateErr != nil && m.log != nil {
				m.log.ManagerError("HTTP client rebuild failed: " + rotateErr.Error())
			}
			continue
		}
		if err != nil && isClosedSessionError(err) && attempt < requestRetryAttempts {
			rebuildErr := m.rebuildHTTPClientSameProxy(fmt.Sprintf("%s closed session retry", label))
			if rebuildErr != nil {
				if m.log != nil {
					m.log.ManagerError("HTTP client same-proxy rebuild failed: " + rebuildErr.Error())
				}
				rotateErr := m.rotateHTTPClientProxy(fmt.Sprintf("%s closed session proxy fallback", label))
				if rotateErr != nil {
					if m.log != nil {
						m.log.ManagerError("HTTP client proxy fallback failed: " + rotateErr.Error())
					}
					return nil, fmt.Errorf("%s closed session recovery failed: same-proxy rebuild: %w; proxy fallback: %v", label, rebuildErr, rotateErr)
				}
			}
			logRetryAttempt(m, fmt.Sprintf("%s closed session", label), attempt, err.Error())
			continue
		}
		if err != nil && isStripeProxyForbidden(requestURL, err) && !switchedStripeDirect {
			switchStripeToDirect(m)
			switchedStripeDirect = true
		}
		if attempt < requestRetryAttempts && shouldRotateProxyForRetry(err, res) {
			rotateErr := m.rebuildHTTPClientForRetry(fmt.Sprintf("%s retry", label), err)
			if rotateErr != nil && m.log != nil {
				m.log.ManagerError("HTTP client rebuild failed: " + rotateErr.Error())
			}
		}
		if err != nil {
			logRetryAttempt(m, fmt.Sprintf("%s failed", label), attempt, err.Error())
		} else {
			logRetryAttempt(m, fmt.Sprintf("%s returned %s", label, res.Status), attempt, "")
		}
		if attempt == requestRetryAttempts {
			break
		}
		time.Sleep(requestRetryDelay)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return lastResponse, nil
}

func (m *Module) postRequestWithRetry(label string, url string, body []byte, header http.Header) (*httpResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("%s request failed: client session is not available", label)
	}
	if m.client == nil {
		if err := m.rebuildHTTPClientSameProxy(fmt.Sprintf("%s missing client recovery", label)); err != nil {
			return nil, fmt.Errorf("%s request failed: recover missing client: %w", label, err)
		}
	}

	var lastErr error
	var lastResponse *httpResponse
	for attempt := 1; attempt <= proxyUnauthorizedRetryAttempts; attempt++ {
		if m.client == nil {
			if err := m.rebuildHTTPClientSameProxy(fmt.Sprintf("%s missing client retry", label)); err != nil {
				return nil, fmt.Errorf("%s request failed: recover missing client retry: %w", label, err)
			}
		}
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
		if err != nil {
			return nil, fmt.Errorf("%s request creation failed: %w", label, err)
		}
		req.Header = header
		rawResp, err := m.client.Do(req)
		var res *httpResponse
		if err == nil {
			res, err = readResponse(rawResp)
		}
		if err == nil {
			m.autosaveSessionIfChanged(label)
		}
		proxyUnauthorized := isProxyUnauthorized(err, res)
		if err == nil && !proxyUnauthorized && !shouldRetryStatus(res) {
			return res, nil
		}
		lastErr = err
		lastResponse = res
		if proxyUnauthorized {
			lastErr = proxyUnauthorizedError(label, err, res)
			if attempt >= proxyUnauthorizedRetryAttempts {
				break
			}
			delay := m.proxyUnauthorizedRetryDelay()
			logProxyUnauthorizedRetry(m, label, attempt, proxyUnauthorizedDetail(err, res), delay)
			if delay > 0 {
				time.Sleep(delay)
			}
			if rotateErr := m.rebuildHTTPClientForRetry(fmt.Sprintf("%s proxy unauthorized retry", label), lastErr); rotateErr != nil && m.log != nil {
				m.log.ManagerError("HTTP client rebuild failed: " + rotateErr.Error())
			}
			continue
		}
		if err != nil && isClosedSessionError(err) && attempt < requestRetryAttempts {
			rebuildErr := m.rebuildHTTPClientSameProxy(fmt.Sprintf("%s closed session retry", label))
			if rebuildErr != nil {
				if m.log != nil {
					m.log.ManagerError("HTTP client same-proxy rebuild failed: " + rebuildErr.Error())
				}
				rotateErr := m.rotateHTTPClientProxy(fmt.Sprintf("%s closed session proxy fallback", label))
				if rotateErr != nil {
					if m.log != nil {
						m.log.ManagerError("HTTP client proxy fallback failed: " + rotateErr.Error())
					}
					return nil, fmt.Errorf("%s closed session recovery failed: same-proxy rebuild: %w; proxy fallback: %v", label, rebuildErr, rotateErr)
				}
			}
			logRetryAttempt(m, fmt.Sprintf("%s closed session", label), attempt, err.Error())
			continue
		}
		if attempt < requestRetryAttempts && shouldRotateProxyForRetry(err, res) {
			rotateErr := m.rebuildHTTPClientForRetry(fmt.Sprintf("%s retry", label), err)
			if rotateErr != nil && m.log != nil {
				m.log.ManagerError("HTTP client rebuild failed: " + rotateErr.Error())
			}
		}
		if err != nil {
			logRetryAttempt(m, fmt.Sprintf("%s failed", label), attempt, err.Error())
		} else {
			logRetryAttempt(m, fmt.Sprintf("%s returned %s", label, res.Status), attempt, "")
		}
		if attempt == requestRetryAttempts {
			break
		}
		time.Sleep(requestRetryDelay)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return lastResponse, nil
}

func shouldRetryStatus(res *httpResponse) bool {
	if res == nil {
		return false
	}
	return res.StatusCode == 407 || res.StatusCode == 408 || res.StatusCode == 410 || res.StatusCode == 425 || res.StatusCode == 429 || res.StatusCode >= 500
}

func isProxyUnauthorized(err error, res *httpResponse) bool {
	if res != nil && res.StatusCode == 407 {
		return true
	}
	text := strings.ToLower(proxyUnauthorizedDetail(err, res))
	for _, needle := range []string{
		"proxy unauthorized",
		"proxy authentication required",
		"proxy authenticate",
		"proxy-authenticate",
		"407 proxy",
		"407 unauthorized",
		"proxy auth",
		"proxy tunnel failed: 407",
		"proxy tunnel returned 407",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func proxyUnauthorizedDetail(err error, res *httpResponse) string {
	parts := []string{}
	if err != nil {
		parts = append(parts, err.Error())
	}
	if res != nil {
		parts = append(parts, res.Status)
		if res.StatusCode == 407 {
			parts = append(parts, "407 proxy authentication required")
		}
	}
	return strings.Join(parts, " ")
}

func proxyUnauthorizedError(label string, err error, res *httpResponse) error {
	detail := strings.TrimSpace(proxyUnauthorizedDetail(err, res))
	if detail == "" {
		detail = "proxy unauthorized"
	}
	return fmt.Errorf("%s proxy unauthorized: %s", label, detail)
}

func (m *Module) proxyUnauthorizedRetryDelay() time.Duration {
	if m != nil && m.fastProxyAuthRetry {
		return 0
	}
	return proxyUnauthorizedRetryDelay
}

func logProxyUnauthorizedRetry(m *Module, label string, attempt int, detail string, delay time.Duration) {
	if m == nil || m.log == nil {
		return
	}
	message := fmt.Sprintf("%s proxy unauthorized (%d/%d): %s.", label, attempt, proxyUnauthorizedRetryAttempts, strings.TrimSpace(detail))
	if delay > 0 {
		message += fmt.Sprintf(" Waiting %.0fs before retry.", delay.Seconds())
	} else {
		message += " Rotating proxy immediately."
	}
	m.log.ManagerNotice(message)
}

func isClosedSessionError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, needle := range []string{
		"session is closed",
		"session closed",
		"client session is closed",
		"client session closed",
		"session has been closed",
		"use of closed network connection",
		"client connection is closed",
		"client connection closed",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func shouldRotateProxyForRetry(err error, res *httpResponse) bool {
	if err != nil {
		text := strings.ToLower(err.Error())
		for _, needle := range []string{
			"410 gone",
			"proxy tunnel",
			"failed to dial",
			"dial tcp",
			"no such host",
			"dns",
			"connect: connection",
			"connection refused",
			"connection reset",
			"session is closed",
			"session closed",
			"client session is closed",
			"client session closed",
			"session has been closed",
			"use of closed network connection",
			"timeout",
			"tls",
			"eof",
		} {
			if strings.Contains(text, needle) {
				return true
			}
		}
	}
	if res == nil {
		return false
	}
	return res.StatusCode == 408 || res.StatusCode == 410 || res.StatusCode == 425 || res.StatusCode == 429 || res.StatusCode >= 500
}

func logRetryAttempt(m *Module, prefix string, attempt int, detail string) {
	if m == nil || m.log == nil {
		return
	}
	message := fmt.Sprintf("%s (%d/%d)", prefix, attempt, requestRetryAttempts)
	if strings.TrimSpace(detail) != "" {
		message += ": " + detail
	}
	if attempt < requestRetryAttempts {
		message += ". Retrying in 10s."
	} else {
		message += ". No retries left."
	}
	m.log.ManagerNotice(message)
}

func isStripeProxyForbidden(rawURL string, err error) bool {
	if err == nil || !strings.Contains(strings.ToLower(rawURL), "stripe.com") {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "proxy tunnel failed") && strings.Contains(text, "403")
}

func switchStripeToDirect(m *Module) {
	if m == nil || m.client == nil {
		return
	}
	if m.config != nil && m.config.HyperDebug {
		if m.log != nil {
			m.log.ManagerNotice("Stripe proxy tunnel returned 403; hyper_debug is on, keeping local debug proxy.")
		}
		return
	}
	_ = m.client.SetProxy("")
	if m.log != nil {
		m.log.ManagerNotice("Stripe proxy tunnel returned 403; using local/direct connection for Stripe only.")
	}
}

func (s *KasadaSolver) doRequestWithRetry(request *http.Request) (*httpResponse, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("kasada request failed: client session is not available")
	}
	if request == nil {
		return nil, fmt.Errorf("kasada request failed: request is nil")
	}

	var lastErr error
	var lastResponse *httpResponse
	requestURL := request.URL.String()
	for attempt := 1; attempt <= proxyUnauthorizedRetryAttempts; attempt++ {
		cloned := cloneRequest(request)
		s.debugRequest(kasadaDebugRequestLabel(requestURL, attempt), cloned)
		rawResp, err := s.session.Do(cloned)
		var res *httpResponse
		if err == nil {
			res, err = readResponse(rawResp)
		}
		s.debugResponse(kasadaDebugRequestLabel(requestURL, attempt), res, err)
		proxyUnauthorized := isProxyUnauthorized(err, res)
		if err == nil && !proxyUnauthorized && !shouldRetryKasadaStatus(res) {
			return res, nil
		}
		lastErr = err
		lastResponse = res
		if proxyUnauthorized {
			lastErr = proxyUnauthorizedError("kasada", err, res)
			if attempt >= proxyUnauthorizedRetryAttempts {
				break
			}
			time.Sleep(proxyUnauthorizedRetryDelay)
			continue
		}
		if attempt >= kasadaRetryAttempts {
			break
		}
		time.Sleep(requestRetryDelay)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return lastResponse, nil
}

func shouldRetryKasadaStatus(res *httpResponse) bool {
	if res == nil {
		return false
	}
	if res.StatusCode == 403 || res.StatusCode == 429 {
		return false
	}
	return res.StatusCode == 407 || res.StatusCode == 408 || res.StatusCode == 410 || res.StatusCode == 425 || res.StatusCode >= 500
}

func cloneRequest(req *http.Request) *http.Request {
	clone := req.Clone(req.Context())
	if req.Body != nil {
		clone.Body = req.Body
	}
	return clone
}
