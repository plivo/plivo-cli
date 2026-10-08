package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var sipConnectNumber, sipConnectPlatform string

var sipConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Plan how to connect a number to a voice-agent platform",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var sipConnectPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Print every step to connect a number to a SIP platform (changes nothing)",
	Long: `Print everything needed to connect a number to a voice-agent platform over SIP
Trunking: the origination URI, the credential or IP access control list, the
inbound and outbound trunks, the numbers update that routes the number, and
what to set up on the platform's side. It only reads, and changes nothing.

It checks the number first: it must be on this account, with voice enabled.
An India (+91) number must pass the compliance check ` + "`numbers update`" + ` runs,
and the platform must support Indian numbers: Vapi does not. The plan stops at
the first check that fails and exits non-zero.

Each printed command ends in --dry-run: run it to preview the request, then
again without --dry-run to create. -o json returns checks[], requests[] and
next_actions[].`,
	Example: `  plivo sip connect plan --number +14155551234 --platform livekit
  plivo sip connect plan --number +14155551234 --platform vapi -o json`,
	Args: cobra.NoArgs,
	RunE: runSIPConnectPlan,
}

func init() {
	f := sipConnectPlanCmd.Flags()
	f.StringVar(&sipConnectNumber, "number", "", "number to connect, e.g. +14155551234 (required)")
	f.StringVar(&sipConnectPlatform, "platform", "", "livekit|elevenlabs|retell|vapi (required)")
	sipConnectCmd.AddCommand(sipConnectPlanCmd)
	sipCmd.AddCommand(sipConnectCmd)
}

type connectCheck struct {
	Name   string `json:"name"`   // number, voice, country, routing, platform or compliance
	Status string `json:"status"` // pass, warn, fail or info
	Detail string `json:"detail"`
}

