package smsmanager

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/herosms"
	"rnldo.dev/wnshell/internal/smsbower"
	"rnldo.dev/wnshell/internal/smsonline"
	"rnldo.dev/wnshell/internal/smspool"
)

type Config struct {
	Provider string // "herosms", "smspool", "smsbower", or "smsonline"

	HeroSMSAPIKey   string
	SMSPoolAPIKey   string
	SMSBowerAPIKey  string
	SMSOnlineAPIKey string

	HTTPClient *http.Client

	SMSPoolPool     *int
	SMSPoolMaxPrice *int

	CodeTimeout time.Duration
	PollEvery   time.Duration
}

// Client is session-like: provider choice fixed per instance.
type Client struct {
	provider string

	hero   *herosms.Client
	pool   *smspool.Client
	bower  *smsbower.Client
	online *smsonline.Client

	resolver *CountryResolver

	codeTimeout time.Duration
	pollEvery   time.Duration

	smspoolPool     *int
	smspoolMaxPrice *int
}

func NewClient(cfg Config) *Client {
	p := normalizeProvider(cfg.Provider)

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	codeTimeout := cfg.CodeTimeout
	if codeTimeout <= 0 {
		codeTimeout = 2 * time.Minute
	}

	pollEvery := cfg.PollEvery
	if pollEvery <= 0 {
		pollEvery = 3 * time.Second
	}

	return &Client{
		provider: p,

		hero: herosms.NewClient(cfg.HeroSMSAPIKey, herosms.WithHTTPClient(httpClient)),
		pool: smspool.NewClient(cfg.SMSPoolAPIKey,
			smspool.WithHTTPClient(httpClient),
			smspool.WithSMSPoolIDs(true),
		),
		bower:  smsbower.NewClient(cfg.SMSBowerAPIKey, smsbower.WithHTTPClient(httpClient)),
		online: smsonline.NewClient(cfg.SMSOnlineAPIKey, smsonline.WithHTTPClient(httpClient)),

		resolver: NewCountryResolver(cfg.SMSPoolAPIKey, httpClient),

		codeTimeout: codeTimeout,
		pollEvery:   pollEvery,

		smspoolPool:     cfg.SMSPoolPool,
		smspoolMaxPrice: cfg.SMSPoolMaxPrice,
	}
}

func normalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "sms_bower", "sms-bower", "sms bower":
		return "smsbower"
	case "sms_online", "sms-online", "sms online", "smsonlinepro", "sms-online-pro", "sms_online_pro":
		return "smsonline"
	default:
		return p
	}
}

// ClaimNumber returns activationID + phoneNumber immediately.
// activationID is string to support SMSPool's alphanumeric IDs.
func (c *Client) ClaimNumber(ctx context.Context, countryISO string, _ any) (activationID string, phoneNumber string, err error) {

	switch c.provider {
	case "herosms":
		cid, e := c.resolver.HeroSMSCountryID(countryISO)
		if e != nil {
			return "", "", wrap("herosms", "ClaimNumber", e)
		}

		actID64, num, e := c.hero.ClaimNumber(ctx, "bex", cid, nil)
		if e != nil {
			return "", "", wrap("herosms", "ClaimNumber", e)
		}
		return strconv.FormatInt(actID64, 10), num, nil

	case "smsbower", "sms_bower":
		cid, e := c.resolver.SMSBowerCountryID(countryISO)
		if e != nil {
			return "", "", wrap("smsbower", "ClaimNumber", e)
		}

		actID64, num, e := c.bower.ClaimNumber(ctx, "ew", cid, nil)
		if e != nil {
			return "", "", wrap("smsbower", "ClaimNumber", e)
		}
		return strconv.FormatInt(actID64, 10), num, nil

	case "smsonline":
		cid, e := c.resolver.SMSOnlineCountryID(countryISO)
		if e != nil {
			return "", "", wrap("smsonline", "ClaimNumber", e)
		}

		actID64, num, e := c.online.ClaimNumber(ctx, "bex", cid, nil)
		if e != nil {
			return "", "", wrap("smsonline", "ClaimNumber", e)
		}
		return strconv.FormatInt(actID64, 10), num, nil

	case "smspool":
		cid, e := c.resolver.SMSPoolCountryID(ctx, countryISO)
		if e != nil {
			return "", "", wrap("smspool", "ClaimNumber", e)
		}

		act, e := c.pool.ClaimNumber(ctx, "1241", cid, &smspool.ClaimOptions{
			Pool:     c.smspoolPool,
			MaxPrice: c.smspoolMaxPrice,
		})
		if e != nil {
			return "", "", wrap("smspool", "ClaimNumber", e)
		}
		return act.ID, act.Number, nil

	default:
		return "", "", msg(c.provider, "ClaimNumber", `unknown provider (expected "herosms", "smspool", "smsbower", or "smsonline")`)
	}
}

