package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestNewEvent_fillsMachineFields(t *testing.T) {
	t.Setenv(MachineIDEnvVar, "deterministic-test-machine-id")

	e := NewEvent("MAEXAMPLE123456789AB")

	if e.Event != "cli.feedback.submitted" {
		t.Errorf("Event = %q", e.Event)
	}
	if e.SessionID == "" {
		t.Error("SessionID empty")
	}
	if e.AnonMachineID != "deterministic-test-machine-id" {
		t.Errorf("AnonMachineID = %q, want override value", e.AnonMachineID)
	}
	if !strings.HasPrefix(e.AuthIDHash, "sha256:") {
		t.Errorf("AuthIDHash = %q, want sha256: prefix", e.AuthIDHash)
	}
	if len(e.AuthIDHash) != len("sha256:")+16 {
		t.Errorf("AuthIDHash length = %d, want %d", len(e.AuthIDHash), len("sha256:")+16)
	}
	if e.Context.OS != runtime.GOOS {
		t.Errorf("Context.OS = %q, want %q", e.Context.OS, runtime.GOOS)
	}
	if e.Context.Arch != runtime.GOARCH {
		t.Errorf("Context.Arch = %q, want %q", e.Context.Arch, runtime.GOARCH)
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestNewEvent_emptyAuthIDLeavesHashEmpty(t *testing.T) {
	t.Setenv(MachineIDEnvVar, "x")
	e := NewEvent("")
	if e.AuthIDHash != "" {
		t.Errorf("AuthIDHash = %q, want empty", e.AuthIDHash)
	}
}

func TestNewEvent_sameAuthIDProducesSameHash(t *testing.T) {
	t.Setenv(MachineIDEnvVar, "x")
	a := NewEvent("MASAMEID0000000000XX")
	b := NewEvent("MASAMEID0000000000XX")
	if a.AuthIDHash != b.AuthIDHash {
		t.Errorf("hashes differ: %q vs %q", a.AuthIDHash, b.AuthIDHash)
	}
	c := NewEvent("MADIFFERENT00000000XX")
	if a.AuthIDHash == c.AuthIDHash {
		t.Error("different auth_ids produced same hash")
	}
}

func TestEvent_SetComment_redactsAndCounts(t *testing.T) {
	t.Setenv(MachineIDEnvVar, "x")
	e := NewEvent("")
	e.SetComment("phone +14155551212 and email bob@example.com")

	if strings.Contains(e.Comment, "+14155551212") {
		t.Error("phone leaked into Comment")
	}
	if strings.Contains(e.Comment, "bob@example.com") {
		t.Error("email leaked into Comment")
	}
	if e.RedactionCount != 2 {
		t.Errorf("RedactionCount = %d, want 2", e.RedactionCount)
	}
	if e.CommentLength != len("phone +14155551212 and email bob@example.com") {
		t.Errorf("CommentLength = %d", e.CommentLength)
	}
}

func TestEvent_SetComment_empty(t *testing.T) {
	t.Setenv(MachineIDEnvVar, "x")
	e := NewEvent("")
	e.SetComment("   \n  ")
	if e.Comment != "" {
		t.Errorf("expected empty Comment, got %q", e.Comment)
	}
	if e.RedactionCount != 0 {
		t.Errorf("RedactionCount = %d, want 0", e.RedactionCount)
	}
}

func TestSubmit_postsExpectedShape(t *testing.T) {
	var got Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Plivo-CLI/") {
			t.Errorf("User-Agent = %q, want Plivo-CLI prefix", ua)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body not valid JSON: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	t.Setenv(EndpointEnvVar, srv.URL)
	t.Setenv(MachineIDEnvVar, "test-machine")

	e := NewEvent("MAEXAMPLE123456789AB")
	e.Rating = 4
	e.SetComment("works great mostly")
	e.Trigger = TriggerExplicit
	e.Context.CommandPath = "voice.calls.make"

	if err := e.Submit(context.Background()); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if got.Rating != 4 {
		t.Errorf("Rating = %d, want 4", got.Rating)
	}
	if got.Comment != "works great mostly" {
		t.Errorf("Comment = %q", got.Comment)
	}
	if got.Trigger != TriggerExplicit {
		t.Errorf("Trigger = %q", got.Trigger)
	}
	if got.Context.CommandPath != "voice.calls.make" {
		t.Errorf("Context.CommandPath = %q", got.Context.CommandPath)
	}
	if got.AnonMachineID != "test-machine" {
		t.Errorf("AnonMachineID = %q", got.AnonMachineID)
	}
}

func TestSubmit_returnsSentinelWhenEndpointUnset(t *testing.T) {
	t.Setenv(EndpointEnvVar, "")
	t.Setenv(MachineIDEnvVar, "x")
	e := NewEvent("")
	err := e.Submit(context.Background())
	if !errors.Is(err, ErrEndpointNotConfigured) {
		t.Errorf("err = %v, want ErrEndpointNotConfigured", err)
	}
}

func TestSubmit_propagates4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(EndpointEnvVar, srv.URL)
	t.Setenv(MachineIDEnvVar, "x")

	e := NewEvent("")
	err := e.Submit(context.Background())
	if err == nil {
		t.Fatal("expected error from 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("err should mention 429, got %v", err)
	}
}

func TestMachineID_persistsAcrossInvocations(t *testing.T) {
	// Force machineID() to use the file path (NOT the env override).
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(MachineIDEnvVar, "")

	a := machineID()
	b := machineID()
	if a == "" {
		t.Fatal("got empty machineID")
	}
	if a != b {
		t.Errorf("machineID not stable: %q vs %q", a, b)
	}
}
