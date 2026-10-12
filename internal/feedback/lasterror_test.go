package feedback

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveLastError_roundTripsPrivatelyAndAtomically(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("USERPROFILE", home)

	first := LastError{Command: "voice.calls.get", ExitCode: 1, ErrorCode: "RESOURCE_NOT_FOUND",
		CLIVersion: "1.2.0", OS: "linux", Arch: "amd64", Timestamp: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)}
	second := first
	second.Command, second.ExitCode, second.RequestID = "sip.trunks.list", 3, "00000000-0000-0000-0000-000000000000"
	for _, want := range []LastError{first, second} {
		if err := SaveLastError(want); err != nil {
			t.Fatalf("SaveLastError: %v", err)
		}
		got, err := LoadLastError()
		if err != nil || got == nil || *got != want {
			t.Fatalf("LoadLastError = %+v, %v; want %+v", got, err, want)
		}
	}

	p, _ := LastErrorFile()
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v (err %v), want 0600", fi.Mode().Perm(), err)
		}
	}
	// The temp file is renamed into place, never left behind.
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("~/.plivo holds %d entries, want only last-error.json: %v", len(entries), entries)
	}
}

func TestLoadLastError_noneWhenMissingOrCorrupt(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("USERPROFILE", home)
	if got, err := LoadLastError(); got != nil || err != nil {
		t.Errorf("missing file: %+v, %v; want nil, nil", got, err)
	}
	p, _ := LastErrorFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadLastError(); got != nil || err != nil {
		t.Errorf("corrupt file: %+v, %v; want nil, nil", got, err)
	}
}

func TestEndpoint(t *testing.T) {
	t.Setenv(EndpointEnvVar, "")
	if got, err := Endpoint("https://example.test/"); err != nil || got != "https://example.test/v1/accounts/cli/feedback" {
		t.Errorf("Endpoint(base) = %q, %v", got, err)
	}
	if _, err := Endpoint(""); err != ErrEndpointNotConfigured {
		t.Errorf("Endpoint(\"\") err = %v, want ErrEndpointNotConfigured", err)
	}
	t.Setenv(EndpointEnvVar, "http://127.0.0.1:1/collect")
	if got, _ := Endpoint("https://example.test"); got != "http://127.0.0.1:1/collect" {
		t.Errorf("the env override lost to the base: %q", got)
	}
}
