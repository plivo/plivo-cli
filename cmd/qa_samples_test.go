package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// qaSamples holds a valid invocation of every command, so the sweeps can run
// them all: positional arguments first, then the flags that make it reach
// the API. {uuid} and the like stand for well-formed IDs (see qaSampleIDs).
// A new command needs a sample here, or a reason in qaLocalOnly;
// TestQA_samplesCoverEveryCommand fails until it has one.
var qaSamples = map[string]string{
	"account applications create": "--app-name qa --answer-url https://example.com/answer",
	"account applications delete": "{app}",
	"account applications get":    "{app}",
	"account applications list":   "",
	"account applications update": "{app} --app-name qa",
	"account get":                 "",
	"account subaccounts create":  "--name qa",
	"account subaccounts delete":  "{sub}",
	"account subaccounts get":     "{sub}",
	"account subaccounts list":    "",
	"account subaccounts update":  "{sub} --name qa",
	"account update":              "--name qa",
	"account usage":               "",

	"agents create":      "--name qa",
	"agents delete":      "{uuid}",
	"agents get":         "{uuid}",
	"agents list":        "",
	"agents nodes get":   "speak",
	"agents nodes list":  "",
	"agents pause":       "{uuid}",
	"agents publish":     "{uuid}",
	"agents resume":      "{uuid}",
	"agents runs get":    "{uuid} {uuid2}",
	"agents runs list":   "{uuid}",
	"agents update":      "{uuid} --name qa",
	"api":                "GET /Subaccount/",
	"lookup":             "{number}",
	"messaging get":      "{uuid}",
	"messaging mms list": "",
	"messaging mms send": "--src {number} --dst {number2} --text hi --media-url https://example.com/a.png",

	"messaging sms 10dlc brands create":    "--alias qa --legal-name QA",
	"messaging sms 10dlc brands get":       "{brand}",
	"messaging sms 10dlc brands list":      "",
	"messaging sms 10dlc brands update":    "{brand} --email qa@example.com",
	"messaging sms 10dlc campaigns create": "--alias qa --brand-id {brand} --usecase MARKETING --description qa --message-flow qa --sample-message-1 qa",
	"messaging sms 10dlc campaigns get":    "{campaign}",
	"messaging sms 10dlc campaigns list":   "",
	"messaging sms 10dlc campaigns update": "{campaign} --description qa",
	"messaging sms 10dlc links create":     "--campaign-id {campaign} --number {number}",
	"messaging sms 10dlc links delete":     "{uuid}",
	"messaging sms 10dlc links list":       "",

	"messaging sms diagnose":                  "{uuid}",
	"messaging sms list":                      "",
	"messaging sms powerpacks create":         "--name qa",
	"messaging sms powerpacks delete":         "{uuid}",
	"messaging sms powerpacks get":            "{uuid}",
	"messaging sms powerpacks list":           "",
	"messaging sms powerpacks numbers add":    "{uuid} {number}",
	"messaging sms powerpacks numbers list":   "{uuid}",
	"messaging sms powerpacks numbers remove": "{uuid} {number}",
	"messaging sms powerpacks update":         "{uuid} --name qa",
	"messaging sms send":                      "--src {number} --dst {number2} --text hi",
	"messaging sms tollfree get":              "{uuid}",
	"messaging sms tollfree list":             "",
	"messaging sms tollfree submit":           "--business-name qa --use-case qa",
	"messaging mms diagnose":                  "{uuid}",
	"messaging whatsapp diagnose":             "{uuid}",
	"messaging whatsapp list":                 "",
	"messaging whatsapp send":                 "--src {number} --dst {number2} --text hi",

	"numbers buy":                     "{number}",
	"numbers cnam":                    "{number}",
	"numbers compliance create":       `--data {"country_iso":"US","number_type":"local"}`,
	"numbers compliance delete":       "{uuid}",
	"numbers compliance get":          "{uuid}",
	"numbers compliance link":         "--link {number}={uuid}",
	"numbers compliance list":         "",
	"numbers compliance requirements": "--country US --number-type local --user-type business",
	"numbers compliance update":       `{uuid} --data {"alias":"qa"}`,
	"numbers get":                     "{number}",
	"numbers list":                    "",
	"numbers masking sessions create": "--first-party {number} --second-party {number2}",
	"numbers masking sessions delete": "{uuid}",
	"numbers masking sessions get":    "{uuid}",
	"numbers masking sessions list":   "",
	"numbers release":                 "{number}",
	"numbers search":                  "--country US",
	"numbers update":                  "{number} --alias qa",

	"sip calls diagnose":       "{uuid}",
	"sip calls get":            "{uuid}",
	"sip calls insights":       "{uuid}",
	"sip calls list":           "--direction inbound --from-number {number} --hangup-source carrier",
	"sip connect plan":         "--number {number} --platform vapi",
	"sip credentials create":   "--name qa --username qauser01 --password-stdin",
	"sip credentials delete":   "{uuid}",
	"sip credentials get":      "{uuid}",
	"sip credentials list":     "",
	"sip credentials update":   "{uuid} --name qa --username qauser02 --password-stdin",
	"sip ip-acl create":        "--name qa --ip 203.0.113.10",
	"sip ip-acl delete":        "{uuid}",
	"sip ip-acl get":           "{uuid}",
	"sip ip-acl list":          "",
	"sip ip-acl update":        "{uuid} --name qa --ip 203.0.113.10",
	"sip trunks create":        "--name qa --direction inbound --uri {uuid}",
	"sip trunks delete":        "{trunk}",
	"sip trunks get":           "{trunk}",
	"sip trunks list":          "--direction inbound",
	"sip trunks update":        "{trunk} --name qa --status disabled --uri {uuid} --fallback-uri {uuid2}",
	"sip uris create":          "--name qa --uri sip.example.com --authentication-needed --username qauser01 --password-stdin",
	"sip uris delete":          "{uuid}",
	"sip uris get":             "{uuid}",
	"sip uris list":            "",
	"sip uris update":          "{uuid} --name qa",
	"verify sessions create":   "--recipient {number} --app-uuid {uuid}",
	"verify sessions get":      "{uuid}",
	"verify sessions list":     "",
	"verify sessions validate": "{uuid} --otp 123456",

	"voice calls diagnose":          "{uuid}",
	"voice calls dtmf":              "{uuid} --digits 12",
	"voice calls get":               "{uuid}",
	"voice calls hangup":            "{uuid}",
	"voice calls list":              "",
	"voice calls make":              "--from {number} --to {number2}",
	"voice calls play":              "{uuid} --urls https://example.com/a.mp3",
	"voice calls record":            "{uuid}",
	"voice calls speak":             "{uuid} --text hi",
	"voice calls stop-play":         "{uuid}",
	"voice calls stop-record":       "{uuid}",
	"voice calls stop-speak":        "{uuid}",
	"voice calls streams get":       "{uuid} {uuid2}",
	"voice calls streams list":      "{uuid}",
	"voice calls streams start":     "{uuid} --url wss://example.com/stream --stream-status-callback https://example.com/cb",
	"voice calls streams stop":      "{uuid} {uuid2}",
	"voice calls transfer":          "{uuid} --aleg-url https://example.com/x",
	"voice conferences get":         "{room}",
	"voice conferences hangup":      "{room}",
	"voice conferences list":        "",
	"voice conferences record":      "{room}",
	"voice conferences stop-record": "{room}",

	"voice conferences member deaf":       "{room} {member}",
	"voice conferences member kick":       "{room} {member}",
	"voice conferences member mute":       "{room} {member}",
	"voice conferences member play":       "{room} {member} --urls https://example.com/a.mp3",
	"voice conferences member speak":      "{room} {member} --text hi",
	"voice conferences member stop-play":  "{room} {member}",
	"voice conferences member stop-speak": "{room} {member}",
	"voice conferences member undeaf":     "{room} {member}",
	"voice conferences member unmute":     "{room} {member}",

	"voice endpoints create": "--username qauser01 --password {password} --alias qa",
	"voice endpoints delete": "{endpoint}",
	"voice endpoints get":    "{endpoint}",
	"voice endpoints list":   "",
	"voice endpoints update": "{endpoint} --alias qa",

	"voice multiparty end":                "{uuid}",
	"voice multiparty get":                "{uuid}",
	"voice multiparty list":               "",
	"voice multiparty participant add":    "{uuid} --from {number} --to {number2}",
	"voice multiparty participant hold":   "{uuid} {member}",
	"voice multiparty participant kick":   "{uuid} {member}",
	"voice multiparty participant list":   "{uuid}",
	"voice multiparty participant mute":   "{uuid} {member}",
	"voice multiparty participant unhold": "{uuid} {member}",
	"voice multiparty participant unmute": "{uuid} {member}",
	"voice recordings delete":             "{uuid}",
	"voice recordings get":                "{uuid}",
	"voice recordings list":               "",
}

