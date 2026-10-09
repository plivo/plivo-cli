package cmd

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// sipURI is a --uri value taken apart. The API always receives the user's own
// string, trimmed: nothing is ever rebuilt from these parts.
type sipURI struct {
	Scheme    string // "sip", "sips", or "" when none was given
	User      string // the part before @, "" when there is none
	Host      string // hostname, IPv4 address, or IPv6 address without brackets
	Port      int    // 0 when none was given: the platform picks the default
	Transport string // udp, tcp or tls, lower-cased; "" when none was given
}

// sipTransports are the transports Plivo's integration guides offer an
// origination URI: UDP, TCP or TLS.
var sipTransports = []string{"udp", "tcp", "tls"}

const sipURIHint = "Accepted: host, host:port, host;transport=udp|tcp|tls, sip:user@host or sips:host. " +
	"The host is a name, an IPv4 address or an IPv6 address in brackets: [2001:db8::1]:5060."

// parseURIFlag parses a --uri value and reports any problem as BAD_FLAG, before
// a request is spent on a URI that would never answer a call.
func parseURIFlag(v string) (sipURI, error) {
	u, err := parseSIPURI(v)
	if err != nil {
		e := clierr.BadFlag("uri", err.Error())
		e.Hint = sipURIHint
		return sipURI{}, e
	}
	return u, nil
}

// parseSIPURI accepts the shapes Plivo's docs and the platforms' guides use:
// host, host:port, host;transport=tcp, sip:user@host, sip:host;transport=tls,
// sips:host, and IPv4 or [IPv6] hosts. It names the part that is wrong rather
// than refusing the whole value.
func parseSIPURI(raw string) (sipURI, error) {
	s := normalizeSIPURI(raw)
	if s == "" {
		return sipURI{}, errors.New("is empty")
	}
	if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return sipURI{}, errors.New("must not contain spaces")
	}
	if strings.Contains(s, "://") {
		return sipURI{}, errors.New("is not a SIP URI: write sip:host or sips:host, without //")
	}

	var u sipURI
	rest := s
	switch lower := strings.ToLower(s); {
	case strings.HasPrefix(lower, "sips:"):
		u.Scheme, rest = "sips", s[len("sips:"):]
	case strings.HasPrefix(lower, "sip:"):
		u.Scheme, rest = "sip", s[len("sip:"):]
	}
	if user, hostPart, ok := strings.Cut(rest, "@"); ok {
		if u.Scheme == "" {
			return sipURI{}, errors.New("has a user part but no scheme: write sip:user@host")
		}
		if user == "" {
			return sipURI{}, errors.New("has an empty user part before @")
		}
		u.User, rest = user, hostPart
	}

	hostPort, params, hasParams := strings.Cut(rest, ";")
	if err := u.setHostPort(hostPort); err != nil {
		return sipURI{}, err
	}
	if hasParams {
		if err := u.setParams(params); err != nil {
			return sipURI{}, err
		}
	}
	if u.Scheme == "sips" && u.Transport == "udp" {
		return sipURI{}, errors.New("uses sips:, which means TLS, with transport=udp: drop one of them")
	}
	return u, nil
}

func (u *sipURI) setHostPort(hp string) error {
	var host, port string
	var hasPort bool
	switch {
	case strings.HasPrefix(hp, "["):
		inner, after, closed := strings.Cut(hp[1:], "]")
		if !closed {
			return errors.New("has an unclosed [ around the host")
		}
		if a, err := netip.ParseAddr(inner); err != nil || !a.Is6() || a.Zone() != "" {
			return fmt.Errorf("[%s] is not an IPv6 address", inner)
		}
		if after != "" && !strings.HasPrefix(after, ":") {
			return fmt.Errorf("has %q after the IPv6 address; only :port can follow it", after)
		}
		host, port, hasPort = inner, strings.TrimPrefix(after, ":"), after != ""
	case strings.Count(hp, ":") > 1:
		return errors.New("looks like an IPv6 address: put it in brackets, e.g. [2001:db8::1]:5060")
	default:
		host, port, hasPort = strings.Cut(hp, ":")
		if host == "" {
			return errors.New("has no host")
		}
		if err := checkSIPHost(host); err != nil {
			return err
		}
	}
	if hasPort {
		n, err := strconv.Atoi(port)
		if err != nil || strings.Trim(port, "0123456789") != "" || n < 1 || n > 65535 {
			return fmt.Errorf("port %q is not a number from 1 to 65535", port)
		}
		u.Port = n
	}
	u.Host = host
	return nil
}

// checkSIPHost accepts an IPv4 address or a hostname as RFC 3261 defines one:
// dot-separated labels of letters, digits and hyphens, the last one starting
// with a letter. That last rule is what tells 203.0.113 (a broken address)
// apart from a name.
func checkSIPHost(host string) error {
	if a, err := netip.ParseAddr(host); err == nil && a.Is4() {
		return nil
	}
	if strings.Trim(host, "0123456789.") == "" {
		return fmt.Errorf("host %q is not a valid IPv4 address", host)
	}
	for _, r := range host {
		if !isASCIILetter(r) && !(r >= '0' && r <= '9') && r != '-' && r != '.' {
			return fmt.Errorf("host %q contains %q, which a hostname cannot", host, r)
		}
	}
	name := strings.TrimSuffix(host, ".")
	labels := strings.Split(name, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return fmt.Errorf("host %q is not a valid hostname", host)
		}
	}
	if len(name) > 253 || !isASCIILetter(rune(labels[len(labels)-1][0])) {
		return fmt.Errorf("host %q is not a valid hostname", host)
	}
	return nil
}

func isASCIILetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

func (u *sipURI) setParams(params string) error {
	for _, p := range strings.Split(params, ";") {
		name, value, _ := strings.Cut(p, "=")
		if name == "" {
			return errors.New("has an empty ;parameter")
		}
		if !strings.EqualFold(name, "transport") {
			continue
		}
		if u.Transport != "" {
			return errors.New("gives transport more than once")
		}
		t := strings.ToLower(value)
		known := false
		for _, k := range sipTransports {
			known = known || t == k
		}
		if !known {
			return fmt.Errorf("transport %q is not one of %s", value, strings.Join(sipTransports, ", "))
		}
		u.Transport = t
	}
	return nil
}

// checkACLEntries checks every --ip value before a request is spent. Each is
// one IPv4 or IPv6 address or CIDR range. A comma-separated list is refused
// rather than split, so what is sent is exactly what was typed.
func checkACLEntries(entries []string) error {
	for _, e := range entries {
		if err := checkACLEntry(e); err != nil {
			bad := clierr.BadFlag("ip", err.Error())
			bad.Hint = "Pass one address or CIDR range per --ip, e.g. --ip 203.0.113.4 --ip 198.51.100.0/24."
			return bad
		}
	}
	return nil
}

func checkACLEntry(e string) error {
	if strings.Contains(e, ",") {
		return fmt.Errorf("%q holds more than one value: repeat --ip for each address", e)
	}
	if strings.Contains(e, "/") {
		if _, err := netip.ParsePrefix(e); err != nil {
			return fmt.Errorf("%q is not a CIDR range such as 198.51.100.0/24", e)
		}
		return nil
	}
	if a, err := netip.ParseAddr(e); err != nil || a.Zone() != "" {
		return fmt.Errorf("%q is not an IPv4 or IPv6 address", e)
	}
	return nil
}
