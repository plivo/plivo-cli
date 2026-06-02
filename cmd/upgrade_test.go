package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHomebrewInstallHint(t *testing.T) {
	cases := []struct {
		name, path string
		wantHint   bool
	}{
		{"apple-silicon-brew", "/opt/homebrew/bin/plivo", true},
		{"apple-silicon-cellar", "/opt/homebrew/Cellar/plivo/0.1.0/bin/plivo", true},
		{"intel-brew-cellar", "/usr/local/Cellar/plivo/0.1.0/bin/plivo", true},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/plivo", true},
		{"manual-install", "/usr/local/bin/plivo", false},
		{"user-home", "/Users/alice/go/bin/plivo", false},
		{"system-path", "/usr/bin/plivo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := homebrewInstallHint(tc.path)
			if (got != "") != tc.wantHint {
				t.Errorf("hint = %q for path %q; wantHint=%v", got, tc.path, tc.wantHint)
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1500, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{7759462, "7.4 MB"},
	}
	for _, tc := range cases {
		got := humanSize(tc.bytes)
		if got != tc.want {
			t.Errorf("humanSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestAtomicReplace(t *testing.T) {
	// We can't replace the test process binary, so simulate with two
	// non-executable temp files. atomicReplace just renames — same code
	// path as the real upgrade minus the OS executable bit.
	dir := t.TempDir()
	exePath := filepath.Join(dir, "plivo")
	newPath := filepath.Join(dir, "plivo.upgrade.tmp")

	if err := os.WriteFile(exePath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := atomicReplace(exePath, newPath); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Errorf("exePath contents = %q, want %q", got, "NEW")
	}
	// On Unix the new file replaced; on Windows the old is moved to .old.
	if runtime.GOOS == "windows" {
		oldPath := exePath + ".old"
		if _, err := os.Stat(oldPath); err != nil {
			t.Errorf("expected %s to exist on windows", oldPath)
		}
	} else {
		// The .upgrade.tmp source should be gone (renamed away).
		if _, err := os.Stat(newPath); !os.IsNotExist(err) {
			t.Errorf("expected %s to be gone, stat=%v", newPath, err)
		}
	}
}

func TestPermissionHint(t *testing.T) {
	// Non-permission errors pass through unchanged.
	other := os.ErrNotExist
	if got := permissionHint(other, "/usr/local/bin/plivo"); got != other {
		t.Errorf("non-permission err mutated: got %v, want %v", got, other)
	}

	// nil passes through.
	if got := permissionHint(nil, "/usr/local/bin/plivo"); got != nil {
		t.Errorf("nil err mutated: got %v", got)
	}

	// EACCES gets wrapped with a hint mentioning the install dir.
	wrapped := permissionHint(os.ErrPermission, "/usr/local/bin/plivo")
	if wrapped == nil {
		t.Fatal("permission err = nil, want wrapped")
	}
	if !contains(wrapped.Error(), "/usr/local/bin") {
		t.Errorf("wrapped err = %q, want it to mention /usr/local/bin", wrapped)
	}
	if !contains(wrapped.Error(), "sudo") {
		t.Errorf("wrapped err = %q, want it to suggest sudo", wrapped)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestFirstCmdWord(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{}, ""},
		{[]string{"upgrade"}, "upgrade"},
		{[]string{"voice", "calls", "list"}, "voice"},
		{[]string{"--profile", "staging", "upgrade"}, "upgrade"},
		{[]string{"-o", "json", "voice", "calls"}, "voice"},
		{[]string{"--check"}, ""},
		{[]string{"--profile=staging", "voice"}, "voice"},
	}
	for _, tc := range cases {
		got := firstCmdWord(tc.args)
		if got != tc.want {
			t.Errorf("firstCmdWord(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
