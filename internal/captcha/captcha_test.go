package captcha

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kooler/freesupp/internal/config"
)

func TestDisabledAcceptsAnything(t *testing.T) {
	if err := (Disabled{}).Verify(context.Background(), "", ""); err != nil {
		t.Errorf("Verify() error = %v, want nil", err)
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		siteKey string
		secret  string
		want    string
	}{
		{name: "keys configured", siteKey: "site", secret: "shhh", want: "*captcha.Turnstile"},
		{name: "keys absent", want: "captcha.Disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{TurnstileSiteKey: tt.siteKey, TurnstileSecret: tt.secret}
			if got := typeName(New(cfg)); got != tt.want {
				t.Errorf("New() = %s, want %s", got, tt.want)
			}
		})
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *Turnstile:
		return "*captcha.Turnstile"
	case Disabled:
		return "captcha.Disabled"
	default:
		return "unknown"
	}
}

func TestTurnstileVerify(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		status     int
		body       string
		wantErr    bool
		wantFailed bool
	}{
		{name: "success", token: "tok", status: http.StatusOK, body: `{"success":true}`},
		{
			name: "rejected", token: "tok", status: http.StatusOK,
			body: `{"success":false,"error-codes":["invalid-input-response"]}`, wantErr: true, wantFailed: true,
		},
		{name: "empty token", token: "  ", wantErr: true, wantFailed: true},
		{name: "server error", token: "tok", status: http.StatusInternalServerError, body: "boom", wantErr: true},
		{name: "malformed json", token: "tok", status: http.StatusOK, body: "not json", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotForm struct{ secret, response, remoteip string }
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Errorf("ParseForm() error = %v", err)
				}
				gotForm.secret = r.PostFormValue("secret")
				gotForm.response = r.PostFormValue("response")
				gotForm.remoteip = r.PostFormValue("remoteip")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			v := NewTurnstile("shhh")
			v.Endpoint = srv.URL

			err := v.Verify(context.Background(), tt.token, "203.0.113.7")
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := errors.Is(err, ErrFailed); got != tt.wantFailed {
				t.Errorf("errors.Is(err, ErrFailed) = %v, want %v (err = %v)", got, tt.wantFailed, err)
			}
			if !tt.wantErr {
				if gotForm.secret != "shhh" || gotForm.response != "tok" || gotForm.remoteip != "203.0.113.7" {
					t.Errorf("posted form = %+v, want secret/response/remoteip forwarded", gotForm)
				}
			}
		})
	}
}

func TestTurnstileVerifyTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listening any more

	v := NewTurnstile("shhh")
	v.Endpoint = url

	err := v.Verify(context.Background(), "tok", "")
	if err == nil {
		t.Fatal("Verify() error = nil, want transport error")
	}
	if errors.Is(err, ErrFailed) {
		t.Errorf("transport error should not be ErrFailed, got %v", err)
	}
}

func TestTurnstileOmitsRemoteIPWhenEmpty(t *testing.T) {
	var hadRemoteIP bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, hadRemoteIP = r.PostForm["remoteip"]
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	v := NewTurnstile("shhh")
	v.Endpoint = srv.URL
	if err := v.Verify(context.Background(), "tok", ""); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if hadRemoteIP {
		t.Error("remoteip should be omitted when the client IP is unknown")
	}
}