// qaAfterFix holds the samples a pending fix changes, under the finding it
// fixes. While that finding is pending the sample in qaSamples runs (and is
// skipped if it stops running); once its entry leaves qaPending this one runs
// instead, or none for a command the fix retires (qaRetired).
var qaAfterFix = map[string]struct{ owner, sample string }{
	"messaging sms 10dlc brands create": {"r3/D1", "--profile-uuid {uuid} --alias qa"},
	"messaging sms 10dlc brands update": {"r3/D1", qaRetired},
	"messaging sms 10dlc campaigns create": {"r3/D2", "--alias qa --brand-id {brand} --vertical TECHNOLOGY --usecase MARKETING " +
		"--description qa --message-flow qa --sample-message-1 qa --sample-message-2 qa --help-message qa --opt-out-message qa " +
		"--subscriber-optin --subscriber-optout --subscriber-help"},
	"messaging sms 10dlc links delete": {"r3/links", "{campaign} {number}"},
	"messaging sms 10dlc links list":   {"r3/links", "--campaign-id {campaign}"},
	"messaging sms tollfree submit": {"r3/D3", "--profile-uuid {uuid} --number {number} --use-case MARKETING --use-case-summary qa " +
		"--production-message-content qa --opt-in-type VERBAL --opt-in-image-url https://example.com/optin.png --message-volume 1000 " +
		"--terms-and-conditions-link https://example.com/terms --privacy-policy-link https://example.com/privacy"},
	"voice conferences member play": {"r3/D6", "{room} {member} --url https://example.com/a.mp3"},
}

