package cmd

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/spf13/cobra"
)

var sipTestURI string

var sipTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Check that a SIP URI is reachable from this machine (places no call)",
	Long: `Check, from this machine, that a SIP URI can be reached before any call is
sent to it. No login is needed, and no INVITE is ever sent, so no call is placed.

TCP and TLS: resolve the host (A/AAAA), open a TCP connection and, for TLS,
complete the handshake: SNI is the host, the certificate is verified and its
expiry shown. UDP: resolve the host and send one SIP OPTIONS request.

sips: and transport=tls are tested as TLS, on port 5061 unless the URI gives
one; tcp and udp use 5060. With no transport, UDP is tested: SIP's default.
Quote a URI that has ;transport, or the shell cuts the command at the ;.

A pass means the host answers, not that the platform will accept calls. Exit 0
when reachable. Exit 3 when unreachable, and when a UDP OPTIONS gets no reply:
hosted platforms often ignore SIP from addresses they do not know, so that
result is "unknown". Each step waits up to 5 seconds, or --timeout when given.`,
	Example: `  plivo sip test --uri "sip:agent.example.com;transport=tls"
  plivo sip test --uri "sip.example.com:5060;transport=tcp" -o json
  plivo sip test --uri sips:sip.example.com --dry-run`,
	Args: cobra.NoArgs,
	RunE: runSIPTest,
}

func init() {
	sipTestCmd.Flags().StringVar(&sipTestURI, "uri", "", "SIP URI to test, e.g. \"sip.example.com;transport=tcp\" (required)")
	sipCmd.AddCommand(sipTestCmd)
}

// sipTestStep is how long each step waits when --timeout is not given. A UDP
// OPTIONS that nobody answers would otherwise hold the terminal for the 30
// seconds an API request is allowed.
const sipTestStep = 5 * time.Second

// Seams for tests: the resolver, and the roots a TLS certificate must chain to
// (nil means the system's).
var (
	sipLookup      = net.DefaultResolver.LookupIPAddr
	sipTestRootCAs *x509.CertPool
)

// sipResultOf turns the status of the check that decided a run into its result.
var sipResultOf = map[string]string{"pass": "reachable", "fail": "unreachable", "unknown": "unknown"}

// sipCheck is one step of `sip test`.
type sipCheck struct {
	Name         string   `json:"name"`   // dns, tcp, tls or udp
	Status       string   `json:"status"` // pass, fail, skip or unknown
	Detail       string   `json:"detail"`
	DurationMS   int64    `json:"duration_ms"`
	Addresses    []string `json:"addresses,omitempty"`
	CertNotAfter string   `json:"cert_not_after,omitempty"`
	SIPResponse  string   `json:"sip_response,omitempty"`
}

type sipTestResult struct {
	URI       string     `json:"uri"`
	Host      string     `json:"host"`
	Port      int        `json:"port"`
	Transport string     `json:"transport"`
	Checks    []sipCheck `json:"checks"`
	Result    string     `json:"result"` // reachable, unreachable or unknown
}

func runSIPTest(cmd *cobra.Command, _ []string) error {
	if normalizeSIPURI(sipTestURI) == "" {
		return clierr.BadFlag("uri", "required: the SIP URI to test, e.g. \"sip.example.com;transport=tcp\"")
	}
	u, err := parseURIFlag(sipTestURI)
	if err != nil {
		return err
	}
	step := sipTestStep
	if cmd.Flags().Changed("timeout") {
		if timeoutSec < 1 {
			return clierr.BadFlag("timeout", "must be at least 1 second")
		}
		step = time.Duration(timeoutSec) * time.Second
	}
	transport, defaulted := u.testTransport()
	port := u.testPort(transport)
	if dryRunFlag {
		printSIPTestPlan(u, transport, port, step)
		return nil
	}

	res := probeSIPURI(cmd.Context(), u, transport, port, step)
	res.URI = normalizeSIPURI(sipTestURI)
	if err := renderSIPTest(res, defaulted); err != nil {
		return err
	}
	return sipTestOutcome(res)
}

