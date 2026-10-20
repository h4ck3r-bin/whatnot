package smspool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.smspool.net/stubs/handler_api"

type Client struct {
	APIKey        string
	BaseURL       string
	HTTP          *http.Client
	UseSMSPoolIDs bool // adds setting=smspool
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.BaseURL = u }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.HTTP = h
		}
	}
}

func WithSMSPoolIDs(v bool) Option {
	return func(c *Client) { c.UseSMSPoolIDs = v }
}

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		APIKey:        strings.TrimSpace(apiKey),
		BaseURL:       DefaultBaseURL,
		UseSMSPoolIDs: true,
		HTTP:          &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Activation struct {
	ID     string // IMPORTANT: SMSPool may return alphanumeric IDs (e.g. EF2BV38H)
	Number string
	Raw    string
}

type ClaimOptions struct {
	Pool     *int
	MaxPrice *int
}

// ClaimNumber returns activation ID + phone number immediately.
func (c *Client) ClaimNumber(ctx context.Context, service string, countryID int, opt *ClaimOptions) (*Activation, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, errors.New("service is required")
	}
	if countryID < 0 {
		return nil, errors.New("countryID must be >= 0")
	}
	if c.APIKey == "" {
		return nil, errors.New("api key is required")
	}

	q := url.Values{}
	q.Set("action", "getNumber")
	q.Set("api_key", c.APIKey)
	q.Set("service", service)
	q.Set("country", strconv.Itoa(countryID))
	if c.UseSMSPoolIDs {
		q.Set("setting", "smspool")
	}
	if opt != nil {
		if opt.Pool != nil {
			q.Set("pool", strconv.Itoa(*opt.Pool))
		}
		if opt.MaxPrice != nil {
			q.Set("maxPrice", strconv.Itoa(*opt.MaxPrice))
		}
	}
	raw, err := c.do(ctx, q)
	if err != nil {
		return nil, err
	}

	// sms-activate style: ACCESS_NUMBER:<id>:<number>
	if strings.HasPrefix(raw, "ACCESS_NUMBER:") {
		rest := strings.TrimPrefix(raw, "ACCESS_NUMBER:")
		parts := strings.Split(rest, ":")
		if len(parts) < 2 {
			return nil, &APIError{Op: "getNumber", Raw: raw}
		}
		id := strings.TrimSpace(parts[0])
		num := strings.TrimSpace(parts[1])
		if id == "" || num == "" {
			return nil, &APIError{Op: "getNumber", Raw: raw}
		}
		return &Activation{ID: id, Number: num, Raw: raw}, nil
	}

	return nil, &APIError{Op: "getNumber", Raw: raw}
}

func (c *Client) GetStatus(ctx context.Context, activationID string) (string, error) {
	activationID = strings.TrimSpace(activationID)
	if activationID == "" {
		return "", errors.New("activationID is required")
	}
	q := url.Values{}
	q.Set("action", "getStatus")
	q.Set("api_key", c.APIKey)
	q.Set("id", activationID)
	if c.UseSMSPoolIDs {
		q.Set("setting", "smspool")
	}
	return c.do(ctx, q)
}

// GetCode polls until STATUS_OK:<code> or ctx deadline.
func (c *Client) GetCode(ctx context.Context, activationID string, pollEvery time.Duration) (string, error) {
	activationID = strings.TrimSpace(activationID)
	if activationID == "" {
		return "", errors.New("activationID is required")
	}
	if pollEvery <= 0 {
		pollEvery = 3 * time.Second
	}

	t := time.NewTicker(pollEvery)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			if code, ok := c.checkCodeOneLastTime(activationID); ok {
				_, _ = c.SetStatus(context.Background(), activationID, 6)
				return code, nil
			}
			_, _ = c.SetStatus(context.Background(), activationID, 8)
			return "", ctx.Err()
		case <-t.C:
			st, err := c.GetStatus(ctx, activationID)
			if err != nil {
				// transient network issue; keep polling until timeout
				continue
			}

			if strings.HasPrefix(st, "STATUS_OK:") {
				code := strings.TrimPrefix(st, "STATUS_OK:")
				_, _ = c.SetStatus(context.Background(), activationID, 6)
				return code, nil
			}

			if strings.HasPrefix(st, "STATUS_CANCEL") ||
				strings.HasPrefix(st, "NO_ACTIVATION") ||
				strings.HasPrefix(st, "BAD_KEY") ||
				strings.HasPrefix(st, "BAD_ACTION") {
				_, _ = c.SetStatus(context.Background(), activationID, 8)
				return "", &APIError{Op: "getStatus", Raw: st}
			}
		}
	}
}

func (c *Client) checkCodeOneLastTime(activationID string) (string, bool) {
	checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := c.GetStatus(checkCtx, activationID)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(st, "STATUS_OK:") {
		code := strings.TrimSpace(strings.TrimPrefix(st, "STATUS_OK:"))
		return code, code != ""
	}
	return "", false
}

func (c *Client) SetStatus(ctx context.Context, activationID string, status int) (string, error) {
	activationID = strings.TrimSpace(activationID)
	if activationID == "" {
		return "", errors.New("activationID is required")
	}
	q := url.Values{}
	q.Set("action", "setStatus")
	q.Set("api_key", c.APIKey)
	q.Set("id", activationID)
	q.Set("status", strconv.Itoa(status))
	if c.UseSMSPoolIDs {
		q.Set("setting", "smspool")
	}
	return c.do(ctx, q)
}

func (c *Client) do(ctx context.Context, q url.Values) (string, error) {
	u := c.BaseURL
	if strings.Contains(u, "?") {
		u += "&" + q.Encode()
	} else {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(b))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, raw)
	}
	return raw, nil
}