// GetCode fetches SMS code for a previously claimed activationID (string).
func (c *Client) GetCode(ctx context.Context, activationID string) (string, error) {
	activationID = strings.TrimSpace(activationID)
	if activationID == "" {
		return "", msg(c.provider, "GetCode", "activationID is required")
	}

	switch c.provider {
	case "herosms":
		id64, err := strconv.ParseInt(activationID, 10, 64)
		if err != nil || id64 <= 0 {
			return "", msg("herosms", "GetCode", "invalid activationID for herosms (expected numeric)")
		}
		waitCtx := c.waitContext(ctx)
		code, _, err := c.hero.WaitForCode(waitCtx.ctx, id64, c.pollEvery)
		waitCtx.cancel()
		if err != nil {
			return "", wrap("herosms", "GetCode", err)
		}
		_, _ = c.hero.SetStatus(context.Background(), id64, 6)
		return code, nil

	case "smsbower", "sms_bower":
		id64, err := strconv.ParseInt(activationID, 10, 64)
		if err != nil || id64 <= 0 {
			return "", msg("smsbower", "GetCode", "invalid activationID for smsbower (expected numeric)")
		}
		waitCtx := c.waitContext(ctx)
		code, _, err := c.bower.WaitForCode(waitCtx.ctx, id64, c.pollEvery)
		waitCtx.cancel()
		if err != nil {
			return "", wrap("smsbower", "GetCode", err)
		}
		_, _ = c.bower.SetStatus(context.Background(), id64, 6)
		return code, nil

	case "smsonline":
		id64, err := strconv.ParseInt(activationID, 10, 64)
		if err != nil || id64 <= 0 {
			return "", msg("smsonline", "GetCode", "invalid activationID for smsonline (expected numeric)")
		}
		waitCtx := c.waitContext(ctx)
		code, _, err := c.online.WaitForCode(waitCtx.ctx, id64, c.pollEvery)
		waitCtx.cancel()
		if err != nil {
			return "", wrap("smsonline", "GetCode", err)
		}
		_, _ = c.online.SetStatus(context.Background(), id64, 6)
		return code, nil

	case "smspool":
		waitCtx := c.waitContext(ctx)
		code, err := c.pool.GetCode(waitCtx.ctx, activationID, c.pollEvery)
		waitCtx.cancel()
		if err != nil {
			return "", wrap("smspool", "GetCode", err)
		}
		return code, nil

	default:
		return "", msg(c.provider, "GetCode", `unknown provider (expected "herosms", "smspool", "smsbower", or "smsonline")`)
	}
}

type smsWaitContext struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *Client) waitContext(parent context.Context) smsWaitContext {
	if c.codeTimeout <= 0 {
		return smsWaitContext{ctx: parent, cancel: func() {}}
	}
	ctx, cancel := context.WithTimeout(parent, c.codeTimeout)
	return smsWaitContext{ctx: ctx, cancel: cancel}
}

func (c *Client) CancelActivation(ctx context.Context, activationID string) error {
	activationID = strings.TrimSpace(activationID)
	if activationID == "" {
		return nil
	}

	switch c.provider {
	case "herosms":
		id64, err := strconv.ParseInt(activationID, 10, 64)
		if err != nil || id64 <= 0 {
			return msg("herosms", "CancelActivation", "invalid activationID for herosms (expected numeric)")
		}
		if _, err := c.hero.SetStatus(ctx, id64, 8); err != nil {
			return wrap("herosms", "CancelActivation", err)
		}
		return nil

	case "smsbower":
		id64, err := strconv.ParseInt(activationID, 10, 64)
		if err != nil || id64 <= 0 {
			return msg("smsbower", "CancelActivation", "invalid activationID for smsbower (expected numeric)")
		}
		if _, err := c.bower.SetStatus(ctx, id64, 8); err != nil {
			return wrap("smsbower", "CancelActivation", err)
		}
		return nil

	case "smsonline":
		id64, err := strconv.ParseInt(activationID, 10, 64)
		if err != nil || id64 <= 0 {
			return msg("smsonline", "CancelActivation", "invalid activationID for smsonline (expected numeric)")
		}
		if _, err := c.online.SetStatus(ctx, id64, 8); err != nil {
			return wrap("smsonline", "CancelActivation", err)
		}
		return nil

	case "smspool":
		if _, err := c.pool.SetStatus(ctx, activationID, 8); err != nil {
			return wrap("smspool", "CancelActivation", err)
		}
		return nil

	default:
		return msg(c.provider, "CancelActivation", `unknown provider (expected "herosms", "smspool", "smsbower", or "smsonline")`)
	}
}
