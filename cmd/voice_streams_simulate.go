package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/plivo/plivo-cli/internal/version"
	"github.com/plivo/plivo-cli/internal/wsproxy"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	simAnswerURL, simAnswerMethod string
	simFrom, simTo, simDirection  string
	simFlags                      simulateFlags
)

var voiceStreamsSimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Act as Plivo on one call: request an answer URL, check its XML, stream to its <Stream>",
	Long: `Do what Plivo does when a call reaches your answer URL, from this machine
and without placing a call: request --answer-url with --answer-method and
the fields Plivo sends (CallUUID, From, To, Direction, CallStatus), signed
with X-Plivo-Signature-V3 when a profile is logged in; check the reply (a
2xx within Plivo's 15 seconds, its content type, and Plivo XML with a
<Stream>); then open the <Stream>'s WebSocket and stream --duration seconds
of a 1 kHz tone in its contentType, as ` + "`voice streams test`" + ` does, reading back
what the voice agent sends when the stream is bidirectional.

--xml-only stops after the XML and opens no WebSocket. Only the answer
document is read: no <Redirect> or action URL is followed.

A pass means your server answers this machine as Plivo needs, not that
Plivo can reach it or that a caller hears the audio. Exit 0 on a pass; 1
when a check fails, 2 when the WebSocket refuses the upgrade, 3 when a host
cannot be reached or the WebSocket closes before the stop frame. -o json
prints checks[] and the stream summary; when a check fails it prints only
the error, with checks[] in error.context.`,
	Example: `  plivo voice streams simulate --answer-url https://agent.example.com/answer
  plivo voice streams simulate --answer-url https://agent.example.com/answer --answer-method GET --to +14155551234 -o json
  plivo voice streams simulate --answer-url https://agent.example.com/answer --xml-only`,
	Args: cobra.NoArgs,
	RunE: runVoiceStreamsSimulate,
}

func init() {
	f := voiceStreamsSimulateCmd.Flags()
	f.StringVar(&simAnswerURL, "answer-url", "", "the answer URL to request, as Plivo would (required)")
	f.StringVar(&simAnswerMethod, "answer-method", http.MethodPost, "GET|POST, as set on the application")
	f.StringVar(&simFrom, "from", sampleCaller, "the caller, sent as From")
	f.StringVar(&simTo, "to", sampleCalled, "the number called, sent as To")
	f.StringVar(&simDirection, "direction", dirInbound, "inbound|outbound, sent as Direction")
	simFlags.register(f)
	voiceStreamsCmd.AddCommand(voiceStreamsSimulateCmd)
}

// simulateFlags are the flags simulate and readiness share.
type simulateFlags struct {
	xmlOnly, expectAudio, insecure bool
	duration                       int
}

func (f *simulateFlags) register(fs *pflag.FlagSet) {
	fs.BoolVar(&f.xmlOnly, "xml-only", false, "stop after the answer URL and its XML: open no WebSocket")
	fs.IntVar(&f.duration, "duration", 3, "seconds of synthetic audio to stream (max 30)")
	fs.BoolVar(&f.expectAudio, "expect-audio", false, "fail unless playAudio comes back; the audio is a tone, so only an echo-style agent or one that speaks first answers")
	fs.BoolVar(&f.insecure, "insecure", false, "skip TLS verification (self-signed dev certs only)")
}

// simulateOpts is one simulated call.
type simulateOpts struct {
	simulateFlags
	answerURL, method string
	params            map[string]string // the fields sent to the answer URL
	strict            bool              // readiness: hosts Plivo cannot reach fail, tunnels warn
}

func (f simulateFlags) validate() error {
	if f.duration <= 0 || f.duration > 30 {
		return clierr.BadFlag("duration", "must be 1..30 seconds")
	}
	if f.xmlOnly && f.expectAudio {
		return clierr.BadFlag("expect-audio", "needs the WebSocket, which --xml-only skips")
	}
	return nil
}

