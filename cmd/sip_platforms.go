package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// The --platform presets. Every value is copied from Plivo's integration guide
// for the platform (guideURL) or, where noted, from the platform's own docs;
// nothing is inferred. A preset never sets `secure`: secure trunking is billed
// per minute, so where a guide turns it on the CLI recommends it and leaves the
// choice to the user.

// sipPlatformURI is one inbound URI exactly as a guide writes it.
type sipPlatformURI struct {
	host      string // "" for the project's own host, which comes with --uri
	port      int    // 0 when the guide gives none
	transport string
}

type sipPlatform struct {
	name, label string
	// uris are the inbound URIs the guides give. The first is the default, and
	// the first for a host is that host's default.
	uris []sipPlatformURI
	// hostSuffix ends every host of a platform with no shared one:
	// <project>.sip.livekit.cloud, or <project>.<region>.sip.livekit.cloud.
	hostSuffix string
	// outbound is how Plivo's guide authenticates the platform's outbound
	// calls: "credential" or "ip-acl", the latter allowing ips.
	outbound string
	ips      []string
	ipsNote  string
	secure   bool   // the guide uses secure trunking for outbound calls
	india    string // what Plivo's guides say about Indian numbers
	note     string // printed when an inbound trunk is created
	guide    string
	verified string // YYYY-MM-DD the values were last checked
}

var sipPlatforms = []sipPlatform{
	{
		name: "livekit", label: "LiveKit",
		uris:       []sipPlatformURI{{transport: "tcp"}, {transport: "tls"}},
		hostSuffix: ".sip.livekit.cloud",
		outbound:   "credential", secure: true,
		india: "need region pinning on the LiveKit project (<project>.india.sip.livekit.cloud), " +
			"or calls fail to connect.",
		guide: "livekit", verified: "2026-10-08",
	},
	{
		name: "elevenlabs", label: "ElevenLabs",
		uris: []sipPlatformURI{
			{"sip.rtc.elevenlabs.io", 5060, "tcp"},
			{"sip.rtc.elevenlabs.io", 5061, "tls"},
			{"sip.rtc.in.residency.elevenlabs.io", 5060, "tcp"},
		},
		outbound: "credential", secure: true,
		india: "need an ElevenLabs India deployment and --uri sip.rtc.in.residency.elevenlabs.io.",
		guide: "elevenlabs", verified: "2026-10-08",
	},
	{
		name: "retell", label: "Retell",
		uris: []sipPlatformURI{
			{"sip.retellai.com", 0, "tcp"},
			{"sip.retellai.com", 0, "tls"},
			{"sip.retellai.com", 0, "udp"}, // from Retell's docs; Plivo's guide uses tcp
		},
		outbound: "credential",
		india:    "need Retell to confirm that your deployment terminates SIP in India.",
		note: "Retell will not import a number without an outbound trunk's domain and credential, " +
			"even for inbound-only use: create an outbound trunk too.",
		guide: "retell", verified: "2026-10-08",
	},
	{
		name: "vapi", label: "Vapi",
		uris: []sipPlatformURI{
			{"sip.vapi.ai", 0, "udp"},
			{"sip.eu.vapi.ai", 0, "udp"}, // EU, from Vapi's own Plivo guide
		},
		outbound: "ip-acl",
		ips:      []string{"44.229.228.186/32", "44.238.177.138/32"},
		ipsNote: "These are Vapi's US addresses. Vapi's EU region sends from 63.182.83.170/32 " +
			"(Vapi's docs): pass --ip 63.182.83.170/32 instead.",
		india: "are not supported: Vapi does not terminate SIP in India.",
		guide: "vapi", verified: "2026-10-08",
	},
}

// sipPresetNow is the clock the staleness check reads, so tests can move it.
var sipPresetNow = time.Now

const sipPresetMaxAge = 90 * 24 * time.Hour

func sipPlatformNames() []string {
	names := make([]string, len(sipPlatforms))
	for i, p := range sipPlatforms {
		names[i] = p.name
	}
	return names
}

// sipPlatformFlag resolves --platform; nil means none was given.
func sipPlatformFlag(name string) (*sipPlatform, error) {
	if name == "" {
		return nil, nil
	}
	for i := range sipPlatforms {
		if strings.EqualFold(sipPlatforms[i].name, name) {
			return &sipPlatforms[i], nil
		}
	}
	e := clierr.BadFlag("platform", fmt.Sprintf("%q is not one of %s", name, strings.Join(sipPlatformNames(), ", ")))
	e.Context["allowed"] = sipPlatformNames()
	return nil, e
}

func checkTransportFlag(t string) error {
	if t == "" {
		return nil
	}
	for _, k := range sipTransports {
		if t == k {
			return nil
		}
	}
	return clierr.BadFlag("transport", fmt.Sprintf("%q is not one of %s", t, strings.Join(sipTransports, ", ")))
}

func (p *sipPlatform) guideURL() string {
	return "https://www.plivo.com/docs/voice-agents/sip-trunking/integration-guides/" + p.guide
}

