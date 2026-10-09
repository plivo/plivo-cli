package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
)

func decodeSIPTest(t *testing.T, stdout string) sipTestResult {
	t.Helper()
	var env struct {
		Data sipTestResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not the JSON result: %v\n%s", err, stdout)
	}
	return env.Data
}

func sipTestExit(t *testing.T, err error) (int, *clierr.Error) {
	t.Helper()
	if err == nil {
		return 0, nil
	}
	var ce *clierr.Error
	if !errors.As(err, &ce) {
		t.Fatalf("not a CLI error: %v", err)
	}
	return ce.ExitCode(), ce
}

// stubLookup answers every name with 127.0.0.1 and counts the calls, so a
// test can use a hostname without the network's resolver.
func stubLookup(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	sipLookup = func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		if err != nil {
			return nil, err
		}
		return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, nil
	}
	t.Cleanup(func() { sipLookup = net.DefaultResolver.LookupIPAddr })
	return &calls
}

// The TCP check only connects: it writes nothing, so it cannot start a call.
// It also needs no login, so it runs with no profile at all.
func TestSIPTest_tcpConnectsAndWritesNothing(t *testing.T) {
	setEmptyHome(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			got <- nil
			return
		}
		b, _ := io.ReadAll(conn)
		got <- b
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", fmt.Sprintf("127.0.0.1:%d;transport=tcp", port), "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeSIPTest(t, stdout)
	if res.Result != "reachable" || res.Transport != "tcp" || len(res.Checks) != 2 ||
		res.Checks[0].Status != "skip" || res.Checks[1].Name != "tcp" || res.Checks[1].Status != "pass" {
		t.Fatalf("unexpected result: %+v", res)
	}
	select {
	case b := <-got:
		if len(b) != 0 {
			t.Errorf("the TCP check wrote %q; it must only connect", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never closed")
	}
}

func TestSIPTest_tableSaysWhatAPassMeans(t *testing.T) {
	setEmptyHome(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", fmt.Sprintf("127.0.0.1:%d;transport=tcp", port), "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"CHECK", "tcp", fmt.Sprintf("result: reachable (TCP to 127.0.0.1:%d)", port), "not that the platform will accept calls"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q:\n%s", want, stdout)
		}
	}
}

func TestSIPTest_refusedTCPIsUnreachable(t *testing.T) {
	setEmptyHome(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", fmt.Sprintf("127.0.0.1:%d;transport=tcp", port), "-o", "json")
	if code, ce := sipTestExit(t, err); code != 3 || ce.Context["check"] != "tcp" {
		t.Fatalf("want exit 3 on the tcp check, got %d: %v", code, err)
	}
	if res := decodeSIPTest(t, stdout); res.Result != "unreachable" || res.Checks[1].Status != "fail" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// tlsServer records the SNI each client sends.
func tlsServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var sni string
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		sni = hello.ServerName
		mu.Unlock()
		return nil, nil
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return sni
	}
}

// sips: means TLS: the handshake sends the host as SNI, verifies the
// certificate, and reports when it expires.
func TestSIPTest_tlsVerifiesTheCertificateForTheHost(t *testing.T) {
	setEmptyHome(t)
	srv, sni := tlsServer(t)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	sipTestRootCAs = pool
	t.Cleanup(func() { sipTestRootCAs = nil })
	stubLookup(t, nil)
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", fmt.Sprintf("sips:example.com:%d", port), "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeSIPTest(t, stdout)
	if res.Result != "reachable" || res.Transport != "tls" || len(res.Checks) != 3 || res.Checks[2].Status != "pass" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if want := srv.Certificate().NotAfter.UTC().Format(time.RFC3339); res.Checks[2].CertNotAfter != want {
		t.Errorf("cert_not_after = %q, want %q", res.Checks[2].CertNotAfter, want)
	}
	if got := sni(); got != "example.com" {
		t.Errorf("SNI = %q, want the URI's host", got)
	}
}

func TestSIPTest_untrustedCertificateIsUnreachable(t *testing.T) {
	setEmptyHome(t)
	srv, _ := tlsServer(t)
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", fmt.Sprintf("127.0.0.1:%d;transport=tls", port), "-o", "json")
	if code, ce := sipTestExit(t, err); code != 3 || ce.Context["check"] != "tls" {
		t.Fatalf("want exit 3 on the tls check, got %d: %v", code, err)
	}
	if res := decodeSIPTest(t, stdout); res.Result != "unreachable" || res.Checks[2].Status != "fail" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func sipHeader(msg, name string) string {
	for _, line := range strings.Split(msg, "\r\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(k, name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// UDP sends one OPTIONS (never an INVITE) and any SIP reply proves the host is
// there. Call-ID and branch are new on every run.
func TestSIPTest_udpSendsOneOptionsAndReadsTheReply(t *testing.T) {
	setEmptyHome(t)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	reqs := make(chan string, 8)
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			req := string(buf[:n])
			reqs <- req
			reply := "SIP/2.0 200 OK\r\nCall-ID: " + sipHeader(req, "Call-ID") + "\r\nContent-Length: 0\r\n\r\n"
			_, _ = pc.WriteTo([]byte(reply), from)
		}
	}()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	uri := fmt.Sprintf("127.0.0.1:%d", port)

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", uri, "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := decodeSIPTest(t, stdout)
	if res.Result != "reachable" || res.Transport != "udp" || res.Checks[1].SIPResponse != "SIP/2.0 200 OK" {
		t.Fatalf("unexpected result: %+v", res)
	}
	err, stdout, _ = execCmd(t, "sip", "test", "--uri", uri, "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "names no transport, so UDP was tested") {
		t.Errorf("a defaulted transport should be called out:\n%s", stdout)
	}

	first, second := <-reqs, <-reqs
	if len(reqs) != 0 {
		t.Errorf("each run must send exactly one request, got %d extra", len(reqs))
	}
	for _, req := range []string{first, second} {
		if !strings.HasPrefix(req, fmt.Sprintf("OPTIONS sip:127.0.0.1:%d SIP/2.0\r\n", port)) || strings.Contains(req, "INVITE") {
			t.Fatalf("not a lone OPTIONS:\n%s", req)
		}
		for _, h := range []string{"Max-Forwards: 70", "CSeq: 1 OPTIONS", ";branch=z9hG4bK", "Content-Length: 0"} {
			if !strings.Contains(req, h) {
				t.Errorf("request missing %q:\n%s", h, req)
			}
		}
	}
	if sipHeader(first, "Call-ID") == sipHeader(second, "Call-ID") || sipHeader(first, "Via") == sipHeader(second, "Via") {
		t.Error("Call-ID and branch must be unique per run")
	}
}

// A UDP OPTIONS nobody answers is "unknown", not "unreachable", and still
// exits 3. --timeout bounds the wait.
func TestSIPTest_silentUDPIsUnknown(t *testing.T) {
	setEmptyHome(t)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	port := pc.LocalAddr().(*net.UDPAddr).Port

	start := time.Now()
	err, stdout, _ := execCmd(t, "sip", "test", "--uri", fmt.Sprintf("127.0.0.1:%d;transport=udp", port), "--timeout", "1", "-o", "json")
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("--timeout 1 was not respected: took %s", took)
	}
	code, ce := sipTestExit(t, err)
	if code != 3 || !strings.Contains(ce.Hint, "ignore SIP from addresses they do not know") {
		t.Fatalf("want exit 3 with the hosted-platform hint, got %d: %+v", code, ce)
	}
	if res := decodeSIPTest(t, stdout); res.Result != "unknown" || res.Checks[1].Status != "unknown" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestSIPTest_unresolvableHostIsUnreachable(t *testing.T) {
	setEmptyHome(t)
	stubLookup(t, errors.New("no such host"))

	err, stdout, _ := execCmd(t, "sip", "test", "--uri", "sip.example.com;transport=tcp", "-o", "json")
	if code, ce := sipTestExit(t, err); code != 3 || ce.Context["check"] != "dns" {
		t.Fatalf("want exit 3 on the dns check, got %d: %v", code, err)
	}
	res := decodeSIPTest(t, stdout)
	if res.Result != "unreachable" || len(res.Checks) != 1 || res.Checks[0].Status != "fail" {
		t.Fatalf("nothing should be dialled after a DNS failure: %+v", res)
	}
}

// --dry-run prints the plan and touches nothing: no lookup, no socket.
func TestSIPTest_dryRunOnlyPrintsThePlan(t *testing.T) {
	for _, tc := range []struct {
		uri  string
		want []string
	}{
		{"sips:sip.example.com", []string{"[dry-run]", "dns  resolve sip.example.com", "tcp  connect to port 5061", "tls  handshake"}},
		{"sip.example.com", []string{"dns  resolve sip.example.com", "udp  send one SIP OPTIONS to port 5060"}},
		{"203.0.113.4:5080;transport=tcp", []string{"dns  skip", "tcp  connect to port 5080"}},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			setEmptyHome(t)
			calls := stubLookup(t, nil)
			err, stdout, stderr := execCmd(t, "sip", "test", "--uri", tc.uri, "--dry-run")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *calls != 0 || stdout != "" {
				t.Errorf("--dry-run resolved %d time(s) or printed a result: %q", *calls, stdout)
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("plan missing %q:\n%s", want, stderr)
				}
			}
		})
	}
}