// isHTTPURL reports whether raw is an absolute http:// or https:// URL, the
// only kind Plivo requests.
func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// checkAnswerFlags validates an answer URL flag and its method flag; the
// method is upper-cased in place.
func checkAnswerFlags(urlFlag, answerURL, methodFlag string, method *string) error {
	if !isHTTPURL(answerURL) {
		return clierr.BadFlag(urlFlag, fmt.Sprintf("%q is not an http:// or https:// URL", answerURL))
	}
	*method = strings.ToUpper(strings.TrimSpace(*method))
	if *method != http.MethodGet && *method != http.MethodPost {
		return clierr.BadFlag(methodFlag, fmt.Sprintf("%q is not GET or POST", *method))
	}
	return nil
}

func runVoiceStreamsSimulate(cmd *cobra.Command, _ []string) error {
	if err := checkAnswerFlags("answer-url", simAnswerURL, "answer-method", &simAnswerMethod); err != nil {
		return err
	}
	if err := simFlags.validate(); err != nil {
		return err
	}
	if simDirection != dirInbound && simDirection != dirOutbound {
		return clierr.BadFlag("direction", fmt.Sprintf("%q is not inbound or outbound", simDirection))
	}
	signer, err := resolveStreamSigner()
	if err != nil {
		return err
	}
	o := simulateOpts{simulateFlags: simFlags, answerURL: simAnswerURL, method: simAnswerMethod,
		params: callParams(strings.TrimSpace(simFrom), strings.TrimSpace(simTo), simDirection)}
	if dryRunFlag {
		printSimulatePlan(o, signer)
		return nil
	}
	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return renderSimulation(simulateCall(ctx, o, signer))
}

// The From and To a simulated call sends unless told otherwise: the sample
// numbers in Plivo's stream protocol reference.
const sampleCaller, sampleCalled = "+14155551234", "+14155555678"

// callParams are the fields Plivo's XML reference says an answer URL gets.
// An outbound call's A leg also carries its own ids.
func callParams(from, to, direction string) map[string]string {
	id := uuid.NewString()
	p := map[string]string{"CallUUID": id, "From": from, "To": to, "Direction": direction, "CallStatus": "ringing"}
	if direction == dirOutbound {
		p["CallStatus"], p["ALegUUID"], p["ALegRequestUUID"] = "in-progress", id, id
	}
	return p
}

// streamsCheck is one finding of simulate or readiness.
type streamsCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, warn, fail, unknown or info
	Detail string `json:"detail"`
	Source string `json:"source,omitempty"` // readiness: the request the check read
}

// simulation is what one simulated call found.
type simulation struct {
	AnswerURL   string             `json:"answer_url"`
	Method      string             `json:"method"`
	Params      map[string]string  `json:"params"` // the fields sent, as Plivo sends them
	Signed      bool               `json:"signed"`
	HTTPStatus  int                `json:"http_status"` // 0 when no reply came
	LatencyMs   int64              `json:"latency_ms"`  // the answer URL's reply time
	ContentType string             `json:"content_type"`
	StreamURL   string             `json:"stream_url"`
	Checks      []streamsCheck     `json:"checks"`
	Stream      *streamsTestResult `json:"stream"` // null when no WebSocket was opened
	Result      string             `json:"result"` // pass or fail

	err *clierr.Error // the first failed check
}

func (s *simulation) add(name, status, detail string) {
	s.Checks = append(s.Checks, streamsCheck{Name: name, Status: status, Detail: detail})
}

// fail records a failed check; the first one is the run's error.
func (s *simulation) fail(name string, e *clierr.Error) {
	s.add(name, "fail", e.Message)
	if s.err == nil {
		s.err = e
	}
}

// streamCheckFailed is a failed check's error: exit 1, with the fix in hint.
func streamCheckFailed(msg, hint string) *clierr.Error {
	return &clierr.Error{Code: clierr.CodeValidation, Message: msg, Hint: hint}
}

// answerTimeout is how long Plivo waits for an answer URL's XML.
const answerTimeout = 15 * time.Second

// maxAnswerXML is the largest answer document Plivo's XML reference allows.
const maxAnswerXML = 100 << 10

