//go:build internal

package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Internal build wires runLoginEmailDispatch to the real handler so
// `plivo login --email` reaches /v1/accounts/login-cli. Public build's
// dispatch stub returns BAD_INPUT — this test guards against an
// accidental init() removal that would silently break the dev flow.
func TestRunLoginEmailDispatch_internalIsWired(t *testing.T) {
	if runLoginEmailDispatch == nil {
		t.Fatal("runLoginEmailDispatch is nil — init() didn't wire the real handler")
	}
	// Calling it with empty saveEnv must hit the "needs --env" guard
	// inside runLoginEmailPassword — proves we routed to the real handler
	// and not to the public stub (which would reject with "not available").
	err := runLoginEmailDispatch("")
	if err == nil {
		t.Fatal("expected error from runLoginEmailDispatch(\"\")")
	}
	if !strings.Contains(err.Error(), "dev") {
		t.Errorf("error should mention 'dev' (env required), got: %v", err)
	}
}

// postLoginCLI marshals {email,password}, POSTs to <base>/v1/accounts/login-cli,
// and parses the wrapped {"data":{"plivo_auth_id","plivo_auth_token",...}}
// envelope hodor returns.
func TestPostLoginCLI_happyPath(t *testing.T) {
	var got struct {
		Email, Password string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/login-cli" {
			t.Errorf("path = %q, want /v1/accounts/login-cli", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"api_id":  "req-1",
			"message": "Authentication successful",
			"data": map[string]string{
				"email":            "u@example.com",
				"name":             "Test User",
				"plivo_auth_id":    "MA_TEST",
				"plivo_auth_token": "tok_xyz",
			},
		})
	}))
	defer srv.Close()

	resp, err := postLoginCLI(srv.URL, "u@example.com", "hunter2")
	if err != nil {
		t.Fatalf("postLoginCLI: %v", err)
	}
	if got.Email != "u@example.com" || got.Password != "hunter2" {
		t.Errorf("server received email=%q password=%q", got.Email, got.Password)
	}
	if resp.Data.PlivoAuthID != "MA_TEST" {
		t.Errorf("PlivoAuthID = %q, want MA_TEST", resp.Data.PlivoAuthID)
	}
	if resp.Data.PlivoAuthToken != "tok_xyz" {
		t.Errorf("PlivoAuthToken = %q, want tok_xyz", resp.Data.PlivoAuthToken)
	}
	if resp.Data.Name != "Test User" {
		t.Errorf("Name = %q, want Test User", resp.Data.Name)
	}
}

// 4xx from hodor must surface as a clean BadInput with the response body
// so the user sees "wrong password" vs "5xx" rather than a generic error.
func TestPostLoginCLI_badPasswordReturnsActionableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":{"global_error":"Invalid email or password"}}`))
	}))
	defer srv.Close()

	_, err := postLoginCLI(srv.URL, "u@example.com", "wrong")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid email or password") {
		t.Errorf("error should surface hodor message, got: %v", err)
	}
}
