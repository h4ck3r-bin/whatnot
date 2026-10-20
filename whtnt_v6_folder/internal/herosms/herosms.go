package herosms

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

const DefaultBaseURL = "https://hero-sms.com/stubs/handler_api.php"

// Client talks to a sms-activate-compatible API (HeroSMS supports this protocol).
type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		APIKey:  apiKey,
		BaseURL: DefaultBaseURL,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.BaseURL = baseURL }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.HTTP = h
		}
	}
}

// Activation is the result of requesting a number.
type Activation struct {
	ID     int64
	Number string
	Raw    string
}

// GetNumberOptions lets you pass additional optional parameters that many providers support.
// Anything non-empty will be added to the query string.
type GetNumberOptions struct {
	Forward        *int   // optional
	Operator       string // optional
	Ref            string // optional
	PhoneException string // optional
	MaxPrice       *int   // optional (some providers)
	Verification   *int   // optional (some providers)
}

func (c *Client) GetNumber(ctx context.Context, service string, country int, opt *GetNumberOptions) (*Activation, error) {
	if service == "" {
		return nil, errors.New("service is required")
	}
	q := url.Values{}
	q.Set("action", "getNumber")
	q.Set("api_key", c.APIKey)
	q.Set("service", service)
	q.Set("country", strconv.Itoa(country))

	if opt != nil {
		if opt.Forward != nil {
			q.Set("forward", strconv.Itoa(*opt.Forward))
		}
		if opt.Operator != "" {
			q.Set("operator", opt.Operator)
		}
		if opt.Ref != "" {
			q.Set("ref", opt.Ref)
		}
		if opt.PhoneException != "" {
			q.Set("phoneException", opt.PhoneException)
		}
		if opt.MaxPrice != nil {
			q.Set("maxPrice", strconv.Itoa(*opt.MaxPrice))
		}
		if opt.Verification != nil {
			q.Set("verification", strconv.Itoa(*opt.Verification))
		}
	}

	raw, err := c.do(ctx, q)
	if err != nil {
		return nil, err
	}
	// Standard success format: ACCESS_NUMBER:<id>:<number>
	// Errors: NO_NUMBERS, NO_BALANCE, BAD_KEY, BAD_ACTION, etc.
	if strings.HasPrefix(raw, "ACCESS_NUMBER:") {
		rest := strings.TrimPrefix(raw, "ACCESS_NUMBER:")
		parts := strings.Split(rest, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("unexpected ACCESS_NUMBER format: %q", raw)
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid activation id: %w (raw=%q)", err, raw)
		}
		number := parts[1]
		return &Activation{ID: id, Number: number, Raw: raw}, nil
	}

	return nil, apiError(raw)
}

// SetStatus sets activation status.
// Common codes (provider dependent): 1=ready, 3=request another code, 6=finish, 8=cancel. :contentReference[oaicite:2]{index=2}
func (c *Client) SetStatus(ctx context.Context, id int64, status int) (string, error) {
	q := url.Values{}
	q.Set("action", "setStatus")
	q.Set("api_key", c.APIKey)
	q.Set("id", strconv.FormatInt(id, 10))
	q.Set("status", strconv.Itoa(status))

	raw, err := c.do(ctx, q)
	if err != nil {
		return "", err
	}
	// Success responses include: ACCESS_READY, ACCESS_RETRY_GET, ACCESS_ACTIVATION, ACCESS_CANCEL... :contentReference[oaicite:3]{index=3}
	if strings.HasPrefix(raw, "ACCESS_") {
		return raw, nil
	}
	return "", apiError(raw)
}

// StatusState is a normalized status.
type StatusState string

const (
	StatusWaitCode   StatusState = "WAIT_CODE"
	StatusOk         StatusState = "OK"
	StatusCancel     StatusState = "CANCEL"
	StatusWaitRetry  StatusState = "WAIT_RETRY"
	StatusWaitResend StatusState = "WAIT_RESEND"
	StatusUnknown    StatusState = "UNKNOWN"
)

// Status is the result of getStatus.
type Status struct {
	State   StatusState
	Code    string // present when State == OK
	Raw     string
	PrevMsg string // e.g. previous code/message for WAIT_RETRY
}

// GetStatus fetches activation status. :contentReference[oaicite:4]{index=4}
func (c *Client) GetStatus(ctx context.Context, id int64) (*Status, error) {
	q := url.Values{}
	q.Set("action", "getStatus")
	q.Set("api_key", c.APIKey)
	q.Set("id", strconv.FormatInt(id, 10))

	raw, err := c.do(ctx, q)
	if err != nil {
		return nil, err
	}

	switch {
	case raw == "STATUS_WAIT_CODE":
		return &Status{State: StatusWaitCode, Raw: raw}, nil
	case raw == "STATUS_CANCEL":
		return &Status{State: StatusCancel, Raw: raw}, nil
	case raw == "STATUS_WAIT_RESEND":
		return &Status{State: StatusWaitResend, Raw: raw}, nil
	case strings.HasPrefix(raw, "STATUS_OK:"):
		code := strings.TrimPrefix(raw, "STATUS_OK:")
		code = strings.TrimSpace(code)
		return &Status{State: StatusOk, Code: code, Raw: raw}, nil
	case strings.HasPrefix(raw, "STATUS_WAIT_RETRY:"):
		msg := strings.TrimPrefix(raw, "STATUS_WAIT_RETRY:")
		msg = strings.TrimSpace(msg)
		return &Status{State: StatusWaitRetry, PrevMsg: msg, Raw: raw}, nil
	default:
		// could be an error like NO_ACTIVATION, BAD_KEY...
		if looksLikeError(raw) {
			return nil, apiError(raw)
		}
		return &Status{State: StatusUnknown, Raw: raw}, nil
	}
}