// simulateCall requests the answer URL, checks its XML and streams to its
// <Stream>, stopping at the first check that rules out the next step.
func simulateCall(ctx context.Context, o simulateOpts, signer streamSigner) *simulation {
	s := &simulation{AnswerURL: o.answerURL, Method: o.method, Params: o.params, Signed: signer.token != "", Checks: []streamsCheck{}}
	u, _ := url.Parse(o.answerURL) // checked with the flags
	if !o.strict || s.hostCheck("answer_url", u) {
		if body := s.fetch(ctx, o, signer); body != nil {
			if doc, ok := s.checkXML(body); ok {
				st, codec, rate := s.checkStream(doc, o.strict)
				if s.err == nil && !o.xmlOnly {
					s.stream(ctx, st, codec, rate, o, signer)
				}
			}
		}
	}
	s.Result = "pass"
	if s.err != nil {
		s.Result = "fail"
	}
	return s
}

// fetch requests the answer URL as Plivo does and checks the reply; it
// returns the body, or nil when the XML cannot be checked.
func (s *simulation) fetch(ctx context.Context, o simulateOpts, signer streamSigner) []byte {
	form := url.Values{}
	for k, v := range o.params {
		form.Set(k, v)
	}
	target, body := o.answerURL, io.Reader(nil)
	if o.method == http.MethodGet {
		u, _ := url.Parse(o.answerURL)
		q := u.Query()
		for k, v := range form {
			q[k] = v
		}
		u.RawQuery = q.Encode()
		target = u.String()
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, o.method, target, body)
	if err == nil {
		req.Header, err = signer.sign(o.answerURL, o.method, o.params)
	}
	if err != nil {
		s.fail("answer_url", clierr.Wrap(err))
		return nil
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", version.UserAgent())
	client := &http.Client{
		Timeout: answerTimeout,
		// Plivo needs the XML from this URL itself: a redirect is a non-2xx answer.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if o.insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	start := time.Now()
	resp, err := client.Do(req)
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxAnswerXML+1))
		_ = resp.Body.Close()
	}
	s.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		s.fail("answer_url", &clierr.Error{
			Code:      clierr.CodeNetworkError,
			Message:   "could not reach the answer URL: " + sipNetFailure(err, answerTimeout),
			Hint:      "A real call would end with 7011 (Error Reaching Answer URL): check that the server runs and that the URL is right.",
			Retryable: true,
		})
		return nil
	}
	s.HTTPStatus = resp.StatusCode
	if resp.StatusCode/100 != 2 {
		msg := fmt.Sprintf("the answer URL returned HTTP %d", resp.StatusCode)
		if loc := resp.Header.Get("Location"); loc != "" {
			msg += " (a redirect to " + loc + ")"
		}
		hint := ""
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			hint = signatureHint(signer.profile)
		}
		s.fail("answer_url", streamCheckFailed(msg+": Plivo treats a non-2xx answer as 7011 (Error Reaching Answer URL)", hint))
		return nil
	}
	s.add("answer_url", "pass", fmt.Sprintf("HTTP %d in %d ms", resp.StatusCode, s.LatencyMs))

	s.ContentType = resp.Header.Get("Content-Type")
	if mt, _, _ := mime.ParseMediaType(s.ContentType); mt == "application/xml" || mt == "text/xml" {
		s.add("content_type", "pass", mt)
	} else {
		s.add("content_type", "warn", fmt.Sprintf("%q: Plivo's XML reference asks for application/xml or text/xml", s.ContentType))
	}
	if len(data) > maxAnswerXML {
		s.fail("xml", streamCheckFailed("the XML is larger than 100 KB, Plivo's limit", ""))
		return nil
	}
	return data
}

// signatureHint explains a 401 or 403 from a server that checks Plivo's
// signature.
func signatureHint(profile string) string {
	if profile == "" {
		return "No profile is logged in, so the request went unsigned, and a server that checks Plivo's signature refuses it. Run `plivo login`, then try again."
	}
	return fmt.Sprintf("The request was signed like Plivo's, with profile %q's auth token. If the server checks another account's token, pass --profile for that account.", profile)
}

