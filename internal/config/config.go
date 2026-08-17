// Package config loads and validates FreeSupp runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Defaults applied when the corresponding env var is empty.
const (
	DefaultListen   = ":8080"
	DefaultDBPath   = "/data/freesupp.db"
	DefaultSMTPPort = 587
)

// MinSessionSecretLen is the shortest accepted SESSION_SECRET. A guessable
// secret lets anyone forge an operator session cookie.
const MinSessionSecretLen = 16

// Config holds every setting the server needs.
type Config struct {
	BaseURL string
	Listen  string
	DBPath  string

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	MailFrom     string

	TurnstileSiteKey string
	TurnstileSecret  string

	SessionSecret string

	// UseProxy enables reading the client IP from X-Forwarded-For. Only turn
	// it on when a reverse proxy is the sole way to reach the process.
	UseProxy bool
}

// Getenv reads an environment variable; os.Getenv satisfies it.
type Getenv func(string) string

// SMTPConfigured reports whether outbound email can actually be delivered.
// When false the server falls back to a logging-only sender (dev mode).
func (c *Config) SMTPConfigured() bool { return c.SMTPHost != "" }

// CaptchaConfigured reports whether Turnstile verification is enabled.
func (c *Config) CaptchaConfigured() bool {
	return c.TurnstileSiteKey != "" && c.TurnstileSecret != ""
}

// Load reads configuration from getenv and validates it.
func Load(getenv Getenv) (*Config, error) {
	get := func(key string) string { return strings.TrimSpace(getenv(key)) }

	c := &Config{
		BaseURL:            strings.TrimRight(get("BASE_URL"), "/"),
		Listen:             get("LISTEN"),
		DBPath:             get("DB_PATH"),
		SMTPHost:           get("SMTP_HOST"),
		SMTPUser:           get("SMTP_USER"),
		SMTPPassword:       getenv("SMTP_PASSWORD"),
		MailFrom:           get("MAIL_FROM"),
		TurnstileSiteKey:   get("TURNSTILE_SITE_KEY"),
		TurnstileSecret:    get("TURNSTILE_SECRET"),
		SessionSecret:      get("SESSION_SECRET"),
	}

	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	if c.DBPath == "" {
		c.DBPath = DefaultDBPath
	}

	var errs []error

	port, err := parsePort(get("SMTP_PORT"))
	if err != nil {
		errs = append(errs, err)
	}
	c.SMTPPort = port

	useProxy, err := parseBool("USE_PROXY", get("USE_PROXY"))
	if err != nil {
		errs = append(errs, err)
	}
	c.UseProxy = useProxy

	if c.BaseURL == "" {
		errs = append(errs, missing("BASE_URL", "public URL of this deployment, e.g. https://support.example.com"))
	} else if !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://") {
		errs = append(errs, fmt.Errorf("BASE_URL must start with http:// or https:// (got %q)", c.BaseURL))
	}
	if c.SessionSecret == "" {
		errs = append(errs, missing("SESSION_SECRET", "random string used to sign operator session cookies"))
	} else if len(c.SessionSecret) < MinSessionSecretLen {
		errs = append(errs, fmt.Errorf(
			"SESSION_SECRET must be at least %d characters (generate one with `openssl rand -hex 32`)",
			MinSessionSecretLen))
	} else if isPlaceholder(c.SessionSecret) {
		// The example value ships in docker-compose.yml, so a deployment that
		// kept it signs cookies with a publicly known key: anyone could forge an
		// operator session. Nothing else breaks, so only this check catches it.
		errs = append(errs, errors.New(
			"SESSION_SECRET is still the example placeholder (generate one with `openssl rand -hex 32`)"))
	}
	// MAIL_FROM only matters once email can actually be sent.
	if c.SMTPConfigured() && c.MailFrom == "" {
		errs = append(errs, missing("MAIL_FROM", "sender address for outbound email; required when SMTP_HOST is set"))
	}
	if (c.TurnstileSiteKey == "") != (c.TurnstileSecret == "") {
		errs = append(errs, errors.New("TURNSTILE_SITE_KEY and TURNSTILE_SECRET must be set together (or both left empty to disable the captcha)"))
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  %w", errors.Join(errs...))
	}
	return c, nil
}

func missing(key, hint string) error {
	return fmt.Errorf("%s is required (%s)", key, hint)
}

// isPlaceholder reports whether a value is one of the "change-me" examples
// shipped in docker-compose.yml rather than a real secret.
func isPlaceholder(v string) bool {
	return strings.Contains(strings.ToLower(v), "change-me")
}

// parseBool accepts the usual truthy/falsy spellings; empty means false.
func parseBool(key, raw string) (bool, error) {
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(strings.ToLower(raw))
	if err != nil {
		return false, fmt.Errorf("%s must be true or false (got %q)", key, raw)
	}
	return v, nil
}

func parsePort(raw string) (int, error) {
	if raw == "" {
		return DefaultSMTPPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return DefaultSMTPPort, fmt.Errorf("SMTP_PORT must be a number between 1 and 65535 (got %q)", raw)
	}
	return port, nil
}

