package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// Every shape Plivo's guides and the platforms' docs write must parse, or the
// check blocks the setups it exists to protect.
func TestParseSIPURI_acceptsEveryDocumentedShape(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want sipURI
	}{
		{"sip.example.com", sipURI{Host: "sip.example.com"}},
		{"sip.example.com:5060", sipURI{Host: "sip.example.com", Port: 5060}},
		{"sip.example.com;transport=tcp", sipURI{Host: "sip.example.com", Transport: "tcp"}},
		{"sip.example.com:5061;transport=TLS", sipURI{Host: "sip.example.com", Port: 5061, Transport: "tls"}},
		{"sip:user@sip.example.com", sipURI{Scheme: "sip", User: "user", Host: "sip.example.com"}},
		{"sip:+14155551234@sip.example.com:5060", sipURI{Scheme: "sip", User: "+14155551234", Host: "sip.example.com", Port: 5060}},
		{"sip:sip.example.com;transport=tls", sipURI{Scheme: "sip", Host: "sip.example.com", Transport: "tls"}},
		{"sips:sip.example.com", sipURI{Scheme: "sips", Host: "sip.example.com"}},
		{"SIPS:sip.example.com;transport=tcp", sipURI{Scheme: "sips", Host: "sip.example.com", Transport: "tcp"}},
		{"203.0.113.4", sipURI{Host: "203.0.113.4"}},
		{"203.0.113.4:5080;transport=udp", sipURI{Host: "203.0.113.4", Port: 5080, Transport: "udp"}},
		{"[2001:db8::1]", sipURI{Host: "2001:db8::1"}},
		{"sip:[2001:db8::1]:5061;transport=tls", sipURI{Scheme: "sip", Host: "2001:db8::1", Port: 5061, Transport: "tls"}},
		{"sip.example.com;transport=tcp;lr", sipURI{Host: "sip.example.com", Transport: "tcp"}},
		{"sip.example.com.", sipURI{Host: "sip.example.com."}},
		{"  sip.example.com  ", sipURI{Host: "sip.example.com"}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseSIPURI(tc.in)
			if err != nil {
				t.Fatalf("%q was rejected: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseSIPURI(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// Each refusal names the part that is wrong, so the user can fix it without
// guessing which rule fired.
func TestParseSIPURI_namesWhatIsWrong(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "is empty"},
		{"sip.example .com", "spaces"},
		{"sip.example.com:0", `port "0"`},
		{"sip.example.com:65536", `port "65536"`},
		{"sip.example.com:50x0", `port "50x0"`},
		{"sip.example.com:+5060", `port "+5060"`},
		{"sip.example.com:", `port ""`},
		{"sip.example.com;transport=sctp", `transport "sctp"`},
		{"sip.example.com;transport=", `transport ""`},
		{"sip.example.com;transport=tcp;transport=udp", "more than once"},
		{"sip.example.com;", "empty ;parameter"},
		{"sip:", "no host"},
		{":5060", "no host"},
		{"sip:user@", "no host"},
		{"sip:@sip.example.com", "empty user part"},
		{"user@sip.example.com", "no scheme"},
		{"sip_trunk.example.com", `contains '_'`},
		{"-sip.example.com", "not a valid hostname"},
		{"sip..example.com", "not a valid hostname"},
		{"203.0.113", "not a valid IPv4 address"},
		{"999.0.113.4", "not a valid IPv4 address"},
		{"sip.example.123", "not a valid hostname"},
		{"2001:db8::1", "brackets"},
		{"[2001:db8::1", "unclosed ["},
		{"[203.0.113.4]", "not an IPv6 address"},
		{"[2001:db8::1]x", "only :port"},
		{"sip://sip.example.com", "without //"},
		{"sips:sip.example.com;transport=udp", "sips:"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			_, err := parseSIPURI(tc.in)
			if err == nil {
				t.Fatalf("%q was accepted", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("parseSIPURI(%q) error %q should mention %q", tc.in, err, tc.want)
			}
		})
	}
}

func TestCheckACLEntry(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"203.0.113.4", true},
		{"198.51.100.0/24", true},
		{"2001:db8::1", true},
		{"2001:db8::/32", true},
		{"0.0.0.0/0", true},
		{"203.0.113.4,198.51.100.7", false},
		{"203.0.113.4, 198.51.100.7", false},
		{"999.0.113.4", false},
		{"203.0.113.4/33", false},
		{"fe80::1%eth0", false},
		{"sip.example.com", false},
		{"", false},
	} {
		if err := checkACLEntry(tc.in); (err == nil) != tc.ok {
			t.Errorf("checkACLEntry(%q) = %v, want ok=%v", tc.in, err, tc.ok)
		}
	}
}

// A bad --uri or --ip must be refused locally as BAD_FLAG, on create and on
// update, before a single request leaves.
func TestSIPWrites_refuseBadValuesWithoutARequest(t *testing.T) {
	for _, tc := range []struct {
		name, flag string
		args       []string
	}{
		{"uris create bad port", "uri", []string{"sip", "uris", "create", "--name", "n", "--uri", "sip.example.com:70000"}},
		{"uris create unknown transport", "uri", []string{"sip", "uris", "create", "--name", "n", "--uri", "sip.example.com;transport=sctp"}},
		{"uris update bad host", "uri", []string{"sip", "uris", "update", "U1", "--uri", "sip_example.com"}},
		{"uris update empty", "uri", []string{"sip", "uris", "update", "U1", "--uri", " "}},
		{"ip-acl create comma list", "ip", []string{"sip", "ip-acl", "create", "--name", "a", "--ip", "203.0.113.4,198.51.100.7"}},
		{"ip-acl create bad address", "ip", []string{"sip", "ip-acl", "create", "--name", "a", "--ip", "203.0.113.4", "--ip", "999.0.113.4"}},
		{"ip-acl update bad range", "ip", []string{"sip", "ip-acl", "update", "A1", "--ip", "198.51.100.0/33"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFakeCreds(t)
			resetWriteFlags(t)
			reqs := sipWriteServer(t, trunksUsingU1)
			err, _, _ := execCmd(t, tc.args...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag || ce.Context["flag"] != tc.flag {
				t.Fatalf("want BAD_FLAG on --%s, got %v", tc.flag, err)
			}
			if n := len(reqs()); n != 0 {
				t.Errorf("validation must not spend a request, made %d", n)
			}
		})
	}
}

// Valid IPv6 addresses and ranges reach the API exactly as typed.
func TestSIPACLCreate_sendsValidEntriesVerbatim(t *testing.T) {
	setFakeCreds(t)
	resetWriteFlags(t)
	reqs := sipWriteServer(t, trunksUsingU1)

	if err, _, _ := execCmd(t, "sip", "ip-acl", "create", "--name", "a", "--ip", "2001:db8::1", "--ip", "198.51.100.0/24"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := post(reqs(), "/Zentrunk/IPAccessControlList/")
	if p == nil {
		t.Fatal("no create request")
	}
	got, _ := p.body["ip_addresses"].([]any)
	if len(got) != 2 || got[0] != "2001:db8::1" || got[1] != "198.51.100.0/24" {
		t.Errorf("entries not sent verbatim: %v", p.body["ip_addresses"])
	}
}