// qaRetired marks a command a fix retires: removes it, or leaves it sending
// nothing.
const qaRetired = "(retired)"

// qaRetiredNow reports whether the fix that retires the command is in.
func qaRetiredNow(key string) bool {
	fix, ok := qaAfterFix[key]
	return ok && fix.sample == qaRetired && qaPending[fix.owner] == ""
}

// qaSampleFor is the sample to run for a command, and whether it has one.
func qaSampleFor(key string) (string, bool) {
	if fix, ok := qaAfterFix[key]; ok && qaPending[fix.owner] == "" {
		return fix.sample, fix.sample != qaRetired
	}
	sample, ok := qaSamples[key]
	return sample, ok
}

// qaLocalOnly are the commands that never call a Plivo API, with what they do
// instead. The request sweeps skip them; the --dry-run sweep still runs the
// ones with a sample.
var qaLocalOnly = map[string]string{
	"ask":                     "talks to the assistant (covered by the regression cases)",
	"auth list":               "reads the local profiles",
	"auth remove":             "edits the local profiles and keychain",
	"auth use":                "edits the local profiles",
	"auth whoami":             "reads the local profile",
	"config get":              "reads the local config",
	"config set":              "edits the local config",
	"config telemetry":        "edits the local config",
	"docs list":               "reads the docs index",
	"docs search":             "reads the docs index",
	"docs show":               "reads a docs page",
	"feedback":                "posts feedback to Plivo",
	"login":                   "opens a browser",
	"logout":                  "edits the local profiles and keychain",
	"open":                    "opens a browser",
	"sip test":                "probes the given SIP host itself",
	"skill install":           "writes skill files",
	"skill list":              "reads skill files",
	"skill update":            "writes skill files",
	"support":                 "talks to the assistant",
	"upgrade":                 "downloads a release",
	"voice multiparty create": "retired: says how to start a multiparty call, sends nothing",
	"voice streams forward":   "runs a tunnel and a local proxy",
	"voice streams test":      "dials the given WebSocket itself",
}

// qaLocalSamples are runnable invocations of local commands for the --dry-run
// sweep: each must change nothing on disk. upgrade has none: it reads the
// release from GitHub first, which the suite cannot reach.
var qaLocalSamples = map[string]string{
	"ask":                "-i",
	"auth remove":        "test",
	"auth use":           "other",
	"config set":         "telemetry false",
	"config telemetry":   "off",
	"docs list":          "",
	"docs search":        "call",
	"docs show":          "voice/api/calls",
	"login":              "",
	"logout":             "test",
	"open":               "call {uuid}",
	"skill install":      "cli",
	"skill update":       "",
	"voice streams test": "--to ws://127.0.0.1:9/ws",
}

