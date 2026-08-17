package config

import (
	"strings"
	"testing"
)

// validEnv returns the minimum environment that produces a valid config.
func validEnv() map[string]string {
	return map[string]string{
		"BASE_URL":       "https://support.example.com",
		"SESSION_SECRET": "s3cret-long-enough-for-hmac",
	}
}

func getenvFrom(env map[string]string) Getenv {
	return func(k string) string { return env[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(getenvFrom(validEnv()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("Listen = %q, want %q", cfg.Listen, DefaultListen)
	}
	if cfg.DBPath != DefaultDBPath {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, DefaultDBPath)
	}
	if cfg.SMTPPort != DefaultSMTPPort {
		t.Errorf("SMTPPort = %d, want %d", cfg.SMTPPort, DefaultSMTPPort)
	}
	if cfg.SMTPConfigured() {
		t.Error("SMTPConfigured() = true, want false without SMTP_HOST")
	}
	if cfg.CaptchaConfigured() {
		t.Error("CaptchaConfigured() = true, want false without Turnstile keys")
	}
}

func TestLoadFullEnv(t *testing.T) {
	env := validEnv()
	env["BASE_URL"] = "https://support.example.com/"
	env["LISTEN"] = "127.0.0.1:9000"
	env["DB_PATH"] = "/tmp/freesupp.db"
	env["SMTP_HOST"] = "email-smtp.eu-west-1.amazonaws.com"
	env["SMTP_PORT"] = "465"
	env["SMTP_USER"] = "AKIA"
	env["SMTP_PASSWORD"] = " pass with spaces "
	env["MAIL_FROM"] = "support@example.com"
	env["TURNSTILE_SITE_KEY"] = "site"
	env["TURNSTILE_SECRET"] = "secret"
	env["USE_PROXY"] = "true"

	cfg, err := Load(getenvFrom(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "https://support.example.com" {
		t.Errorf("BaseURL = %q, want trailing slash trimmed", cfg.BaseURL)
	}
	if cfg.Listen != "127.0.0.1:9000" || cfg.DBPath != "/tmp/freesupp.db" {
		t.Errorf("Listen/DBPath = %q/%q", cfg.Listen, cfg.DBPath)
	}
	if cfg.SMTPPort != 465 {
		t.Errorf("SMTPPort = %d, want 465", cfg.SMTPPort)
	}
	if cfg.SMTPPassword != " pass with spaces " {
		t.Errorf("SMTPPassword = %q, want untrimmed", cfg.SMTPPassword)
	}
	if !cfg.SMTPConfigured() || !cfg.CaptchaConfigured() {
		t.Error("expected SMTP and captcha to be configured")
	}
	if !cfg.UseProxy {
		t.Error("UseProxy = false, want true from USE_PROXY=true")
	}
}

func TestUseProxyDefaultsToFalse(t *testing.T) {
	cfg, err := Load(getenvFrom(validEnv()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.UseProxy {
		t.Error("UseProxy = true, want false when USE_PROXY is unset")
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{"missing base url", func(e map[string]string) { delete(e, "BASE_URL") }, "BASE_URL is required"},
		{"base url without scheme", func(e map[string]string) { e["BASE_URL"] = "support.example.com" }, "must start with http"},
		{"missing session secret", func(e map[string]string) { delete(e, "SESSION_SECRET") }, "SESSION_SECRET is required"},
		{"short session secret", func(e map[string]string) { e["SESSION_SECRET"] = "s3cret" }, "SESSION_SECRET must be at least 16 characters"},
		{
			"placeholder session secret",
			func(e map[string]string) { e["SESSION_SECRET"] = "change-me-to-a-random-32-byte-hex-string" },
			"SESSION_SECRET is still the example placeholder",
		},
		{"bad use proxy", func(e map[string]string) { e["USE_PROXY"] = "yes please" }, "USE_PROXY must be true or false"},
		{"smtp without mail from", func(e map[string]string) { e["SMTP_HOST"] = "smtp.example.com" }, "MAIL_FROM is required"},
		{"bad smtp port", func(e map[string]string) { e["SMTP_PORT"] = "abc" }, "SMTP_PORT must be a number"},
		{"out of range smtp port", func(e map[string]string) { e["SMTP_PORT"] = "70000" }, "SMTP_PORT must be a number"},
		{"half configured turnstile", func(e map[string]string) { e["TURNSTILE_SITE_KEY"] = "site" }, "must be set together"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			tt.mutate(env)
			cfg, err := Load(getenvFrom(env))
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantErr)
			}
			if cfg != nil {
				t.Error("Load() returned a config alongside an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadReportsAllErrorsAtOnce(t *testing.T) {
	_, err := Load(getenvFrom(map[string]string{}))
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	for _, want := range []string{"BASE_URL", "SESSION_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing mention of %s", err, want)
		}
	}
}