// testTransport is what gets tested: sips: always means TLS, then the
// transport parameter, then UDP, SIP's default when none is given.
func (u sipURI) testTransport() (transport string, defaulted bool) {
	switch {
	case u.Scheme == "sips":
		return "tls", false
	case u.Transport != "":
		return u.Transport, false
	}
	return "udp", true
}

// testPort is the URI's port, else SIP's default: 5061 for TLS, 5060 otherwise.
func (u sipURI) testPort(transport string) int {
	switch {
	case u.Port != 0:
		return u.Port
	case transport == "tls":
		return 5061
	}
	return 5060
}

func probeSIPURI(ctx context.Context, u sipURI, transport string, port int, step time.Duration) sipTestResult {
	res := sipTestResult{Host: u.Host, Port: port, Transport: transport}
	addrs, dns := sipResolve(ctx, u.Host, step)
	res.Checks = append(res.Checks, dns)
	if dns.Status == "fail" {
		res.Result = "unreachable"
		return res
	}
	if transport == "udp" {
		c := sipSendOptions(u, addrs, port, step)
		res.Checks = append(res.Checks, c)
		res.Result = sipResultOf[c.Status]
		return res
	}
	conn, c := sipDialTCP(ctx, addrs, port, step)
	res.Checks = append(res.Checks, c)
	if conn == nil {
		res.Result = "unreachable"
		return res
	}
	defer func() { _ = conn.Close() }()
	if transport == "tls" {
		c := sipHandshake(ctx, conn, u.Host, step)
		res.Checks = append(res.Checks, c)
		res.Result = sipResultOf[c.Status]
		return res
	}
	res.Result = "reachable"
	return res
}

// sipResolve returns the addresses to try. An IP host is used as is.
func sipResolve(ctx context.Context, host string, step time.Duration) ([]string, sipCheck) {
	c := sipCheck{Name: "dns"}
	if _, err := netip.ParseAddr(host); err == nil {
		c.Status, c.Detail = "skip", host+" is an IP address"
		return []string{host}, c
	}
	ctx, cancel := context.WithTimeout(ctx, step)
	defer cancel()
	start := time.Now()
	found, err := sipLookup(ctx, host)
	c.DurationMS = time.Since(start).Milliseconds()
	if err == nil && len(found) == 0 {
		err = errors.New("no A or AAAA record")
	}
	if err != nil {
		why := err.Error()
		var de *net.DNSError
		if errors.As(err, &de) {
			why = de.Err // without the "lookup <host>:" prefix
		}
		c.Status, c.Detail = "fail", fmt.Sprintf("%s does not resolve: %s", host, why)
		return nil, c
	}
	for _, a := range found {
		c.Addresses = append(c.Addresses, a.IP.String())
	}
	c.Status, c.Detail = "pass", host+" resolves to "+strings.Join(c.Addresses, ", ")
	return c.Addresses, c
}

// sipDialTCP connects to the first address that answers. What is left of the
// step is split across the addresses still to try, as Go's own dialer does, so
// one silent address cannot use up the others' time.
func sipDialTCP(ctx context.Context, addrs []string, port int, step time.Duration) (net.Conn, sipCheck) {
	c := sipCheck{Name: "tcp"}
	start := time.Now()
	var fails []string
	for i, ip := range addrs {
		target := net.JoinHostPort(ip, strconv.Itoa(port))
		budget := time.Until(start.Add(step)) / time.Duration(len(addrs)-i)
		d := net.Dialer{Timeout: budget}
		conn, err := d.DialContext(ctx, "tcp", target)
		if err == nil {
			c.Status, c.Detail = "pass", "connected to "+target
			c.DurationMS = time.Since(start).Milliseconds()
			return conn, c
		}
		fails = append(fails, sipFailureAt(addrs, target, sipNetFailure(err, budget)))
	}
	c.Status, c.Detail = "fail", strings.Join(fails, "; ")
	c.DurationMS = time.Since(start).Milliseconds()
	return nil, c
}

