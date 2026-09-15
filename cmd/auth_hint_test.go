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

// TestMalformedAuthIDHint covers the shape mistakes the server cannot
// distinguish from a wrong password: it answers all of them with the same
// "invalid credentials". Every auth failure in the CLI's first fortnight in
// prod was one of these, not a wrong secret.
func TestMalformedAuthIDHint(t *testing.T) {
	cases := []struct {
		name    string
		authID  string
		wantSub string // "" means: looks real, fall through to credentialHint
	}{
		// Seen in prod logs.
		{"lowercase", "mamze3oteymtqtnzyync", "uppercase"},
		{"docs placeholder X", "MAXXXXXXXXXXXXXXXXXX", "placeholder"},
		{"docs placeholder alphabet", "MAABCDEFGHIJKLMNOPQR", "placeholder"},
		// Other shape mistakes.
		{"truncated", "MAMZE3OTEY", "10 characters"},
		{"too long", "MAMZE3OTEYMTQTNZYYNCXX", "22 characters"},
		{"mixed case", "MaMzE3OtEyMtQtNzYyNc", "uppercase"},
		{"wrong prefix", "XYMZE3OTEYMTQTNZYYNC", "not in Plivo's format"},
		{"has punctuation", "MAMZE3OTEY-MTQTNZYYN", "not in Plivo's format"},
		// Must NOT fire.
		{"real account id", "MAMZE3OTEYMTQTNZYYNC", ""},
		{"real subaccount id", "SAMZE3OTEYMTQTNZYYNC", ""},
		{"empty", "", ""},
		// Guards against over-eager placeholder matching: a real ID that
		// merely starts with a few sequential letters is not a placeholder.
		{"starts ABC but real", "MAABCZE3OTEYMTQTNZYY", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := malformedAuthIDHint(tc.authID)
			if tc.wantSub == "" {
				if got != "" {
					t.Fatalf("expected no hint for %q, got %q", tc.authID, got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("hint for %q = %q, want it to mention %q", tc.authID, got, tc.wantSub)
			}
		})
	}
}

// The lowercase hint is only useful if it shows the corrected value.
func TestMalformedAuthIDHint_lowercaseSuggestsTheFix(t *testing.T) {
	got := malformedAuthIDHint("mamze3oteymtqtnzyync")
	if !strings.Contains(got, "MAMZE3OTEYMTQTNZYYNC") {
		t.Errorf("hint should name the uppercased ID, got %q", got)
	}
}

// A region-resolution 401 must keep its own hint: it is not a credential
// problem at all, and the shape check must not shadow it.
func TestMalformedAuthIDHint_doesNotShadowRegionHint(t *testing.T) {
	if h := nonCredentialAuthHint("region resolution failed"); h == "" {
		t.Fatal("region hint regressed")
	}
}
