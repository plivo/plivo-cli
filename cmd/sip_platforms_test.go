package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// The four platforms of the plan, no more, and every value in the table must
// itself pass the checks the CLI applies to what users type.
func TestSIPPlatforms_tableIsWellFormed(t *testing.T) {
	if got := strings.Join(sipPlatformNames(), ","); got != "livekit,elevenlabs,retell,vapi" {
		t.Fatalf("platforms = %s", got)
	}
	for _, p := range sipPlatforms {
		if _, err := time.Parse("2006-01-02", p.verified); err != nil {
			t.Errorf("%s: verified %q is not a date", p.name, p.verified)
		}
		for _, u := range p.uris {
			host := u.host
			if host == "" {
				host = "project" + p.hostSuffix
			}
			if _, err := parseSIPURI(fillSIPURI(host, u.port, u.transport)); err != nil {
				t.Errorf("%s: %+v does not parse: %v", p.name, u, err)
			}
		}
		if err := checkACLEntries(p.ips); err != nil {
			t.Errorf("%s: %v", p.name, err)
		}
		if p.outbound != "credential" && p.outbound != "ip-acl" {
			t.Errorf("%s: outbound %q", p.name, p.outbound)
		}
	}
}

// A preset fills only what the guide gives; whatever the user typed wins.
func TestSIPURIsCreate_platformFillsThePublishedValues(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"elevenlabs default", "sip.rtc.elevenlabs.io:5060;transport=tcp", []string{"--platform", "elevenlabs"}},
		{"elevenlabs tls", "sip.rtc.elevenlabs.io:5061;transport=tls", []string{"--platform", "elevenlabs", "--transport", "tls"}},
		{"elevenlabs tls by port", "sip.rtc.elevenlabs.io:5061;transport=tls", []string{"--platform", "elevenlabs", "--uri", "sip.rtc.elevenlabs.io:5061"}},
		{"elevenlabs india", "sip.rtc.in.residency.elevenlabs.io:5060;transport=tcp", []string{"--platform", "elevenlabs", "--uri", "sip.rtc.in.residency.elevenlabs.io"}},
		{"livekit project", "abc123.sip.livekit.cloud;transport=tcp", []string{"--platform", "livekit", "--uri", "abc123.sip.livekit.cloud"}},
		{"livekit region tls", "abc123.india.sip.livekit.cloud;transport=tls", []string{"--platform", "livekit", "--uri", "abc123.india.sip.livekit.cloud", "--transport", "tls"}},
		{"retell", "sip.retellai.com;transport=tcp", []string{"--platform", "retell"}},
		{"vapi us", "sip.vapi.ai;transport=udp", []string{"--platform", "vapi"}},
		{"vapi eu", "sip.eu.vapi.ai;transport=udp", []string{"--platform", "vapi", "--uri", "sip.eu.vapi.ai"}},
		{"typed values win", "sip:+14155551234@sip.rtc.elevenlabs.io:5080;transport=tcp", []string{"--platform", "elevenlabs", "--uri", "sip:+14155551234@sip.rtc.elevenlabs.io:5080;transport=tcp"}},
		{"transport without a platform", "sip.example.com;transport=tls", []string{"--uri", "sip.example.com", "--transport", "tls"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, stderr := execCmd(t, append([]string{"sip", "uris", "create", "--name", "n"}, tc.args...)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			p := post(reqs(), "/Zentrunk/URI/")
			if p == nil || p.body["uri"] != tc.want {
				t.Fatalf("uri sent = %v, want %q", p, tc.want)
			}
			if strings.Contains(stderr, "Warning") {
				t.Errorf("a listed value must not warn:\n%s", stderr)
			}
		})
	}
}

// Off-table values are explicit, so they are used, but never silently.
func TestSIPURIsCreate_platformWarnsOnValuesTheGuideDoesNotList(t *testing.T) {
	for _, tc := range []struct {
		name, want, warn string
		args             []string
	}{
		{"foreign host", "sip.example.com;transport=udp", "not a Vapi SIP host", []string{"--platform", "vapi", "--uri", "sip.example.com"}},
		{"unlisted transport", "sip.rtc.elevenlabs.io;transport=udp", "does not list transport=udp", []string{"--platform", "elevenlabs", "--transport", "udp"}},
		{"bare livekit domain", "sip.livekit.cloud;transport=tcp", "not a LiveKit SIP host", []string{"--platform", "livekit", "--uri", "sip.livekit.cloud"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, stderr := execCmd(t, append([]string{"sip", "uris", "create", "--name", "n"}, tc.args...)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p := post(reqs(), "/Zentrunk/URI/"); p == nil || p.body["uri"] != tc.want {
				t.Fatalf("uri sent = %v, want %q", p, tc.want)
			}
			if !strings.Contains(stderr, tc.warn) {
				t.Errorf("stderr should warn %q:\n%s", tc.warn, stderr)
			}
		})
	}
}

func TestSIPCreates_platformRefusalsSpendNoRequest(t *testing.T) {
	for _, tc := range []struct {
		name, code, want string
		args             []string
	}{
		{"unknown platform", string(clierr.CodeBadFlag), "not one of livekit, elevenlabs, retell, vapi",
			[]string{"sip", "uris", "create", "--name", "n", "--platform", "xai"}},
		{"livekit needs the project host", string(clierr.CodeBadFlag), "no shared SIP host",
			[]string{"sip", "uris", "create", "--name", "n", "--platform", "livekit"}},
		{"unknown transport", string(clierr.CodeBadFlag), `"sctp" is not one of udp, tcp, tls`,
			[]string{"sip", "uris", "create", "--name", "n", "--uri", "sip.example.com", "--transport", "sctp"}},
		{"conflicting transports", string(clierr.CodeBadFlag), "conflicts with transport=tcp",
			[]string{"sip", "uris", "create", "--name", "n", "--uri", "sip.example.com;transport=tcp", "--transport", "tls"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, _ := execCmd(t, tc.args...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || string(ce.Code) != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			if !strings.Contains(ce.Message+" "+ce.Hint, tc.want) {
				t.Errorf("message/hint should say %q, got %q / %q", tc.want, ce.Message, ce.Hint)
			}
			if n := len(reqs()); n != 0 {
				t.Errorf("a refusal must not spend a request, made %d", n)
			}
		})
	}
}

// The table goes stale silently, so a preset older than 90 days says so.
func TestSIPPlatforms_warnWhenTheValuesAreOld(t *testing.T) {
	verified, _ := time.Parse("2006-01-02", sipPlatforms[0].verified)
	t.Cleanup(func() { sipPresetNow = time.Now })
	for _, tc := range []struct {
		age  time.Duration
		warn bool
	}{
		{89 * 24 * time.Hour, false},
		{91 * 24 * time.Hour, true},
	} {
		sipPresetNow = func() time.Time { return verified.Add(tc.age) }
		setFakeCreds(t)
		resetWriteFlags(t)
		sipWriteServer(t, trunksUsingU1)
		err, _, stderr := execCmd(t, "sip", "uris", "create", "--name", "n", "--platform", "livekit", "--uri", "abc123.sip.livekit.cloud")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := strings.Contains(stderr, "more than 90 days ago"); got != tc.warn {
			t.Errorf("age %s: warned=%v, want %v:\n%s", tc.age, got, tc.warn, stderr)
		}
	}
}