// answerXML is what the checks read from an answer document: its top-level
// verbs in order, and each <Stream> among them.
type answerXML struct {
	verbs   []string
	streams []streamVerb
}

type streamVerb struct {
	attrs map[string]string
	url   string
	index int // its place among the verbs
}

// parseAnswerXML reads data as Plivo XML: one <Response> root and nothing
// outside it.
func parseAnswerXML(data []byte) (answerXML, error) {
	var doc answerXML
	dec := xml.NewDecoder(bytes.NewReader(data))
	// A declared charset such as ISO-8859-1: the parts checked are ASCII.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	depth, root, open := 0, "", -1
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return doc, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case depth == 0 && root != "":
				return doc, errors.New("it has more than one root element")
			case depth == 0:
				if root = t.Name.Local; root != "Response" {
					return doc, fmt.Errorf("its root element is <%s>, not <Response>", root)
				}
			case depth == 1:
				doc.verbs = append(doc.verbs, t.Name.Local)
				if t.Name.Local == "Stream" {
					attrs := map[string]string{}
					for _, a := range t.Attr {
						attrs[a.Name.Local] = a.Value
					}
					doc.streams = append(doc.streams, streamVerb{attrs: attrs, index: len(doc.verbs) - 1})
					open = len(doc.streams) - 1
					text.Reset()
				}
			}
			depth++
		case xml.EndElement:
			if depth--; depth == 1 && open >= 0 {
				doc.streams[open].url = strings.TrimSpace(text.String())
				open = -1
			}
		case xml.CharData:
			switch {
			case depth == 2 && open >= 0:
				text.Write(t)
			case depth == 0 && len(bytes.TrimSpace(t)) > 0:
				return doc, errors.New("it has text outside <Response>")
			}
		}
	}
	if root == "" {
		return doc, errors.New("it has no XML element")
	}
	return doc, nil
}

// checkXML checks the answer document is Plivo XML.
func (s *simulation) checkXML(body []byte) (answerXML, bool) {
	trimmed := bytes.TrimSpace(body)
	doc, err := parseAnswerXML(trimmed)
	why, hint := "", ""
	switch {
	case len(trimmed) == 0:
		why = "the answer URL returned an empty body"
	case trimmed[0] == '{' || trimmed[0] == '[':
		why, hint = "the answer URL returned JSON, not XML", "JSON usually means a console flow answers this URL; Plivo needs XML here."
	case err != nil:
		why = "the reply is not Plivo XML: " + err.Error()
	}
	if why != "" {
		s.fail("xml", streamCheckFailed(why+"; Plivo ends the call with 8011 (Invalid Answer XML)", hint))
		return doc, false
	}
	verbs := "no verbs"
	if len(doc.verbs) > 0 {
		verbs = strings.Join(doc.verbs, ", ")
	}
	s.add("xml", "pass", "<Response> with "+verbs)
	return doc, true
}

// streamContentTypes are the <Stream> contentType values Plivo documents.
var streamContentTypes = map[string]struct {
	codec string
	rate  int
}{
	"audio/x-mulaw;rate=8000": {wsproxy.CodecMulaw, 8000},
	"audio/x-l16;rate=8000":   {wsproxy.CodecL16, 8000},
	"audio/x-l16;rate=16000":  {wsproxy.CodecL16, 16000},
	"audio/x-l16;rate=24000":  {wsproxy.CodecL16, 24000},
}