// connectRequest is one step of the plan: the command to run, and the request
// it makes.
type connectRequest struct {
	Step    int    `json:"step"`
	Purpose string `json:"purpose"`
	Command string `json:"command"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Returns string `json:"returns,omitempty"`
}

type connectPlan struct {
	Number      string           `json:"number"`
	Platform    string           `json:"platform"`
	Checks      []connectCheck   `json:"checks"`
	Requests    []connectRequest `json:"requests"`
	NextActions []string         `json:"next_actions"`
}

func (c *connectPlan) add(name, status, detail string) {
	c.Checks = append(c.Checks, connectCheck{name, status, detail})
}

func runSIPConnectPlan(cmd *cobra.Command, _ []string) error {
	digits := trimPlus(sipConnectNumber)
	if digits == "" {
		return clierr.BadFlag("number", "required: the number to connect, e.g. +14155551234")
	}
	if strings.Trim(digits, "0123456789") != "" || len(digits) > 15 {
		return clierr.BadFlag("number", fmt.Sprintf("%q is not an E.164 number such as +14155551234", sipConnectNumber))
	}
	if sipConnectPlatform == "" {
		return clierr.BadFlag("platform", "required: one of "+strings.Join(sipPlatformNames(), ", "))
	}
	p, err := sipPlatformFlag(sipConnectPlatform)
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}

	plan := connectPlan{Number: "+" + digits, Platform: p.name, Requests: []connectRequest{}, NextActions: []string{}}
	if err := plan.check(client, p, digits); err != nil {
		if rerr := renderConnectPlan(plan); rerr != nil {
			return rerr
		}
		return err
	}
	plan.build(p, digits)
	return renderConnectPlan(plan)
}

// check runs the checks in order and stops at the first that fails, returning
// its error. Every read goes through readThrough, so --dry-run changes nothing
// here: the plan only ever reads.
func (c *connectPlan) check(client *api.Client, p *sipPlatform, digits string) error {
	var n api.Number
	if e := getForCheck(client, client.AccountURL("Number", digits), &n); e != nil {
		c.add("number", "fail", "could not read it on this account: "+e.Message)
		if e.Code == clierr.CodeResourceNotFound {
			e.Hint = "`plivo numbers list` shows the numbers on this account."
		}
		return e
	}
	c.add("number", "pass", c.Number+" is on this account")

	var voice struct {
		Enabled *bool `json:"voice_enabled"`
	}
	_ = json.Unmarshal(n.Raw(), &voice)
	switch {
	case voice.Enabled == nil:
		c.add("voice", "warn", "the number record does not say whether voice is enabled")
	case !*voice.Enabled:
		c.add("voice", "fail", "voice is not enabled, so the number cannot take calls")
		return &clierr.Error{
			Code:    clierr.CodeBadInput,
			Message: fmt.Sprintf("%s cannot be connected: voice is not enabled on it", c.Number),
			Hint:    "Connect a number with voice enabled; `plivo numbers get <number>` shows voice_enabled.",
		}
	default:
		c.add("voice", "pass", "voice is enabled")
	}

	india := strings.HasPrefix(digits, "91")
	country := n.Country
	if country == "" {
		country = "not on the number record"
	}
	if india {
		country += " (+91: India's rules apply)"
	}
	c.add("country", "info", country)

	switch {
	case strings.Contains(n.Application, "/Zentrunk/Trunk/"):
		c.add("routing", "info", "routed to SIP trunk "+n.ResolvedAppID()+"; the last step routes it to the new inbound trunk")
	case n.ResolvedAppID() != "":
		c.add("routing", "info", "routed to application "+n.ResolvedAppID()+"; the last step routes it to the new inbound trunk")
	default:
		c.add("routing", "info", "not routed; the last step routes it to the new inbound trunk")
	}
	if !india {
		return nil
	}

	if p.noIndia {
		c.add("platform", "fail", "Indian numbers on "+p.label+" "+p.india)
		return &clierr.Error{
			Code:    clierr.CodeBadInput,
			Message: fmt.Sprintf("%s is an Indian number, and Indian numbers on %s %s", c.Number, p.label, p.india),
			Hint:    "Plivo's India calling guide supports LiveKit (with region pinning) and ElevenLabs (with an India deployment).",
		}
	}
	c.add("platform", "warn", "Indian numbers on "+p.label+" "+p.india)

	warning, err := checkIndiaCompliance(client, digits)
	if err != nil {
		msg := err.Error()
		var ce *clierr.Error
		if errors.As(err, &ce) {
			msg = ce.Message
		}
		c.add("compliance", "fail", msg)
		return err
	}
	if warning != "" {
		c.add("compliance", "warn", warning)
	} else {
		c.add("compliance", "pass", "its compliance application is accepted")
	}
	return nil
}

// build writes the steps in the order they must run: the URI, the outbound
// auth, both trunks, then the route. Each reuses the --platform preset, so the
// plan and the commands can never disagree about a value.
func (c *connectPlan) build(p *sipPlatform, digits string) {
	const account = "/v1/Account/{auth_id}/"
	uri := ""
	switch {
	case strings.HasPrefix(digits, "91") && p.indiaHost != "":
		uri = " --uri " + p.indiaHost
	case p.hostSuffix != "":
		uri = " --uri " + p.hostExample()
	}
	auth := connectRequest{
		Step: 2, Purpose: "credential your platform signs its outbound calls with",
		Command: "printf '%s' \"$SIP_PASSWORD\" | plivo sip credentials create --name " + p.name +
			"-out --username <username> --password-stdin --dry-run",
		Method: "POST", Path: account + "Zentrunk/Credential/", Returns: "credential_uuid",
	}
	authFlag := "--credential <credential_uuid>"
	if p.outbound == "ip-acl" {
		auth.Purpose = "IP access control list for the platform's outbound calls"
		auth.Command = "plivo sip ip-acl create --name " + p.name + "-out --platform " + p.name + " --dry-run"
		auth.Path, auth.Returns = account+"Zentrunk/IPAccessControlList/", "ipacl_uuid"
		authFlag = "--ip-acl <ipacl_uuid>"
	}
	trunk := "plivo sip trunks create --name " + p.name + "-%s --direction %s --platform " + p.name + " %s --dry-run"
	c.Requests = []connectRequest{
		{Step: 1, Purpose: "origination URI inbound calls are sent to",
			Command: "plivo sip uris create --name " + p.name + "-in --platform " + p.name + uri + " --dry-run",
			Method:  "POST", Path: account + "Zentrunk/URI/", Returns: "uri_uuid"},
		auth,
		{Step: 3, Purpose: "inbound trunk",
			Command: fmt.Sprintf(trunk, "in", dirInbound, "--uri <uri_uuid>"),
			Method:  "POST", Path: account + "Zentrunk/Trunk/", Returns: "trunk_id"},
		{Step: 4, Purpose: "outbound trunk; its trunk_domain is what the platform dials",
			Command: fmt.Sprintf(trunk, "out", dirOutbound, authFlag),
			Method:  "POST", Path: account + "Zentrunk/Trunk/", Returns: "trunk_id"},
		{Step: 5, Purpose: "route the number to the inbound trunk",
			Command: "plivo numbers update " + digits + " --trunk-id <inbound_trunk_id> --dry-run",
			Method:  "POST", Path: account + "Number/" + digits + "/"},
	}

	c.NextActions = append(c.NextActions,
		"Run the steps in order: each first as printed, to preview, then again without --dry-run. "+
			"Fill in <uri_uuid>, <"+auth.Returns+"> and <inbound_trunk_id> (the trunk_id from step 3) from the earlier steps' output.")
	if p.hostSuffix != "" {
		c.NextActions = append(c.NextActions, "<project> is the subdomain of the SIP URI on your "+p.label+" project's settings page.")
	}
	if p.outbound == "credential" {
		c.NextActions = append(c.NextActions, "Step 2 reads the password from $SIP_PASSWORD on stdin: "+
			"set it and pick the <username> your platform will sign in with.")
	}
	if p.secure {
		c.NextActions = append(c.NextActions, "recommended: add --secure to step 4 (Plivo's "+p.label+
			" guide uses secure trunking for outbound calls; it is billed per minute).")
	}
	if strings.HasPrefix(digits, "91") {
		c.NextActions = append(c.NextActions, "India: Indian numbers on "+p.label+" "+p.india)
	}
	c.NextActions = append(c.NextActions, p.steps...)
	c.NextActions = append(c.NextActions, "Plivo's "+p.label+" guide: "+p.guideURL())
}

func renderConnectPlan(c connectPlan) error {
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, c, nil)
	}
	rows := [][]string{{"CHECK", "STATUS", "DETAIL"}}
	for _, ch := range c.Checks {
		rows = append(rows, []string{ch.Name, ch.Status, ch.Detail})
	}
	if err := output.Table(os.Stdout, rows); err != nil {
		return err
	}
	if len(c.Requests) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stdout, "\nSteps to connect %s to %s (nothing has been changed):\n", c.Number, c.Platform)
	for _, r := range c.Requests {
		fmt.Fprintf(os.Stdout, "  %d. %s\n", r.Step, output.SafeText(r.Command))
	}
	fmt.Fprintln(os.Stdout, "\nThen:")
	for _, a := range c.NextActions {
		fmt.Fprintf(os.Stdout, "  - %s\n", output.SafeText(a))
	}
	return nil
}
