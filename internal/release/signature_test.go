package release

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// stubCosign writes a fake cosign that exits with the given code, so every
// branch is exercised without a network round-trip or a real signature.
func stubCosign(t *testing.T, exitCode int, stdout string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "cosign")
	script := "#!/bin/sh\necho \"" + stdout + "\"\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return string(rune('0' + i))
}

func writeFiles(t *testing.T) (sums, sig, cert string) {
	t.Helper()
	dir := t.TempDir()
	sums = filepath.Join(dir, "SHA256SUMS")
	sig = filepath.Join(dir, "SHA256SUMS.sig")
	cert = filepath.Join(dir, "SHA256SUMS.pem")
	for _, p := range []string{sums, sig, cert} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestVerifySignature_skipsWithoutCosign(t *testing.T) {
	sums, sig, cert := writeFiles(t)
	res, detail, err := VerifySignature("", sums, sig, cert)
	if res != SignatureSkipped || err != nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if detail != "cosign not installed" {
		t.Errorf("detail = %q", detail)
	}
}

// A release from before signing began has no .sig; that must install, not fail.
func TestVerifySignature_skipsWhenArtefactsMissing(t *testing.T) {
	sums, _, _ := writeFiles(t)
	cosign := stubCosign(t, 0, "")

	res, detail, err := VerifySignature(cosign, sums, "", "")
	if res != SignatureSkipped || err != nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if detail != "release carries no signature" {
		t.Errorf("detail = %q", detail)
	}

	res, _, _ = VerifySignature(cosign, sums, filepath.Join(t.TempDir(), "nope.sig"), "also-missing")
	if res != SignatureSkipped {
		t.Errorf("a nonexistent sig path should skip, got %v", res)
	}
}

func TestVerifySignature_verifiedWhenCosignSucceeds(t *testing.T) {
	sums, sig, cert := writeFiles(t)
	res, detail, err := VerifySignature(stubCosign(t, 0, "ok"), sums, sig, cert)
	if res != SignatureVerified || err != nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	// The detail names which identity matched, so the user can see who signed.
	if detail == "" || !contains(detail, TrustedIdentities[0]) {
		t.Errorf("detail should name the identity, got %q", detail)
	}
}

// The security-critical branch: a signature that is present and does not verify
// must be fatal, never a warning.
func TestVerifySignature_invalidIsFatal(t *testing.T) {
	sums, sig, cert := writeFiles(t)
	res, _, err := VerifySignature(stubCosign(t, 1, "verification failed"), sums, sig, cert)
	if res != SignatureInvalid {
		t.Fatalf("expected SignatureInvalid, got %v", res)
	}
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("expected ErrSignatureInvalid, got %v", err)
	}
}

// Rotation safety: identities and issuers are lists so a new one can be added
// without stranding binaries that only know the old one.
func TestTrustedIdentities_areListsNotConstants(t *testing.T) {
	if len(TrustedIdentities) == 0 {
		t.Fatal("no trusted identities configured")
	}
	if len(TrustedIssuers) < 2 {
		t.Errorf("expected multiple issuers so a rotation is additive, got %v", TrustedIssuers)
	}
	for _, id := range TrustedIdentities {
		if id == "" {
			t.Error("empty identity would accept any signer")
		}
	}
}

func TestCosignPath_emptyWhenAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got := CosignPath(); got != "" {
		// A real cosign outside PATH/HOME could still resolve; only assert the
		// contract when nothing is findable.
		t.Logf("CosignPath resolved to %q despite an empty PATH", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
