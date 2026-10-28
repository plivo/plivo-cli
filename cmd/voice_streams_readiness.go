package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	readyFrom, readyTo, readyAnswerURL, readyAnswerMethod string
	readyFlags                                            simulateFlags
)

var voiceStreamsReadinessCmd = &cobra.Command{
	Use:   "readiness [number]",
	Short: "Check a number, or a caller ID and answer URL, is ready for a streamed call",
	Long: `Check what a streamed call needs, then simulate the call as
` + "`voice streams simulate`" + ` does. It places no call and changes nothing.

Inbound, readiness <number>: the number is on this account with voice
enabled; an Indian (+91) number has an accepted compliance application; it
is routed to an application with an answer URL, which is then simulated with
the application's method. A missing fallback answer URL or hangup URL, and a
development tunnel, are warnings; a host Plivo cannot reach (this machine, a
private address) fails. A Plivo-hosted answer URL (a console flow or an
agent) is not simulated, in either direction: a request there would run it
for real.

Outbound, readiness --from <caller ID> --to <number> --answer-url <url>: the
caller ID is a number on this account with voice enabled, or a verified
caller ID; an Indian caller ID has an accepted compliance application and
calls only Indian numbers; the account has cash credits. Geo permissions and
the concurrent call limit have no API, so they are unknown, never a pass.
--answer-method defaults to GET, as in ` + "`voice calls make`" + `.

Each account check names the request it read. A number with no country
check says so. result is ready, not_ready, or unknown when nothing failed
but something could not be checked; the exit is 0 unless a check fails.
--dry-run makes the account reads but sends nothing to the answer URL. -o
json prints checks[] and the simulation; when a check fails, only the error,
with both in error.context.`,
	Example: `  plivo voice streams readiness +14155551234
  plivo voice streams readiness +14155551234 --xml-only -o json
  plivo voice streams readiness --from +14155551234 --to +14155555678 --answer-url https://agent.example.com/answer`,
	Args: cobra.MaximumNArgs(1),
	RunE: runVoiceStreamsReadiness,
}

func init() {
	f := voiceStreamsReadinessCmd.Flags()
	f.StringVar(&readyFrom, "from", "", "outbound: the caller ID, e.g. +14155551234")
	f.StringVar(&readyTo, "to", "", "outbound: the number to call")
	f.StringVar(&readyAnswerURL, "answer-url", "", "outbound: the answer URL the call will use")
	f.StringVar(&readyAnswerMethod, "answer-method", http.MethodGet, "outbound: GET|POST")
	readyFlags.register(f)
	voiceStreamsCmd.AddCommand(voiceStreamsReadinessCmd)
}

// readiness is what readiness found: the account checks, then the call.
type readiness struct {
	Direction string         `json:"direction"`
	Number    string         `json:"number,omitempty"` // inbound: the number checked
	From      string         `json:"from,omitempty"`   // outbound: the caller ID
	To        string         `json:"to,omitempty"`     // outbound: the number called
	AppID     string         `json:"app_id,omitempty"` // inbound: the number's application
	Checks    []streamsCheck `json:"checks"`
	Simulate  *simulation    `json:"simulate"` // null when there was no answer URL to request, or under --dry-run
	Result    string         `json:"result"`   // ready, not_ready or unknown

	err *clierr.Error // the first failed check
}

func (r *readiness) add(name, status, detail, source string) {
	r.Checks = append(r.Checks, streamsCheck{name, status, detail, source})
}

func (r *readiness) fail(name, source string, e *clierr.Error) {
	r.add(name, "fail", e.Message, source)
	if r.err == nil {
		r.err = e
	}
}

// readSource names the API request a check read.
func readSource(parts ...string) string {
	return strings.TrimSuffix("GET /v1/Account/{auth_id}/"+strings.Join(parts, "/"), "/") + "/"
}

// isE164 reports whether digits, without the +, can be an E.164 number.
func isE164(digits string) bool {
	return digits != "" && strings.Trim(digits, "0123456789") == "" && len(digits) <= 15
}