// WaitForCode polls until STATUS_OK:<code>, or context is done.
// If timeout/cancel happens, it attempts to cancel the activation (status=8) best-effort. :contentReference[oaicite:5]{index=5}
func (c *Client) WaitForCode(ctx context.Context, id int64, pollEvery time.Duration) (string, *Status, error) {
	if pollEvery <= 0 {
		pollEvery = 3 * time.Second
	}

	t := time.NewTicker(pollEvery)
	defer t.Stop()

	for {

		st, err := c.GetStatus(ctx, id)
		if err != nil {
			return "", nil, err
		}
		if st.State == StatusOk && st.Code != "" {
			return st.Code, st, nil
		}
		if st.State == StatusCancel {
			return "", st, fmt.Errorf("activation cancelled (id=%d)", id)
		}

		select {
		case <-ctx.Done():
			// best-effort cancel
			_, _ = c.SetStatus(context.Background(), id, 8)
			return "", st, ctx.Err()
		case <-t.C:
		}
	}
}

func (c *Client) do(ctx context.Context, q url.Values) (string, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, raw)
	}
	return raw, nil
}

func looksLikeError(raw string) bool {
	switch raw {
	case "BAD_KEY", "BAD_ACTION", "BAD_SERVICE", "WRONG_SERVICE", "NO_NUMBERS", "NO_BALANCE", "NO_ACTIVATION", "BAD_STATUS", "ERROR_SQL", "YOU_TEMPORARY_BANNED":
		return true
	default:
		// some providers return "ERROR_*"
		return strings.HasPrefix(raw, "ERROR_")
	}
}

func apiError(raw string) error {
	switch raw {
	case "BAD_KEY":
		return errors.New("api error: BAD_KEY (invalid api key)")
	case "NO_BALANCE":
		return errors.New("api error: NO_BALANCE (insufficient balance)")
	case "NO_NUMBERS":
		return errors.New("api error: NO_NUMBERS (no numbers available)")
	case "NO_ACTIVATION":
		return errors.New("api error: NO_ACTIVATION (activation id not found)")
	case "BAD_SERVICE":
		return errors.New("api error: BAD_SERVICE (invalid service)")
	case "WRONG_SERVICE":
		return errors.New("api error: WRONG_SERVICE (service not supported)")
	case "BAD_ACTION":
		return errors.New("api error: BAD_ACTION (invalid action)")
	case "BAD_STATUS":
		return errors.New("api error: BAD_STATUS (invalid status)")
	case "YOU_TEMPORARY_BANNED":
		return errors.New("api error: YOU_TEMPORARY_BANNED")
	case "ERROR_SQL":
		return errors.New("api error: ERROR_SQL")
	default:
		return fmt.Errorf("api error: %s", raw)
	}
}

// RequestAndWaitForCode requests a number, marks it ready (status=1), then polls until a code arrives.
// It returns (activationID, phoneNumber, code).
// If it takes longer than 2 minutes, it stops and best-effort cancels the activation (status=8).
func (c *Client) ClaimNumber(
	ctx context.Context,
	service string,
	country int,
	opt *GetNumberOptions,
) (int64, string, error) {
	act, err := c.GetNumber(ctx, service, country, opt)
	if err != nil {
		return 0, "", err
	}

	// Mark as ready to receive SMS (common best practice).
	if _, err := c.SetStatus(ctx, act.ID, 1); err != nil {
		// Best-effort cancel if we couldn't set ready.
		_, _ = c.SetStatus(context.Background(), act.ID, 8)
		return 0, "", err
	}
	return act.ID, act.Number, nil
}

// GetCode waits up to 2 minutes for the SMS code for an activation ID.
// On timeout/cancel, it best-effort cancels the activation (status=8).
// On success, it best-effort finishes the activation (status=6).
func (c *Client) GetCode(
	parent context.Context,
	activationID int64,
) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()

	code, _, err := c.WaitForCode(ctx, activationID, 3*time.Second)
	if err != nil {
		// WaitForCode already best-effort cancels on ctx timeout/cancel,
		// but cancel again just in case.
		_, _ = c.SetStatus(context.Background(), activationID, 8)
		return "", err
	}

	// Best-effort finish activation.
	_, _ = c.SetStatus(context.Background(), activationID, 6)

	return code, nil
}