// qaSampleIDs are the well-formed IDs the samples use.
var qaSampleIDs = strings.NewReplacer(
	"{uuid}", "d1e2f3a4-0000-4000-8000-000000000001",
	"{uuid2}", "d1e2f3a4-0000-4000-8000-000000000002",
	"{number}", "+14155550100",
	"{number2}", "+14155550101",
	"{app}", "31000000000000001",
	"{sub}", "SAQASUBACCOUNT1",
	"{trunk}", "21000000000001",
	"{endpoint}", "12345678",
	"{room}", "qa-room",
	"{member}", "11",
	"{brand}", "BQAFAKE1",
	"{campaign}", "CQAFAKE1",
	"{password}", "QaPass1!",
)

// qaStdin is what every sweep run reads on stdin: the --password-stdin
// commands need a valid password, and nothing else reads it.
const qaStdin = "QaPass1!\n"

// qaSample is one command and its sample arguments.
type qaSample struct {
	path  string   // "voice calls get"
	argv  []string // the command words, then the sample with IDs filled in
	words int      // how many of argv are command words
	ids   int      // how many arguments after those are positional
}

// withArg returns argv with positional argument i replaced by v.
func (s qaSample) withArg(i int, v string) []string {
	argv := append([]string(nil), s.argv...)
	argv[s.words+i] = v
	return argv
}

// qaAPISamples returns the samples of qaSamples in tree order, the fixed
// ones for findings no longer pending.
func qaAPISamples() []qaSample {
	table := map[string]string{}
	for key := range qaSamples {
		if sample, ok := qaSampleFor(key); ok {
			table[key] = sample
		}
	}
	return qaCollect(table)
}

// qaCollect turns a sample table into samples, in tree order.
func qaCollect(table map[string]string) []qaSample {
	var out []qaSample
	walkOwnCommands(func(c *cobra.Command, path []string) {
		key := strings.Join(path, " ")
		sample, ok := table[key]
		if !ok || c.HasSubCommands() || !c.Runnable() {
			return
		}
		args := strings.Fields(qaSampleIDs.Replace(sample))
		argv := append(append([]string(nil), path...), args...)
		out = append(out, qaSample{path: key, argv: argv, words: len(path), ids: qaPositionals(c, args)})
	})
	return out
}

// qaPositionals counts the sample's leading arguments that the command's Use
// line names as positional.
func qaPositionals(c *cobra.Command, args []string) int {
	n := 0
	for _, word := range strings.Fields(c.Use)[1:] {
		if n < len(args) && !strings.HasPrefix(args[n], "-") && (strings.HasPrefix(word, "<") || strings.HasPrefix(word, "[")) {
			n++
		}
	}
	return n
}

func TestQA_samplesCoverEveryCommand(t *testing.T) {
	seen := map[string]bool{}
	var retired []string
	walkOwnCommands(func(c *cobra.Command, path []string) {
		if c.HasSubCommands() || !c.Runnable() {
			return
		}
		key := strings.Join(path, " ")
		seen[key] = true
		if qaRetiredNow(key) {
			retired = append(retired, key)
			return
		}
		_, sample := qaSampleFor(key)
		_, local := qaLocalOnly[key]
		if sample == local && !strings.HasPrefix(key, "auth token") { // internal builds add auth token
			t.Errorf("%s needs exactly one of a sample in qaSamples or a reason in qaLocalOnly", key)
		}
	})
	if len(retired) > 0 { // a retired command still here must send nothing
		setFakeCreds(t)
		srv := newQAServer(t, qaSweepReplies)
		table := map[string]string{}
		for _, key := range retired {
			table[key] = qaSamples[key]
		}
		for _, s := range qaCollect(table) {
			if r := srv.run(t, qaStdin, append(s.argv, "--yes")...); len(r.reqs) > 0 {
				t.Errorf("%s is retired but sent %v", s.path, r.reqs)
			}
		}
	}
	for key := range qaAfterFix {
		if _, ok := qaSamples[key]; !ok {
			t.Errorf("qaAfterFix has %s, which has no sample in qaSamples", key)
		}
	}
	for _, m := range []map[string]string{qaSamples, qaLocalOnly, qaLocalSamples} {
		for key := range m {
			if seen[key] {
				continue
			}
			switch fix, ok := qaAfterFix[key]; {
			case ok && qaPending[fix.owner] != "":
				t.Errorf("%s is not a command: if its fix is in, delete %s from qaPending", key, fix.owner)
			case !ok || fix.sample != qaRetired:
				t.Errorf("%s is not a command", key)
			}
		}
	}
}
