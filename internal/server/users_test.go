package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func withSession(req *http.Request, sess *http.Cookie) *http.Request {
	req.AddCookie(sess)
	return req
}

// passwordLogin signs in over the password endpoint and returns the cookie.
func passwordLogin(t *testing.T, env *testEnv, email, password string) *http.Cookie {
	t.Helper()
	rec := env.do(jsonRequest(http.MethodPost, "/api/auth/login", map[string]string{
		"email": email, "password": password,
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body)
	}
	sess := cookieByName(rec, sessionCookie)
	if sess == nil {
		t.Fatal("login did not set a session cookie")
	}
	return sess
}

type meBody struct {
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
}

type userBody struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
}

func TestAuthConfig(t *testing.T) {
	type authConfig struct {
		SetupRequired bool `json:"setup_required"`
	}

	env := newEmptyTestEnv(t)
	got := decodeBody[authConfig](t, env.do(httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)))
	if !got.SetupRequired {
		t.Error("setup_required = false on a fresh install, want true")
	}

	env = newTestEnv(t)
	got = decodeBody[authConfig](t, env.do(httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)))
	if got.SetupRequired {
		t.Error("setup_required = true with users seeded, want false")
	}
}

func TestSetupCreatesFirstAdmin(t *testing.T) {
	env := newEmptyTestEnv(t)

	rec := env.do(jsonRequest(http.MethodPost, "/api/auth/setup", map[string]string{
		"email": "First@Example.com", "password": "Password1",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d (body %s)", rec.Code, rec.Body)
	}
	me := decodeBody[meBody](t, rec)
	if me.Email != "first@example.com" || !me.IsAdmin {
		t.Errorf("setup response = %+v, want normalized email and admin", me)
	}
	// Setup signs the new admin in directly.
	sess := cookieByName(rec, sessionCookie)
	if sess == nil {
		t.Fatal("setup did not set a session cookie")
	}
	rec = env.do(withSession(httptest.NewRequest(http.MethodGet, "/api/me", nil), sess))
	if rec.Code != http.StatusOK {
		t.Errorf("me after setup status = %d", rec.Code)
	}

	// A second setup attempt must be rejected.
	rec = env.do(jsonRequest(http.MethodPost, "/api/auth/setup", map[string]string{
		"email": "intruder@example.com", "password": "Password1",
	}))
	if rec.Code != http.StatusForbidden {
		t.Errorf("second setup status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestSetupValidation(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
	}{
		{"bad email", "not-an-email", "Password1"},
		{"weak password", "a@example.com", "password"},
		{"short password", "a@example.com", "Pw1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newEmptyTestEnv(t)
			rec := env.do(jsonRequest(http.MethodPost, "/api/auth/setup", map[string]string{
				"email": tt.email, "password": tt.password,
			}))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, http.StatusBadRequest, rec.Body)
			}
			if n, _ := env.store.CountUsers(context.Background()); n != 0 {
				t.Errorf("users created = %d, want 0", n)
			}
		})
	}
}

func TestPasswordLogin(t *testing.T) {
	env := newTestEnv(t)

	sess := passwordLogin(t, env, "OPS@example.com", testPassword)
	me := decodeBody[meBody](t, env.do(withSession(httptest.NewRequest(http.MethodGet, "/api/me", nil), sess)))
	if me.Email != "ops@example.com" || !me.IsAdmin {
		t.Errorf("me = %+v, want the admin ops user", me)
	}

	// The same generic 401 for a wrong password and an unknown account.
	for _, creds := range []map[string]string{
		{"email": "ops@example.com", "password": "WrongPass1"},
		{"email": "nobody@example.com", "password": testPassword},
	} {
		rec := env.do(jsonRequest(http.MethodPost, "/api/auth/login", creds))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("login %v status = %d, want %d", creds, rec.Code, http.StatusUnauthorized)
		}
		if body := decodeBody[errorResponse](t, rec); body.Error != "incorrect email or password" {
			t.Errorf("login %v error = %q, want the generic message", creds, body.Error)
		}
	}
}

func TestChangeMyPassword(t *testing.T) {
	env := newTestEnv(t)
	sess := passwordLogin(t, env, "second@example.com", testPassword)

	// Wrong current password is rejected.
	rec := env.do(withSession(jsonRequest(http.MethodPut, "/api/me/password", map[string]string{
		"current_password": "WrongPass1", "new_password": "NewPassword1",
	}), sess))
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong current password status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	// A weak replacement is rejected even with the right current password.
	rec = env.do(withSession(jsonRequest(http.MethodPut, "/api/me/password", map[string]string{
		"current_password": testPassword, "new_password": "weak",
	}), sess))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("weak new password status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = env.do(withSession(jsonRequest(http.MethodPut, "/api/me/password", map[string]string{
		"current_password": testPassword, "new_password": "NewPassword1",
	}), sess))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d (body %s)", rec.Code, rec.Body)
	}
	passwordLogin(t, env, "second@example.com", "NewPassword1")
}

