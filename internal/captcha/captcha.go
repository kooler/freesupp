// Package captcha verifies Cloudflare Turnstile tokens submitted by visitors.
package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kooler/freesupp/internal/config"
)

// ErrFailed means the token was rejected by the verification service; the
// visitor should retry the challenge. Any other error is a transport or
// service problem.
var ErrFailed = errors.New("captcha: verification failed")

// SiteVerifyURL is Cloudflare's Turnstile verification endpoint.
const SiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Verifier checks a challenge token. remoteIP may be empty.
type Verifier interface {
	Verify(ctx context.Context, token, remoteIP string) error
}

// New returns a Turnstile verifier when keys are configured, otherwise one
// that accepts everything (local dev).
func New(cfg *config.Config) Verifier {
	if !cfg.CaptchaConfigured() {
		return Disabled{}
	}
	return NewTurnstile(cfg.TurnstileSecret)
}

// Disabled accepts every token; used when Turnstile keys are absent.
type Disabled struct{}

// Verify always succeeds.
func (Disabled) Verify(context.Context, string, string) error { return nil }

// Turnstile calls Cloudflare's siteverify endpoint.
type Turnstile struct {
	Secret   string
	Endpoint string
	Client   *http.Client
}

// NewTurnstile builds a verifier with sane defaults.
func NewTurnstile(secret string) *Turnstile {
	return &Turnstile{
		Secret:   secret,
		Endpoint: SiteVerifyURL,
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
}

type siteVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify posts the token to Turnstile and reports the outcome.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("%w: empty token", ErrFailed)
	}

	form := url.Values{"secret": {t.Secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("captcha: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client().Do(req)
	if err != nil {
		return fmt.Errorf("captcha: siteverify request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("captcha: siteverify returned status %d", resp.StatusCode)
	}

	var out siteVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("captcha: decode siteverify response: %w", err)
	}
	if !out.Success {
		return fmt.Errorf("%w (%s)", ErrFailed, strings.Join(out.ErrorCodes, ", "))
	}
	return nil
}

func (t *Turnstile) endpoint() string {
	if t.Endpoint == "" {
		return SiteVerifyURL
	}
	return t.Endpoint
}

func (t *Turnstile) client() *http.Client {
	if t.Client == nil {
		return http.DefaultClient
	}
	return t.Client
}