// checkStream checks the first <Stream> as a call would use it, and returns
// it with the codec and rate to stream in. A check that fails sets s.err.
func (s *simulation) checkStream(doc answerXML, strict bool) (st streamVerb, codec string, rate int) {
	if len(doc.streams) == 0 {
		s.fail("stream", streamCheckFailed("the answer XML has no <Stream>, so no audio stream starts",
			"Only this document is read: no <Redirect> or action URL is followed. Point at the URL that returns the <Stream>."))
		return st, "", 0
	}
	st = doc.streams[0]
	s.StreamURL = st.url
	u, err := url.Parse(st.url)
	switch {
	case st.url == "":
		s.fail("stream", streamCheckFailed("the <Stream> has no URL", ""))
	case err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "":
		s.fail("stream", streamCheckFailed(fmt.Sprintf("the <Stream> URL %q is not a ws:// or wss:// URL", st.url), ""))
	case len(st.url) > 2048:
		s.fail("stream", streamCheckFailed("the <Stream> URL is longer than 2048 characters, Plivo's limit", ""))
	case !strict || s.hostCheck("stream", u):
		s.add("stream", "pass", st.url)
		if strict && u.Scheme == "ws" {
			s.add("stream", "warn", "ws:// sends the call's audio unencrypted; Plivo's guide asks for wss://")
		}
	}
	if n := len(doc.streams); n > 1 {
		s.add("stream", "warn", fmt.Sprintf("%d <Stream> elements: one runs per call, and only the first is tested", n))
	}

	a := st.attrs
	bidi := strings.EqualFold(a["bidirectional"], "true")
	ct := a["contentType"]
	if ct == "" {
		ct = "audio/x-l16;rate=8000"
		s.add("contentType", "warn", "not set, so Plivo streams its default, "+ct+": set the codec your voice agent expects")
	}
	f, ok := streamContentTypes[strings.ToLower(strings.ReplaceAll(ct, " ", ""))]
	if !ok {
		s.fail("contentType", streamCheckFailed(fmt.Sprintf("%q is not a contentType Plivo documents: audio/x-mulaw;rate=8000, or audio/x-l16 at rate 8000, 16000 or 24000", ct), ""))
	}
	switch track := a["audioTrack"]; {
	case track != "" && track != "inbound" && track != "outbound" && track != "both":
		s.fail("audioTrack", streamCheckFailed(fmt.Sprintf("%q is not inbound, outbound or both", track), ""))
	case bidi && (track == "outbound" || track == "both"):
		s.fail("audioTrack", streamCheckFailed(fmt.Sprintf("%q is not allowed with bidirectional=\"true\"", track), ""))
	}
	if !bidi {
		s.add("bidirectional", "warn", "not \"true\": the caller hears nothing the voice agent sends (right only for listening)")
	}
	if !strings.EqualFold(a["keepCallAlive"], "true") && st.index == len(doc.verbs)-1 {
		s.fail("keepCallAlive", streamCheckFailed("not \"true\", and nothing follows the <Stream>: the call ends as soon as the stream starts (4010)",
			"Add keepCallAlive=\"true\" to the <Stream>."))
	}
	if a["statusCallbackUrl"] == "" {
		s.add("statusCallbackUrl", "warn", "not set: nothing tells you when a stream drops (DroppedStream)")
	}
	return st, f.codec, f.rate
}

// tunnelDomains are the development tunnels Plivo's docs and this CLI use.
var tunnelDomains = []string{"ngrok.io", "ngrok.dev", "ngrok-free.app", "ngrok-free.dev", "trycloudflare.com", "lhr.life"}

// hostCheck is readiness's check that a real call can rely on u's host: it
// fails when Plivo cannot reach it and warns on a development tunnel.
func (s *simulation) hostCheck(name string, u *url.URL) bool {
	h := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, err := netip.ParseAddr(h); h == "localhost" || strings.HasSuffix(h, ".localhost") ||
		err == nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		s.fail(name, streamCheckFailed(h+" is a local or private host, which Plivo cannot reach",
			"Serve it from a public host, or through a tunnel while you test."))
		return false
	}
	for _, d := range tunnelDomains {
		if strings.HasSuffix(h, "."+d) {
			s.add(name, "warn", h+" is a development tunnel: calls fail once it stops, or restarts with a new URL")
		}
	}
	return true
}