// begin prints what every use of a preset prints: a warning once its values
// are more than 90 days old, and what the guides say about Indian numbers.
func (p *sipPlatform) begin() {
	if v, err := time.Parse("2006-01-02", p.verified); err == nil && sipPresetNow().Sub(v) > sipPresetMaxAge {
		fmt.Fprintf(os.Stderr, "Warning: the %s preset was last verified on %s, more than 90 days ago. Check it against %s\n",
			p.label, p.verified, p.guideURL())
	}
	if !quietFlag {
		fmt.Fprintf(os.Stderr, "Indian numbers on %s %s\n", p.label, p.india)
	}
}

func (p *sipPlatform) hostExample() string {
	if p.hostSuffix != "" {
		return "<project>" + p.hostSuffix
	}
	return p.uris[0].host
}

// knowsHost reports whether the guides list host for this platform.
func (p *sipPlatform) knowsHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if p.hostSuffix != "" {
		project, ok := strings.CutSuffix(h, p.hostSuffix)
		return ok && project != "" && strings.Count(project, ".") <= 1
	}
	for _, u := range p.uris {
		if h == u.host {
			return true
		}
	}
	return false
}

// match finds the guide's URI for a listed host: by transport when one is
// set, else by port when one is set, else the host's first.
func (p *sipPlatform) match(host, transport string, port int) *sipPlatformURI {
	var first *sipPlatformURI
	for i := range p.uris {
		u := &p.uris[i]
		if u.host != "" && !strings.EqualFold(u.host, strings.TrimSuffix(host, ".")) {
			continue
		}
		switch {
		case transport != "":
			if u.transport == transport {
				return u
			}
		case port != 0 && u.port == port:
			return u
		case first == nil:
			first = u
		}
	}
	if transport != "" {
		return nil
	}
	return first
}

func (p *sipPlatform) transports() []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range p.uris {
		if !seen[u.transport] {
			seen[u.transport] = true
			out = append(out, u.transport)
		}
	}
	return out
}

// presetURI fills what --platform and --transport leave out of --uri: the
// host, port and transport the guide gives. What the user typed always wins
// and stays as typed; a host or transport the guide does not list is used as
// given, with a warning. Returns "" when there is nothing to send.
func presetURI(p *sipPlatform, raw, transport string) (string, error) {
	s := normalizeSIPURI(raw)
	var u sipURI
	if s != "" {
		var err error
		if u, err = parseURIFlag(s); err != nil {
			return "", err
		}
	}
	if transport != "" && u.Transport != "" && transport != u.Transport {
		return "", clierr.BadFlag("transport", fmt.Sprintf("%s conflicts with transport=%s in --uri", transport, u.Transport))
	}
	want := transport
	if want == "" {
		want = u.Transport
	}
	if p == nil {
		if s == "" || u.Transport != "" {
			return s, nil
		}
		return fillSIPURI(s, 0, want), nil
	}

	if s == "" {
		if p.hostSuffix != "" {
			e := clierr.BadFlag("uri", fmt.Sprintf("%s has no shared SIP host: pass your project's, e.g. --uri %s", p.label, p.hostExample()))
			e.Hint = "It is the SIP URI on your " + p.label + " project's settings page, without sip:. See " + p.guideURL()
			return "", e
		}
		s, u.Host = p.uris[0].host, p.uris[0].host
	}
	var m *sipPlatformURI
	if p.knowsHost(u.Host) {
		if m = p.match(u.Host, want, u.Port); m == nil {
			fmt.Fprintf(os.Stderr, "Warning: Plivo's %s guide does not list transport=%s for %s (it lists %s); using it as given.\n",
				p.label, want, u.Host, strings.Join(p.transports(), ", "))
		}
	} else {
		fmt.Fprintf(os.Stderr, "Warning: %s is not a %s SIP host in Plivo's guide (expected %s); using it as given.\n",
			u.Host, p.label, p.hostExample())
	}
	if want == "" {
		want = p.uris[0].transport
		if m != nil {
			want = m.transport
		}
	}
	addPort, addTransport := 0, ""
	if u.Port == 0 && m != nil {
		addPort = m.port
	}
	if u.Transport == "" {
		addTransport = want
	}
	out := fillSIPURI(s, addPort, addTransport)
	if !quietFlag {
		fmt.Fprintf(os.Stderr, "%s preset (verified %s): --uri %s\n", p.label, p.verified, out)
	}
	return out, nil
}

// fillSIPURI adds a port and a transport to a URI that has neither, keeping the
// rest as typed. It splits where parseSIPURI does: params start at the first
// ";" after any user part.
func fillSIPURI(s string, port int, transport string) string {
	at := strings.Index(s, "@") + 1
	head, params, hasParams := strings.Cut(s[at:], ";")
	out := s[:at] + head
	if port != 0 {
		out += ":" + strconv.Itoa(port)
	}
	if hasParams {
		out += ";" + params
	}
	if transport != "" {
		out += ";transport=" + transport
	}
	return out
}