func TestUserManagementRequiresAdmin(t *testing.T) {
	env := newTestEnv(t)
	sess := passwordLogin(t, env, "second@example.com", testPassword) // not an admin

	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/users", nil),
		jsonRequest(http.MethodPost, "/api/users", map[string]any{"email": "x@example.com", "password": "Password1"}),
		jsonRequest(http.MethodDelete, "/api/users/1", nil),
		jsonRequest(http.MethodPut, "/api/users/1/admin", map[string]any{"is_admin": true}),
		jsonRequest(http.MethodPut, "/api/users/1/password", map[string]string{"password": "Password1"}),
	}
	for _, req := range requests {
		rec := env.do(withSession(req, sess))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want %d", req.Method, req.URL.Path, rec.Code, http.StatusForbidden)
		}
	}
}

func TestCreateListDeleteUsers(t *testing.T) {
	env := newTestEnv(t)
	sess := passwordLogin(t, env, "ops@example.com", testPassword)

	rec := env.do(withSession(jsonRequest(http.MethodPost, "/api/users", map[string]any{
		"email": "New@Example.com", "password": "Password1", "is_admin": true,
	}), sess))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d (body %s)", rec.Code, rec.Body)
	}
	created := decodeBody[userBody](t, rec)
	if created.Email != "new@example.com" || !created.IsAdmin {
		t.Errorf("created = %+v", created)
	}

	// Duplicates conflict, case-insensitively.
	rec = env.do(withSession(jsonRequest(http.MethodPost, "/api/users", map[string]any{
		"email": "NEW@example.com", "password": "Password1",
	}), sess))
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate create status = %d, want %d", rec.Code, http.StatusConflict)
	}

	list := decodeBody[struct {
		Users []userBody `json:"users"`
	}](t, env.do(withSession(httptest.NewRequest(http.MethodGet, "/api/users", nil), sess)))
	if len(list.Users) != 3 {
		t.Fatalf("listed %d users, want 3", len(list.Users))
	}

	// The new user can sign in with the password the admin set.
	passwordLogin(t, env, "new@example.com", "Password1")

	rec = env.do(withSession(jsonRequest(http.MethodDelete, "/api/users/"+strconv.FormatInt(created.ID, 10), nil), sess))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d (body %s)", rec.Code, rec.Body)
	}
	rec = env.do(jsonRequest(http.MethodPost, "/api/auth/login", map[string]string{
		"email": "new@example.com", "password": "Password1",
	}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("deleted user login status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestDeleteOwnAccountBlocked(t *testing.T) {
	env := newTestEnv(t)
	sess := passwordLogin(t, env, "ops@example.com", testPassword)
	self, err := env.store.UserByEmail(context.Background(), "ops@example.com")
	if err != nil {
		t.Fatalf("UserByEmail() error = %v", err)
	}

	rec := env.do(withSession(jsonRequest(http.MethodDelete, "/api/users/"+strconv.FormatInt(self.ID, 10), nil), sess))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("self-delete status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAdminFlagGuardsLastAdmin(t *testing.T) {
	env := newTestEnv(t)
	sess := passwordLogin(t, env, "ops@example.com", testPassword)
	ctx := context.Background()
	admin, _ := env.store.UserByEmail(ctx, "ops@example.com")
	member, _ := env.store.UserByEmail(ctx, "second@example.com")

	// Demoting the only admin (self included) is blocked.
	rec := env.do(withSession(jsonRequest(http.MethodPut, "/api/users/"+strconv.FormatInt(admin.ID, 10)+"/admin",
		map[string]any{"is_admin": false}), sess))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("demote last admin status = %d, want %d (body %s)", rec.Code, http.StatusBadRequest, rec.Body)
	}

	// Promote the second user, then demoting the first admin is fine.
	rec = env.do(withSession(jsonRequest(http.MethodPut, "/api/users/"+strconv.FormatInt(member.ID, 10)+"/admin",
		map[string]any{"is_admin": true}), sess))
	if rec.Code != http.StatusOK {
		t.Fatalf("promote status = %d (body %s)", rec.Code, rec.Body)
	}
	rec = env.do(withSession(jsonRequest(http.MethodPut, "/api/users/"+strconv.FormatInt(admin.ID, 10)+"/admin",
		map[string]any{"is_admin": false}), sess))
	if rec.Code != http.StatusOK {
		t.Fatalf("demote status = %d (body %s)", rec.Code, rec.Body)
	}

	// The demotion takes effect on the very next request.
	rec = env.do(withSession(httptest.NewRequest(http.MethodGet, "/api/users", nil), sess))
	if rec.Code != http.StatusForbidden {
		t.Errorf("demoted admin list status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestResetUserPassword(t *testing.T) {
	env := newTestEnv(t)
	sess := passwordLogin(t, env, "ops@example.com", testPassword)
	member, _ := env.store.UserByEmail(context.Background(), "second@example.com")

	rec := env.do(withSession(jsonRequest(http.MethodPut, "/api/users/"+strconv.FormatInt(member.ID, 10)+"/password",
		map[string]string{"password": "Rescued1pw"}), sess))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d (body %s)", rec.Code, rec.Body)
	}
	passwordLogin(t, env, "second@example.com", "Rescued1pw")

	rec = env.do(withSession(jsonRequest(http.MethodPut, "/api/users/99999/password",
		map[string]string{"password": "Rescued1pw"}), sess))
	if rec.Code != http.StatusNotFound {
		t.Errorf("reset unknown user status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