// stream plays the call to the <Stream> as Plivo would: signed, with the
// answer request's CallUUID in its start frame.
func (s *simulation) stream(ctx context.Context, st streamVerb, codec string, rate int, o simulateOpts, signer streamSigner) {
	u, _ := url.Parse(st.url)
	header, err := signer.upgrade(u)
	if err != nil {
		s.fail("websocket", clierr.Wrap(err))
		return
	}
	account := signer.authID
	if account == "" {
		account = "test-account"
	}
	bidi := strings.EqualFold(st.attrs["bidirectional"], "true")
	res, err := streamsProbe{
		url: st.url, header: header, profile: signer.profile, codec: codec, rate: rate,
		duration: o.duration, readBack: bidi || o.expectAudio, insecure: o.insecure,
		streamID: uuid.NewString(), callID: o.params["CallUUID"], accountID: account,
	}.run(ctx, io.Discard, true)
	s.Stream = &res
	if err != nil {
		s.fail("websocket", clierr.Wrap(err))
		return
	}
	detail := fmt.Sprintf("connected in %d ms; %d frames sent", res.LatencyMs, res.FramesSent)
	if res.Bidirectional {
		detail += fmt.Sprintf(", %d back (%d playAudio)", res.FramesReadBack, res.PlayAudioFrames)
	}
	if res.FirstResponseMs != nil {
		detail += fmt.Sprintf("; first playAudio after %d ms", *res.FirstResponseMs)
	}
	if o.expectAudio && res.PlayAudioFrames == 0 {
		s.fail("websocket", streamCheckFailed("no playAudio came back from "+st.url,
			"The audio is a 1 kHz tone, not speech: only an echo-style voice agent, or one that speaks first, answers it."))
	} else {
		s.add("websocket", "pass", detail)
	}
	for _, w := range res.Warnings {
		s.add("websocket", "warn", w)
	}
}

// printSimulatePlan is --dry-run: the request a real run would send, and
// what it would check, with nothing sent.
func printSimulatePlan(o simulateOpts, signer streamSigner) {
	signed := "unsigned: no profile is logged in"
	if signer.profile != "" {
		signed = fmt.Sprintf("signed with profile %q", signer.profile)
	}
	keys := make([]string, 0, len(o.params))
	for k := range o.params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]string, len(keys))
	for i, k := range keys {
		fields[i] = k + "=" + o.params[k]
	}
	fmt.Fprintf(os.Stderr, "[dry-run] %s %s (%s); nothing is sent\n", o.method, o.answerURL, signed)
	fmt.Fprintf(os.Stderr, "  fields: %s\n", strings.Join(fields, " "))
	fmt.Fprintln(os.Stderr, "  then: check the reply (2xx within 15s), its content type and its XML")
	if !o.xmlOnly {
		fmt.Fprintf(os.Stderr, "  then: open the <Stream> WebSocket and stream %ds of a tone in its contentType\n", o.duration)
	}
}

// withContext puts a run's findings in its error's context, where -o json
// readers find them: a failed run prints nothing on stdout.
func withContext(e *clierr.Error, kv map[string]any) *clierr.Error {
	if e.Context == nil {
		e.Context = map[string]any{}
	}
	for k, v := range kv {
		e.Context[k] = v
	}
	return e
}

func renderStreamsChecks(checks []streamsCheck) error {
	rows := [][]string{{"CHECK", "STATUS", "DETAIL"}}
	for _, c := range checks {
		rows = append(rows, []string{c.Name, c.Status, output.SafeText(c.Detail)})
	}
	return output.Table(os.Stdout, rows)
}

func renderSimulation(s *simulation) error {
	if effectiveFormat() == output.FormatJSON {
		if s.err != nil {
			return withContext(s.err, map[string]any{"checks": s.Checks, "stream": s.Stream})
		}
		return output.JSONSuccess(os.Stdout, s, nil)
	}
	signed := "unsigned"
	if s.Signed {
		signed = "signed"
	}
	fmt.Fprintf(os.Stdout, "Simulated call %s: %s %s (%s)\n", s.Params["CallUUID"], s.Method, output.SafeText(s.AnswerURL), signed)
	if err := renderStreamsChecks(s.Checks); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\nresult: %s\n", s.Result)
	if s.err != nil {
		return s.err
	}
	if !quietFlag {
		fmt.Fprintln(os.Stdout, "A pass means your server answers this machine as Plivo needs, not that Plivo can reach it or that a caller hears the audio.")
	}
	return nil
}