func sipHandshake(ctx context.Context, conn net.Conn, host string, step time.Duration) sipCheck {
	c := sipCheck{Name: "tls"}
	ctx, cancel := context.WithTimeout(ctx, step)
	defer cancel()
	tc := tls.Client(conn, &tls.Config{ServerName: host, RootCAs: sipTestRootCAs, MinVersion: tls.VersionTLS12})
	start := time.Now()
	err := tc.HandshakeContext(ctx)
	c.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		c.Status, c.Detail = "fail", "handshake failed: "+sipNetFailure(err, step)
		return c
	}
	st := tc.ConnectionState()
	leaf := st.PeerCertificates[0]
	c.CertNotAfter = leaf.NotAfter.UTC().Format(time.RFC3339)
	c.Status = "pass"
	c.Detail = fmt.Sprintf("%s; certificate for %s valid until %s (%d days)", tls.VersionName(st.Version), host,
		leaf.NotAfter.UTC().Format("2006-01-02"), int(time.Until(leaf.NotAfter).Hours()/24))
	return c
}

// sipSendOptions sends one OPTIONS and waits for any SIP reply to it: even an
// error status proves something is listening. Silence is "unknown", not
// "unreachable", because a hosted platform may drop SIP from an address it
// does not know. Only a failed send moves on to the next address, so at most
// one request ever leaves.
func sipSendOptions(u sipURI, addrs []string, port int, step time.Duration) sipCheck {
	c := sipCheck{Name: "udp"}
	start := time.Now()
	var conn net.Conn
	var callID string
	var fails []string
	for _, ip := range addrs {
		target := net.JoinHostPort(ip, strconv.Itoa(port))
		cn, err := net.Dial("udp", target)
		if err == nil {
			var req []byte
			req, callID = sipOptionsRequest(u, cn.LocalAddr())
			if _, err = cn.Write(req); err == nil {
				conn = cn
				break
			}
			_ = cn.Close()
		}
		fails = append(fails, sipFailureAt(addrs, target, sipNetFailure(err, step)))
	}
	if conn == nil {
		c.Status, c.Detail = "fail", strings.Join(fails, "; ")
		return c
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(start.Add(step))
	buf := make([]byte, 65535)
	for {
		n, err := conn.Read(buf)
		c.DurationMS = time.Since(start).Milliseconds()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				c.Status, c.Detail = "unknown", fmt.Sprintf("no reply to OPTIONS from %s within %s", conn.RemoteAddr(), step)
				return c
			}
			// An ICMP port-unreachable surfaces here on a connected socket.
			c.Status, c.Detail = "fail", sipFailureAt(addrs, conn.RemoteAddr().String(), sipNetFailure(err, step))
			return c
		}
		reply := string(buf[:n])
		if !strings.HasPrefix(reply, "SIP/2.0 ") || !strings.Contains(reply, callID) {
			continue // not an answer to this request
		}
		status, _, _ := strings.Cut(reply, "\r\n")
		c.Status, c.SIPResponse = "pass", status
		c.Detail = fmt.Sprintf("%s answered: %s", conn.RemoteAddr(), status)
		return c
	}
}

// sipOptionsRequest builds the only request `sip test` sends. OPTIONS asks what
// a server supports and never sets up a call. Call-ID, branch and tag are
// random, so no two runs look alike to the server.
func sipOptionsRequest(u sipURI, local net.Addr) (req []byte, callID string) {
	target := "sip:"
	if u.User != "" {
		target += u.User + "@"
	}
	if strings.Contains(u.Host, ":") {
		target += "[" + u.Host + "]"
	} else {
		target += u.Host
	}
	if u.Port != 0 {
		target += ":" + strconv.Itoa(u.Port)
	}
	callID = randomHex(16)
	var b strings.Builder
	fmt.Fprintf(&b, "OPTIONS %s SIP/2.0\r\n", target)
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP %s;branch=z9hG4bK%s;rport\r\n", local, randomHex(12))
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <sip:plivo-cli@anonymous.invalid>;tag=%s\r\n", randomHex(8))
	fmt.Fprintf(&b, "To: <%s>\r\n", target)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	b.WriteString("CSeq: 1 OPTIONS\r\n")
	fmt.Fprintf(&b, "Contact: <sip:plivo-cli@%s>\r\n", local)
	b.WriteString("Accept: application/sdp\r\n")
	fmt.Fprintf(&b, "User-Agent: %s\r\n", version.UserAgent())
	b.WriteString("Content-Length: 0\r\n\r\n")
	return []byte(b.String()), callID
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sipFailureAt names the address a failure came from when the host has more
// than one; with a single address the result line already says which.
func sipFailureAt(addrs []string, target, why string) string {
	if len(addrs) > 1 {
		return target + ": " + why
	}
	return why
}

// sipNetFailure says why a network step failed, without the dial/read prefix
// Go puts on the error.
func sipNetFailure(err error, waited time.Duration) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "no answer within " + waited.Round(time.Millisecond).String()
	}
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Err != nil {
		return oe.Err.Error()
	}
	return err.Error()
}

