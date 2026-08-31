package release

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Signature verification is best-effort and shells out to cosign rather than
// embedding a Sigstore client. A hello-world linking sigstore-go is 17MB
// against an 8MB CLI, so embedding would roughly triple the binary to add a
// check most users cannot run anyway. Anyone who cares about provenance can
// install cosign; everyone still gets the SHA256 check, which is already more
// than most comparable CLIs enforce.

// TrustedIdentities are the signer identities a release may carry, as a LIST so
// a future rotation is additive. A single hardcoded value would strand every
// already-installed binary the moment the identity changed — and the mechanism
// that would fix it is `plivo upgrade`, the very thing that would break.
// Both GitHub CLI and HashiCorp had to retrofit this under pressure.
var TrustedIdentities = []string{
	"cx-tech@plivo.com",
}

// TrustedIssuers are the OIDC issuers those identities may come from.
var TrustedIssuers = []string{
	"https://accounts.google.com",
	"https://github.com/login/oauth",
}

// SignatureResult reports what verification concluded.
type SignatureResult int

const (
	// SignatureVerified means cosign accepted the signature.
	SignatureVerified SignatureResult = iota
	// SignatureSkipped means we could not check: no cosign, or the release
	// carries no signature. Not a failure — releases before signing began
	// legitimately have none.
	SignatureSkipped
	// SignatureInvalid means a signature was present and did NOT verify. This
	// is always fatal; it is the case signing exists to catch.
	SignatureInvalid
)

// ErrSignatureInvalid is returned when a present signature fails to verify.
var ErrSignatureInvalid = errors.New("release signature did not verify")

// CosignPath returns the cosign binary, or "" when it is not installed.
func CosignPath() string {
	if p, err := exec.LookPath("cosign"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".plivo", "bin", "cosign")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// VerifySignature checks sumsPath against a detached signature and certificate.
//
// Returns SignatureSkipped when cosign is absent or either artefact is missing,
// so an older unsigned release still installs. Returns SignatureInvalid only
// when a signature exists and fails, which callers must treat as fatal.
func VerifySignature(cosign, sumsPath, sigPath, certPath string) (SignatureResult, string, error) {
	if cosign == "" {
		return SignatureSkipped, "cosign not installed", nil
	}
	for _, p := range []string{sumsPath, sigPath, certPath} {
		if p == "" {
			return SignatureSkipped, "release carries no signature", nil
		}
		if _, err := os.Stat(p); err != nil {
			return SignatureSkipped, "release carries no signature", nil
		}
	}

	// Try each trusted identity/issuer pair. Pinning both is the whole security
	// property: without them any Sigstore-recognised identity on earth would
	// produce a passing signature.
	var last string
	for _, id := range TrustedIdentities {
		for _, iss := range TrustedIssuers {
			out, err := runCosign(cosign, sumsPath, sigPath, certPath, id, iss)
			if err == nil {
				return SignatureVerified, fmt.Sprintf("%s via %s", id, iss), nil
			}
			last = out
		}
	}
	return SignatureInvalid, strings.TrimSpace(last), ErrSignatureInvalid
}

func runCosign(cosign, sumsPath, sigPath, certPath, identity, issuer string) (string, error) {
	cmd := exec.Command(cosign, "verify-blob", sumsPath,
		"--signature", sigPath,
		"--certificate", certPath,
		"--certificate-identity", identity,
		"--certificate-oidc-issuer", issuer,
	)
	// cosign reaches Rekor; bound it so a hung network cannot stall an upgrade.
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return "cosign timed out", fmt.Errorf("cosign timed out")
	}
	return string(out), err
}