func runVoiceStreamsReadiness(cmd *cobra.Command, args []string) error {
	var outbound []string
	for _, f := range []string{"from", "to", "answer-url", "answer-method"} {
		if cmd.Flags().Changed(f) {
			outbound = append(outbound, "--"+f)
		}
	}
	r := &readiness{Checks: []streamsCheck{}}
	switch {
	case len(args) == 1 && len(outbound) > 0:
		return clierr.BadInput("a number checks inbound calls, and " + strings.Join(outbound, ", ") +
			" outbound ones: pass one or the other")
	case len(args) == 1:
		if !isE164(trimPlus(args[0])) {
			return clierr.BadInput(fmt.Sprintf("%q is not an E.164 number such as +14155551234", args[0]))
		}
		r.Direction, r.Number = dirInbound, "+"+trimPlus(args[0])
	case len(outbound) == 0:
		return clierr.BadInput("pass a number to check inbound calls, or --from, --to and --answer-url to check outbound ones")
	default:
		for flag, v := range map[string]string{"from": readyFrom, "to": readyTo} {
			if !isE164(trimPlus(v)) {
				return clierr.BadFlag(flag, fmt.Sprintf("%q is not an E.164 number such as +14155551234", v))
			}
		}
		if err := checkAnswerFlags("answer-url", readyAnswerURL, "answer-method", &readyAnswerMethod); err != nil {
			return err
		}
		r.Direction, r.From, r.To = dirOutbound, "+"+trimPlus(readyFrom), "+"+trimPlus(readyTo)
	}
	if err := readyFlags.validate(); err != nil {
		return err
	}
	client, profile, err := getClient()
	if err != nil {
		return err
	}

	var o *simulateOpts
	if r.Direction == dirInbound {
		o = r.inbound(client)
	} else {
		o = r.outbound(client)
	}
	if o != nil {
		if u, _ := url.Parse(o.answerURL); plivoHosted(u) {
			r.add("simulate", "unknown", "not simulated: "+u.Hostname()+plivoHostedWhy, "")
			o = nil
		}
	}
	if o != nil {
		o.simulateFlags, o.strict = readyFlags, true
		signer := streamSigner{profile: profile, authID: client.AuthID, token: client.AuthToken}
		if dryRunFlag {
			printSimulatePlan(*o, signer)
		} else {
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			r.Simulate = simulateCall(ctx, *o, signer)
		}
	}
	return renderReadiness(r)
}

// inbound checks the number and its application, and returns the call to
// simulate: nil when there is no answer URL to request.
func (r *readiness) inbound(client *api.Client) *simulateOpts {
	digits := trimPlus(r.Number)
	src := readSource("Number", digits)
	var n api.Number
	if e := getForCheck(client, client.AccountURL("Number", digits), &n); e != nil {
		if e.Code == clierr.CodeResourceNotFound {
			e.Message, e.Hint = r.Number+" is not a number on this account", "`plivo numbers list` shows the numbers on this account."
		}
		r.fail("number", src, e)
		return nil
	}
	r.add("number", "pass", r.Number+" is on this account", src)
	r.voice(n, src)
	r.country(client, digits, n.Country)

	if r.AppID = n.ResolvedAppID(); r.AppID == "" || strings.Contains(n.Application, "/Zentrunk/Trunk/") {
		why := "it is not routed to an application, so a call gets no XML"
		if r.AppID != "" {
			why, r.AppID = "it is routed to SIP trunk "+r.AppID+", not to an application with an answer URL", ""
		}
		r.fail("application", src, streamCheckFailed(why, fmt.Sprintf("`plivo numbers update %s --app-id <app_id>` routes it to an application.", digits)))
		return nil
	}
	var app api.Application
	src = readSource("Application", r.AppID)
	if e := getForCheck(client, client.AccountURL("Application", r.AppID), &app); e != nil {
		r.fail("application", src, e)
		return nil
	}
	if !isHTTPURL(app.AnswerURL) {
		r.fail("application", src, streamCheckFailed(fmt.Sprintf("application %q has no http:// or https:// answer URL (%q)", app.AppName, app.AnswerURL),
			fmt.Sprintf("`plivo account applications update %s --answer-url <url>` sets one.", r.AppID)))
		return nil
	}
	r.add("application", "pass", fmt.Sprintf("routed to application %q", app.AppName), src)
	for _, u := range []struct{ name, value, missing string }{
		{"fallback_answer_url", app.FallbackAnswerURL, "not set: when the answer URL fails, the call ends with 7011 instead of trying another URL"},
		{"hangup_url", app.HangupURL, "not set: nothing tells your server when a call ends"},
	} {
		if u.value == "" {
			r.add(u.name, "warn", u.missing, src)
		} else {
			r.add(u.name, "pass", u.value, src)
		}
	}
	method := strings.ToUpper(app.AnswerMethod)
	if method != http.MethodGet {
		method = http.MethodPost // the API's default
	}
	return &simulateOpts{answerURL: app.AnswerURL, method: method, params: callParams(sampleCaller, r.Number, dirInbound)}
}