func renderSIPTest(res sipTestResult, defaulted bool) error {
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, res, nil)
	}
	rows := [][]string{{"CHECK", "STATUS", "DETAIL"}}
	for _, c := range res.Checks {
		d := c.Detail
		if c.Status != "skip" {
			d += fmt.Sprintf(" (%d ms)", c.DurationMS)
		}
		rows = append(rows, []string{c.Name, c.Status, d})
	}
	if err := output.Table(os.Stdout, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\nresult: %s (%s to %s)\n", res.Result, strings.ToUpper(res.Transport),
		net.JoinHostPort(res.Host, strconv.Itoa(res.Port)))
	if quietFlag {
		return nil
	}
	if defaulted {
		fmt.Fprintln(os.Stdout, "The URI names no transport, so UDP was tested. Add ;transport=tcp or ;transport=tls if the platform expects one.")
	}
	if res.Result == "reachable" {
		fmt.Fprintln(os.Stdout, "A pass means the host answers, not that the platform will accept calls.")
	}
	return nil
}

// sipTestOutcome turns an unreachable or silent URI into exit 3 once the result
// is printed, so a script can branch on it.
func sipTestOutcome(res sipTestResult) error {
	last := res.Checks[len(res.Checks)-1]
	target := net.JoinHostPort(res.Host, strconv.Itoa(res.Port))
	switch res.Result {
	case "reachable":
		return nil
	case "unknown":
		return &clierr.Error{
			Code:    clierr.CodeUpstreamTimeout,
			Message: fmt.Sprintf("no reply to SIP OPTIONS from %s over UDP: reachability unknown", target),
			Hint: "Hosted platforms often ignore SIP from addresses they do not know, so silence does not mean " +
				"the URI is wrong. If the platform also takes TCP or TLS, test that URI instead.",
			Context: map[string]any{"result": res.Result},
		}
	}
	hints := map[string]string{
		"dns": "Check the host in --uri: it has no address.",
		"tcp": "Check the port and transport: a platform that expects TLS usually listens on 5061, and some take only UDP.",
		"tls": "The certificate must be valid for the host and chain to a trusted authority. Check the host and the port.",
		"udp": "Nothing answered on that UDP port. Check the port and the transport.",
	}
	return &clierr.Error{
		Code:    clierr.CodeNetworkError,
		Message: fmt.Sprintf("%s is not reachable over %s: %s", target, strings.ToUpper(res.Transport), last.Detail),
		Hint:    hints[last.Name],
		Context: map[string]any{"result": res.Result, "check": last.Name},
	}
}

// printSIPTestPlan is --dry-run: the steps a real run would take, with nothing
// resolved, dialled or sent.
func printSIPTestPlan(u sipURI, transport string, port int, step time.Duration) {
	target := net.JoinHostPort(u.Host, strconv.Itoa(port))
	fmt.Fprintf(os.Stderr, "[dry-run] sip test %s over %s; nothing is sent\n", target, strings.ToUpper(transport))
	if _, err := netip.ParseAddr(u.Host); err == nil {
		fmt.Fprintf(os.Stderr, "  dns  skip: %s is an IP address\n", u.Host)
	} else {
		fmt.Fprintf(os.Stderr, "  dns  resolve %s (A/AAAA) within %s\n", u.Host, step)
	}
	if transport == "udp" {
		fmt.Fprintf(os.Stderr, "  udp  send one SIP OPTIONS to port %d and wait up to %s for a reply\n", port, step)
		return
	}
	fmt.Fprintf(os.Stderr, "  tcp  connect to port %d within %s\n", port, step)
	if transport == "tls" {
		fmt.Fprintf(os.Stderr, "  tls  handshake and verify the certificate for %s within %s\n", u.Host, step)
	}
}
