package cmd

import (
	"strings"
	"testing"
)

// A 401 whose message is a server-side resolution failure is not a credential
// problem. Reported from the field: a user hit "Region resolution failed for
// this account", was told to re-run `plivo login`, and logged out and
// reinstalled twice before trying a different account. The credential was
// fine the whole time.
func TestNonCredentialAuthHint_regionResolution(t *testing.T) {
	got := nonCredentialAuthHint("Region resolution failed for this account")
	if got == "" {
		t.Fatal("a region-resolution 401 must not fall back to the credential hint")
	}
	if !strings.Contains(got, "Not a credential problem") {
		t.Errorf("hint should say it is not a credential problem, got: %q", got)
	}
	// It must not send the user back to login, which cannot fix this.
	if strings.Contains(got, "Run `plivo login`") {
		t.Errorf("hint still tells the user to re-login, which cannot help: %q", got)
	}
	// Case-insensitive, since the server's wording may change case.
	if nonCredentialAuthHint("REGION RESOLUTION failed") == "" {
		t.Error("matching should be case-insensitive")
	}
}

// A genuine credential rejection must still get the credential hint.
func TestNonCredentialAuthHint_fallsBackForRealCredentialFailures(t *testing.T) {
	for _, msg := range []string{
		"invalid credentials",
		"Unauthorized",
		"",
		"authentication token expired",
	} {
		if h := nonCredentialAuthHint(msg); h != "" {
			t.Errorf("%q should fall back to the credential hint, got: %q", msg, h)
		}
	}
}

// credentialHint itself still reports the source, so a real rejection names
// the right thing to fix.
func TestCredentialHint_namesTheSource(t *testing.T) {
	t.Cleanup(func() { credSource = "" })

	credSource = ""
	if !strings.Contains(credentialHint(), "plivo login") {
		t.Error("no-credentials hint should point at login")
	}
	credSource = "env"
	if !strings.Contains(credentialHint(), "PLIVO_AUTH_ID") {
		t.Error("env hint should name the env vars")
	}
	credSource = "acme"
	if !strings.Contains(credentialHint(), `"acme"`) {
		t.Error("profile hint should name the profile")
	}
}