// outbound checks the caller ID, the destination and the account, and
// returns the call to simulate.
func (r *readiness) outbound(client *api.Client) *simulateOpts {
	from := trimPlus(r.From)
	india := strings.HasPrefix(from, "91")
	src := readSource("Number", from)
	var n api.Number
	e := getForCheck(client, client.AccountURL("Number", from), &n)
	switch {
	case e == nil:
		r.add("caller_id", "pass", r.From+" is a number on this account", src)
		r.voice(n, src)
	case e.Code != clierr.CodeResourceNotFound:
		r.fail("caller_id", src, e)
	case india:
		r.fail("caller_id", src, streamCheckFailed(r.From+" is not a number on this account, and an Indian caller ID must be one",
			"`plivo numbers list` shows the numbers on this account."))
	default:
		src = readSource("VerifiedCallerId", r.From)
		var verified struct{}
		switch ve := getForCheck(client, client.AccountURL("VerifiedCallerId", r.From), &verified); {
		case ve == nil:
			r.add("caller_id", "pass", r.From+" is a verified caller ID on this account", src)
		// An unverified number answers 400 "Phone Number not found" (seen live).
		case ve.StatusCode == http.StatusNotFound || strings.Contains(strings.ToLower(ve.Message), "not found"):
			r.fail("caller_id", src, streamCheckFailed(r.From+" is neither a number on this account nor a verified caller ID: the call would fail with 3030 (Unknown Caller ID)",
				"`plivo numbers list` shows the numbers on this account."))
		default:
			r.fail("caller_id", src, ve)
		}
	}
	if india {
		if strings.HasPrefix(trimPlus(r.To), "91") {
			r.add("country", "pass", "an Indian caller ID calling an Indian number", "")
		} else {
			r.fail("country", "", streamCheckFailed(r.To+" is outside India, and an Indian caller ID calls only within India", ""))
		}
	}
	if !india || e == nil { // an Indian caller ID not on this account already failed
		r.country(client, from, "")
	}
	r.add("geo_permissions", "unknown", "no API reports them: Voice > Geo Permissions in the console, https://cx.plivo.com/geo-permissions/voice", "")
	r.balance(client)
	r.add("concurrency", "unknown", "no API reports the concurrent call limit: Organization settings > Account limits in the console, https://cx.plivo.com/organization-settings/limits", "")
	return &simulateOpts{answerURL: readyAnswerURL, method: readyAnswerMethod, params: callParams(r.From, r.To, dirOutbound)}
}

// voice checks the number record's voice_enabled; absent is not a pass.
func (r *readiness) voice(n api.Number, src string) {
	var v struct {
		Enabled *bool `json:"voice_enabled"`
	}
	_ = json.Unmarshal(n.Raw(), &v)
	switch {
	case v.Enabled == nil:
		r.add("voice", "warn", "the number record does not say whether voice is enabled", src)
	case !*v.Enabled:
		r.fail("voice", src, streamCheckFailed("voice is not enabled on +"+n.Number+", so it cannot carry calls", ""))
	default:
		r.add("voice", "pass", "voice is enabled", src)
	}
}

// country runs India's compliance rule on a +91 number. Any other number
// gets a line saying no country check applies, so a shorter run never reads
// as a skipped check.
func (r *readiness) country(client *api.Client, digits, name string) {
	if !strings.HasPrefix(digits, "91") {
		if name == "" {
			name = "+" + digits
		}
		r.add("country", "info", "no country-specific check applies to "+name, "")
		return
	}
	src := readSource("PhoneNumber", "Compliance", "{compliance_application_id}")
	if e := clierr.Wrap(checkIndiaCompliance(client, digits)); e != nil {
		// The rule is worded for routing a number; keep only its reason.
		e.Message = "+" + digits + ": " + strings.TrimPrefix(e.Message, "refusing to route "+digits+": ")
		r.fail("compliance", src, e)
		return
	}
	r.add("compliance", "pass", "its compliance application is accepted", src)
}

// balance reads cash_credits: unreadable is unknown, and so is 0 on an
// account that is not prepaid.
func (r *readiness) balance(client *api.Client) {
	src := readSource()
	var a api.Account
	if e := getForCheck(client, client.AccountURL(), &a); e != nil {
		r.add("balance", "unknown", "could not read the account: "+e.Message, src)
		return
	}
	credits, err := strconv.ParseFloat(a.CashCredits, 64)
	switch {
	case err != nil:
		r.add("balance", "unknown", fmt.Sprintf("cash_credits %q is not a number", a.CashCredits), src)
	case credits > 0:
		r.add("balance", "pass", "cash_credits is above 0", src)
	case a.BillingMode == "prepaid":
		r.fail("balance", src, streamCheckFailed("cash_credits is 0 or less on a prepaid account, so calls cannot be placed",
			"Add credits on the console home page, https://cx.plivo.com/home."))
	default:
		r.add("balance", "unknown", fmt.Sprintf("cash_credits is 0 or less, and billing_mode is %q", a.BillingMode), src)
	}
}

func renderReadiness(r *readiness) error {
	err := r.err
	if err == nil && r.Simulate != nil {
		err = r.Simulate.err
	}
	r.Result = "ready"
	for _, c := range r.Checks {
		if c.Status == "unknown" {
			r.Result = "unknown"
		}
	}
	switch {
	case err != nil:
		r.Result = "not_ready"
	case r.Simulate == nil:
		r.Result = "unknown" // the call was not simulated
	}
	if effectiveFormat() == output.FormatJSON {
		if err != nil {
			return withContext(err, map[string]any{"checks": r.Checks, "simulate": r.Simulate, "result": r.Result})
		}
		return output.JSONSuccess(os.Stdout, r, nil)
	}
	if rerr := renderStreamsChecks(r.Checks); rerr != nil {
		return rerr
	}
	if s := r.Simulate; s != nil {
		fmt.Fprintf(os.Stdout, "\nSimulated call %s: %s %s\n", s.Params["CallUUID"], s.Method, output.SafeText(s.AnswerURL))
		if rerr := renderStreamsChecks(s.Checks); rerr != nil {
			return rerr
		}
	}
	fmt.Fprintf(os.Stdout, "\nresult: %s\n", r.Result)
	if err != nil {
		return err
	}
	return nil // never a nil *clierr.Error in an error
}
