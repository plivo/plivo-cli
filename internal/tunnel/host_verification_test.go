package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSHArgsVerifyHost is SA-02. The SSH fallback used
// StrictHostKeyChecking=no with UserKnownHostsFile=/dev/null, which accepts
// any server without authenticating it and throws away the user's stored
// trust. The server's stdout then supplies the URL written into the
// application's answer_url, so an impersonator redirects live call handling.
//
// Asserts against the source, because the flags are what carry the property
// and a refactor could quietly drop them.
func TestSSHArgsVerifyHost(t *testing.T) {
	b, err := os.ReadFile("localhostrun.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)

	for _, banned := range []string{
		`"StrictHostKeyChecking=no"`,
		`"UserKnownHostsFile=/dev/null"`,
	} {
		if strings.Contains(src, banned) {
			t.Errorf("%s is back; the tunnel host is unauthenticated again", banned)
		}
	}
	if !strings.Contains(src, `"StrictHostKeyChecking=accept-new"`) {
		t.Error("expected accept-new: record an unknown host once, refuse a changed key")
	}
	if !strings.Contains(src, `"UserKnownHostsFile="+kh`) {
		t.Error("expected a persisted known-hosts file so a changed key can be detected")
	}
}

// The trust record must be the CLI's own, not the user's ~/.ssh/known_hosts.
func TestKnownHostsPathIsDedicated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p, err := knownHostsPath()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p, filepath.Join(".ssh", "known_hosts")) {
		t.Errorf("must not write into the user's SSH trust store, got %s", p)
	}
	if filepath.Dir(p) != filepath.Join(home, ".plivo") {
		t.Errorf("expected the file under ~/.plivo, got %s", p)
	}
	// The directory must exist and not be world-readable.
	st, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if mode := st.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("~/.plivo is %o; other users should not read the trust record", mode)
	}
}

// firstUse drives a one-time warning, so it must flip once a key is recorded.
func TestHostIsKnown(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "known_hosts_tunnel")

	if hostIsKnown(p) {
		t.Error("a nonexistent file cannot know any host")
	}
	if err := os.WriteFile(p, []byte("unrelated.example.com ssh-ed25519 AAAA...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if hostIsKnown(p) {
		t.Error("an unrelated entry must not count as knowing localhost.run")
	}
	if err := os.WriteFile(p, []byte("localhost.run ssh-ed25519 AAAA...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hostIsKnown(p) {
		t.Error("a recorded localhost.run key should be recognised")
	}
}

// A commented-out mention must not read as a recorded key, or the first-use
// warning silently stops appearing. Found by attacking hostIsKnown.
func TestHostIsKnownIgnoresComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "known_hosts_tunnel")
	for _, content := range []string{
		"# localhost.run was here\n",
		"\n\n# nothing\n",
		"other.example ssh-ed25519 AAA\n",
	} {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if hostIsKnown(p) {
			t.Errorf("treated as known: %q", content)
		}
	}
	for _, content := range []string{
		"localhost.run ssh-ed25519 AAA\n",
		"localhost.run,alias ssh-ed25519 AAA\n",
		"[localhost.run]:22 ssh-ed25519 AAA\n",
	} {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if !hostIsKnown(p) {
			t.Errorf("real entry not recognised: %q", content)
		}
	}
}