func TestSIPTest_badInputIsRefusedBeforeAnyNetwork(t *testing.T) {
	for _, tc := range []struct {
		flag string
		args []string
	}{
		{"uri", []string{"sip", "test"}},
		{"uri", []string{"sip", "test", "--uri", "sip.example.com:0"}},
		{"uri", []string{"sip", "test", "--uri", "sips:sip.example.com;transport=udp"}},
		{"timeout", []string{"sip", "test", "--uri", "sip.example.com", "--timeout", "0"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setEmptyHome(t)
			calls := stubLookup(t, nil)
			err, _, _ := execCmd(t, tc.args...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag || ce.Context["flag"] != tc.flag {
				t.Fatalf("want BAD_FLAG on --%s, got %v", tc.flag, err)
			}
			if *calls != 0 {
				t.Error("validation must come before any lookup")
			}
		})
	}
}

func TestSIPTest_transportAndPortDefaults(t *testing.T) {
	for _, tc := range []struct {
		uri, transport string
		port           int
		defaulted      bool
	}{
		{"sip.example.com", "udp", 5060, true},
		{"sip.example.com;transport=tcp", "tcp", 5060, false},
		{"sip.example.com;transport=tls", "tls", 5061, false},
		{"sips:sip.example.com", "tls", 5061, false},
		{"sips:sip.example.com;transport=tcp", "tls", 5061, false},
		{"sip.example.com:5080;transport=tls", "tls", 5080, false},
	} {
		u, err := parseSIPURI(tc.uri)
		if err != nil {
			t.Fatalf("%q: %v", tc.uri, err)
		}
		transport, defaulted := u.testTransport()
		if transport != tc.transport || defaulted != tc.defaulted || u.testPort(transport) != tc.port {
			t.Errorf("%q: got %s:%d defaulted=%v, want %s:%d defaulted=%v",
				tc.uri, transport, u.testPort(transport), defaulted, tc.transport, tc.port, tc.defaulted)
		}
	}
}
