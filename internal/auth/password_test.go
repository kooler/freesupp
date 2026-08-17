package auth

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"valid", "Password1", false},
		{"valid with symbols", "Sup3r-Secret!", false},
		{"too short", "Pass1xy", true},
		{"no uppercase", "password1", true},
		{"no lowercase", "PASSWORD1", true},
		{"no digit", "Passwordx", true},
		{"empty", "", true},
		{"over bcrypt limit", strings.Repeat("Aa1", 25), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.pw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassword(%q) error = %v, wantErr %v", tt.pw, err, tt.wantErr)
			}
		})
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("Password1")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "Password1" || hash == "" {
		t.Fatalf("hash = %q, want a bcrypt digest", hash)
	}

	if err := CheckPassword(hash, "Password1"); err != nil {
		t.Errorf("CheckPassword(correct) error = %v", err)
	}
	if err := CheckPassword(hash, "wrong"); err == nil {
		t.Error("CheckPassword(wrong) error = nil, want ErrWrongPassword")
	}
	// The empty-hash path stands in for unknown accounts and must never match.
	if err := CheckPassword("", "Password1"); err == nil {
		t.Error("CheckPassword with empty hash error = nil, want ErrWrongPassword")
	}
}
