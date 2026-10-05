package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSaveTightensPreExistingPermissions is SA-09. MkdirAll and OpenFile only
// apply their mode when they CREATE. A ~/.plivo left at 0755 and a config.toml
// left at 0644 — by an older version, a permissive umask or a restored backup
// — kept those modes, and the auth token was written into a file other local
// users could read.
func TestSaveTightensPreExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".plivo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("# stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Confirm the permissive starting state, or the test proves nothing.
	if st, _ := os.Stat(cfgPath); st.Mode().Perm() != 0o644 {
		t.Fatalf("fixture did not start at 0644, got %o", st.Mode().Perm())
	}

	cfg := &Config{}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("config is %o after save; other local users can read the token", mode)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := di.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("~/.plivo is %o after save; it should be private", mode)
	}
}

// Fresh creation must still be private; this is the path that already worked
// and must not regress.
func TestSaveCreatesPrivateConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Save(&Config{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("freshly created config is %o, want private", mode)
	}
}

// The atomic replace must not litter the directory with temp files, and must
// leave exactly one config behind.
func TestSaveLeavesNoTempFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for range 3 {
		if err := Save(&Config{}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(home, ".plivo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("stray file left behind: %s", e.Name())
		}
	}
}
