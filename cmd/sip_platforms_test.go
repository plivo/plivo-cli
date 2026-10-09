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
		if r := p.region; r != nil {
			if !p.knowsHost(r.host) || checkACLEntries(r.ips) != nil {
				t.Errorf("%s: region %+v is not in the table's hosts or has a bad address", p.name, r)
			}
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
		{"inbound trunk without a URI", string(clierr.CodeBadInput), "sip uris create --name livekit --platform livekit --uri <project>.sip.livekit.cloud",
			[]string{"sip", "trunks", "create", "--name", "n", "--direction", "inbound", "--platform", "livekit"}},
		{"inbound trunk hint names the plan", string(clierr.CodeBadInput), "`plivo sip connect plan --number <number> --platform livekit` prints every step",
			[]string{"sip", "trunks", "create", "--name", "n", "--direction", "inbound", "--platform", "livekit"}},
		{"outbound vapi trunk without auth", string(clierr.CodeBadInput), "sip ip-acl create --name vapi --platform vapi",
			[]string{"sip", "trunks", "create", "--name", "n", "--direction", "outbound", "--platform", "vapi"}},
		{"outbound livekit trunk without auth", string(clierr.CodeBadInput), "sip credentials create",
			[]string{"sip", "trunks", "create", "--name", "n", "--direction", "outbound", "--platform", "livekit"}},
		{"ip-acl for a credential platform", string(clierr.CodeBadInput), "has no addresses to fill",
			[]string{"sip", "ip-acl", "create", "--name", "n", "--platform", "retell"}},
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

// Secure trunking is billed per minute: a preset only ever recommends it.
func TestSIPTrunksCreate_platformNeverSetsSecure(t *testing.T) {
	for _, p := range sipPlatforms {
		for _, dir := range []string{dirInbound, dirOutbound} {
			t.Run(p.name+"/"+dir, func(t *testing.T) {
				setFakeCreds(t)
				resetWriteFlags(t)
				reqs := sipWriteServer(t, trunksUsingU1)
				args := []string{"sip", "trunks", "create", "--name", "n", "--direction", dir, "--platform", p.name, "-o", "json"}
				if dir == dirInbound {
					args = append(args, "--uri", "U1")
				} else {
					args = append(args, "--credential", "C1", "--ip-acl", "A1")
				}
				err, _, stderr := execCmd(t, args...)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				body := post(reqs(), "/Zentrunk/Trunk/")
				if body == nil {
					t.Fatal("no create request")
				}
				if _, ok := body.body["secure"]; ok {
					t.Errorf("the preset sent secure: %v", body.body)
				}
				recommended := strings.Contains(stderr, "recommended: --secure")
				if want := p.secure && dir == dirOutbound; recommended != want {
					t.Errorf("recommended: --secure printed=%v, want %v:\n%s", recommended, want, stderr)
				}
			})
		}
	}

	t.Run("an explicit --secure is sent and not re-recommended", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, stderr := execCmd(t, "sip", "trunks", "create", "--name", "n", "--direction", "outbound",
			"--platform", "livekit", "--credential", "C1", "--secure")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p := post(reqs(), "/Zentrunk/Trunk/"); p == nil || p.body["secure"] != true {
			t.Fatalf("--secure was not sent: %v", p)
		}
		if strings.Contains(stderr, "recommended: --secure") {
			t.Errorf("recommended a flag that was passed:\n%s", stderr)
		}
	})
}

func TestSIPTrunksCreate_platformChecksTheAuthAndPrintsNotes(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"vapi with a credential", "authenticates its outbound calls with an IP access control list",
			[]string{"--direction", "outbound", "--platform", "vapi", "--credential", "C1"}},
		{"livekit with an ip acl", "authenticates its outbound calls with a credential",
			[]string{"--direction", "outbound", "--platform", "livekit", "--ip-acl", "A1"}},
		{"retell inbound", "even for inbound-only use",
			[]string{"--direction", "inbound", "--platform", "retell", "--uri", "U1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			sipWriteServer(t, trunksUsingU1)
			err, _, stderr := execCmd(t, append([]string{"sip", "trunks", "create", "--name", "n"}, tc.args...)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr should say %q:\n%s", tc.want, stderr)
			}
		})
	}
}

func TestSIPACLCreate_platformFillsTheGuidesAddresses(t *testing.T) {
	t.Run("vapi fills its US addresses and names the EU one", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, stderr := execCmd(t, "sip", "ip-acl", "create", "--name", "vapi", "--platform", "vapi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := post(reqs(), "/Zentrunk/IPAccessControlList/")
		got, _ := p.body["ip_addresses"].([]any)
		if len(got) != 2 || got[0] != "44.229.228.186/32" || got[1] != "44.238.177.138/32" {
			t.Fatalf("ip_addresses = %v", p.body["ip_addresses"])
		}
		if !strings.Contains(stderr, "63.182.83.170/32") {
			t.Errorf("the EU address should be named:\n%s", stderr)
		}
	})

	t.Run("an explicit --ip wins", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		if err, _, _ := execCmd(t, "sip", "ip-acl", "create", "--name", "vapi", "--platform", "vapi", "--ip", "63.182.83.170/32"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, _ := post(reqs(), "/Zentrunk/IPAccessControlList/").body["ip_addresses"].([]any)
		if len(got) != 1 || got[0] != "63.182.83.170/32" {
			t.Fatalf("ip_addresses = %v", got)
		}
	})

	t.Run("a credential platform with --ip warns and sends", func(t *testing.T) {
		setFakeCreds(t)
		resetWriteFlags(t)
		reqs := sipWriteServer(t, trunksUsingU1)
		err, _, stderr := execCmd(t, "sip", "ip-acl", "create", "--name", "lk", "--platform", "livekit", "--ip", "203.0.113.4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if post(reqs(), "/Zentrunk/IPAccessControlList/") == nil || !strings.Contains(stderr, "with a credential") {
			t.Errorf("want a warning and a request, stderr:\n%s", stderr)
		}
	})
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
