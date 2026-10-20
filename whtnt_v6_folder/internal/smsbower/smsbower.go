package smsbower

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

const (
	DefaultBaseURL = "https://smsbower.page/stubs/handler_api.php"
	DefaultRef     = "505464"
)

// Client talks to SMSBOWER's SMS-Activate-compatible API.
type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
	Ref     string
}

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		APIKey:  apiKey,
		BaseURL: DefaultBaseURL,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
		Ref: DefaultRef,
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

func WithRef(ref string) Option {
	return func(c *Client) {
		if strings.TrimSpace(ref) != "" {
			c.Ref = strings.TrimSpace(ref)
		}
	}
}

type Activation struct {
	ID     int64
	Number string
	Raw    string
}

type GetNumberOptions struct {
	MaxPrice          *int
	MinPrice          *int
	ProviderIDs       string
	ExceptProviderIDs string
	PhoneException    string
	UserID            string
}

func (c *Client) GetNumber(ctx context.Context, service string, country int, opt *GetNumberOptions) (*Activation, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, errors.New("api key is required")
	}
	if strings.TrimSpace(service) == "" {
		return nil, errors.New("service is required")
	}

	q := url.Values{}
	q.Set("action", "getNumber")
	q.Set("api_key", c.APIKey)
	q.Set("service", strings.TrimSpace(service))
	q.Set("country", strconv.Itoa(country))
	q.Set("ref", c.Ref)

	if opt != nil {
		if opt.MaxPrice != nil {
			q.Set("maxPrice", strconv.Itoa(*opt.MaxPrice))
		}
		if opt.MinPrice != nil {
			q.Set("minPrice", strconv.Itoa(*opt.MinPrice))
		}
		if opt.ProviderIDs != "" {
			q.Set("providerIds", opt.ProviderIDs)
		}
		if opt.ExceptProviderIDs != "" {
			q.Set("exceptProviderIds", opt.ExceptProviderIDs)
		}
		if opt.PhoneException != "" {
			q.Set("phoneException", opt.PhoneException)
		}
		if opt.UserID != "" {
			q.Set("userID", opt.UserID)
		}
	}

	raw, err := c.do(ctx, q)
	if err != nil {
		return nil, err
	}
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
		return &Activation{ID: id, Number: parts[1], Raw: raw}, nil
	}
	return nil, apiError(raw)
}

func (c *Client) ClaimNumber(ctx context.Context, service string, country int, opt *GetNumberOptions) (int64, string, error) {
	act, err := c.GetNumber(ctx, service, country, opt)
	if err != nil {
		return 0, "", err
	}
	if _, err := c.SetStatus(ctx, act.ID, 1); err != nil {
		_, _ = c.SetStatus(context.Background(), act.ID, 8)
		return 0, "", err
	}
	return act.ID, act.Number, nil
}

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
	if strings.HasPrefix(raw, "ACCESS_") {
		return raw, nil
	}
	return "", apiError(raw)
}

type StatusState string

const (
	StatusWaitCode   StatusState = "WAIT_CODE"
	StatusOk         StatusState = "OK"
	StatusCancel     StatusState = "CANCEL"
	StatusWaitRetry  StatusState = "WAIT_RETRY"
	StatusWaitResend StatusState = "WAIT_RESEND"
	StatusUnknown    StatusState = "UNKNOWN"
)

type Status struct {
	State   StatusState
	Code    string
	Raw     string
	PrevMsg string
}

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
		code := strings.TrimSpace(strings.TrimPrefix(raw, "STATUS_OK:"))
		return &Status{State: StatusOk, Code: code, Raw: raw}, nil
	case strings.HasPrefix(raw, "STATUS_WAIT_RETRY:"):
		msg := strings.TrimSpace(strings.TrimPrefix(raw, "STATUS_WAIT_RETRY:"))
		return &Status{State: StatusWaitRetry, PrevMsg: msg, Raw: raw}, nil
	default:
		if looksLikeError(raw) {
			return nil, apiError(raw)
		}
		return &Status{State: StatusUnknown, Raw: raw}, nil
	}
}

func (c *Client) WaitForCode(ctx context.Context, id int64, pollEvery time.Duration) (string, *Status, error) {
	if pollEvery <= 0 {
		pollEvery = 3 * time.Second
	}

	t := time.NewTicker(pollEvery)
	defer t.Stop()

	for {
		st, err := c.GetStatus(ctx, id)
		if err != nil {
			if isTerminalStatusError(err) {
				return "", nil, err
			}
			select {
			case <-ctx.Done():
				code, finalStatus, ok := c.checkCodeOneLastTime(id)
				if ok {
					return code, finalStatus, nil
				}
				_, _ = c.SetStatus(context.Background(), id, 8)
				return "", finalStatus, ctx.Err()
			case <-t.C:
				continue
			}
		}
		if st.State == StatusOk && st.Code != "" {
			return st.Code, st, nil
		}
		if st.State == StatusCancel {
			return "", st, fmt.Errorf("activation cancelled (id=%d)", id)
		}

		select {
		case <-ctx.Done():
			code, finalStatus, ok := c.checkCodeOneLastTime(id)
			if ok {
				return code, finalStatus, nil
			}
			_, _ = c.SetStatus(context.Background(), id, 8)
			if finalStatus != nil {
				return "", finalStatus, ctx.Err()
			}
			return "", st, ctx.Err()
		case <-t.C:
		}
	}
}

func (c *Client) checkCodeOneLastTime(id int64) (string, *Status, bool) {
	checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := c.GetStatus(checkCtx, id)
	if err != nil {
		return "", nil, false
	}
	if st.State == StatusOk && st.Code != "" {
		return st.Code, st, true
	}
	return "", st, false
}

func isTerminalStatusError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, needle := range []string{
		"bad_key",
		"bad_action",
		"bad_service",
		"wrong_service",
		"bad_country",
		"no_activation",
		"bad_status",
		"no_balance",
		"api key is required",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func (c *Client) GetCode(parent context.Context, activationID int64) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()

	code, _, err := c.WaitForCode(ctx, activationID, 3*time.Second)
	if err != nil {
		return "", err
	}

	_, _ = c.SetStatus(context.Background(), activationID, 6)
	return code, nil
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
	case "BAD_KEY", "BAD_ACTION", "BAD_SERVICE", "WRONG_SERVICE", "BAD_COUNTRY", "NO_NUMBERS", "NO_BALANCE", "NO_ACTIVATION", "BAD_STATUS", "EARLY_CANCEL_DENIED", "ERROR_SQL", "YOU_TEMPORARY_BANNED":
		return true
	default:
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
	case "BAD_COUNTRY":
		return errors.New("api error: BAD_COUNTRY (invalid country)")
	case "BAD_ACTION":
		return errors.New("api error: BAD_ACTION (invalid action)")
	case "BAD_STATUS":
		return errors.New("api error: BAD_STATUS (invalid status)")
	case "EARLY_CANCEL_DENIED":
		return errors.New("api error: EARLY_CANCEL_DENIED")
	case "YOU_TEMPORARY_BANNED":
		return errors.New("api error: YOU_TEMPORARY_BANNED")
	case "ERROR_SQL":
		return errors.New("api error: ERROR_SQL")
	default:
		return fmt.Errorf("api error: %s", raw)
	}
}
