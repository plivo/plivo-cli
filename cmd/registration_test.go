package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// findCmd walks rootCmd by command name path and returns the matched command.
// Returns nil if any segment doesn't resolve.
func findCmd(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := rootCmd.Find(path)
	if err != nil || cmd == nil {
		t.Fatalf("command %q not found: %v", strings.Join(path, " "), err)
	}
	// cobra.Find returns the closest match; verify the matched cmd's name
	// is the LAST segment (otherwise it bailed out part-way).
	if len(path) > 0 && cmd.Name() != path[len(path)-1] {
		t.Fatalf("command %q resolved to %q (cobra fell back to parent)", strings.Join(path, " "), cmd.Name())
	}
	return cmd
}

// isFlagRequired checks the cobra annotation cobra uses to mark required flags.
func isFlagRequired(cmd *cobra.Command, flagName string) bool {
	f := cmd.Flags().Lookup(flagName)
	if f == nil {
		return false
	}
	annotations := f.Annotations[cobra.BashCompOneRequiredFlag]
	for _, v := range annotations {
		if v == "true" {
			return true
		}
	}
	return false
}

// ─── Top-level services registered ───────────────────────────────────────────

func TestRootCmd_allTopLevelGroupsRegistered(t *testing.T) {
	// The public-v1 top-level surface under `plivo` after the three-segment
	// grammar: service groups (voice/sms) + standalone services. `agent` is a
	// coming-soon stub; `contacto` + `auth token` are internal-only (build tag
	// `internal`) and verified separately in internal_registration_test.go.
	groups := []string{
		"account", "agent", "auth", "lookup", "numbers", "sms", "verify", "voice",
	}
	for _, g := range groups {
		t.Run(g, func(t *testing.T) {
			findCmd(t, g)
		})
	}
}

// ─── Subcommands registered under each group (full grammar path) ─────────────

func TestSubcommands_registered(t *testing.T) {
	cases := []struct {
		path  string // space-separated grammar path to the group
		verbs []string
	}{
		{"account", []string{"get", "update"}},
		{"auth", []string{"login", "list", "use", "remove", "whoami"}}, // `token` is internal-only
		{"account subaccounts", []string{"list", "get", "create", "update", "delete"}},
		{"account applications", []string{"create", "list", "get", "update", "delete"}},
		{"account compliance", []string{"list", "get", "delete"}},
		{"numbers", []string{"list", "get", "search", "buy", "update", "release", "cnam", "masking"}},
		{"numbers masking", []string{"sessions"}},
		{"voice endpoints", []string{"list", "get", "create", "update", "delete"}},
		{"voice calls", []string{
			"make", "list", "get",
			"hangup", "transfer",
			"play", "stop-play",
			"speak", "stop-speak",
			"dtmf",
			"record", "stop-record",
			"streams",
		}},
		{"voice conferences", []string{"list", "get", "hangup", "record", "stop-record", "member"}},
		{"voice multiparty", []string{"list", "get", "create", "end", "participant"}},
		{"voice calls streams", []string{"list", "get", "start", "stop"}},
		{"voice recordings", []string{"list", "get", "delete"}},
		{"verify", []string{"sessions"}},
		{"sms messages", []string{"send", "list", "get"}},
		{"sms 10dlc brands", []string{"list", "get", "create", "update"}},
		{"sms 10dlc campaigns", []string{"list", "get", "create", "update"}},
		{"sms 10dlc links", []string{"list", "create", "delete"}},
		{"sms tollfree", []string{"list", "get", "submit"}},
		{"sms powerpacks", []string{"list", "get", "create", "update", "delete", "numbers"}},
		// `agent` subcommands are internal-only; the public build ships a
		// flat coming-soon stub with no subcommands.
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.path, " ", "_"), func(t *testing.T) {
			group := strings.Fields(tc.path)
			for _, v := range tc.verbs {
				full := append(append([]string(nil), group...), v)
				if cmd := findCmdNoFail(full...); cmd == nil {
					t.Errorf("plivo %s — not registered", strings.Join(full, " "))
				}
			}
		})
	}
}

// ─── Nested subcommands (group → sub → verb) ────────────────────────────────

func TestNestedSubcommands_registered(t *testing.T) {
	nests := map[string][]string{
		"verify sessions":              {"create", "get", "list", "validate"},
		"numbers masking sessions":     {"create", "get", "list", "delete"},
		"voice conferences member":     {"kick", "mute", "unmute", "deaf", "undeaf", "play", "stop-play", "speak", "stop-speak"},
		"voice multiparty participant": {"list", "add", "kick", "mute", "unmute", "hold", "unhold"},
		"sms powerpacks numbers":       {"list", "add", "remove"},
		// `auth token` + `agent session` are internal-only (see internal_registration_test.go).
	}
	for path, verbs := range nests {
		path := path
		verbs := verbs
		t.Run(strings.ReplaceAll(path, " ", "_"), func(t *testing.T) {
			parts := strings.Fields(path)
			for _, v := range verbs {
				full := append(append([]string(nil), parts...), v)
				if cmd := findCmdNoFail(full...); cmd == nil {
					t.Errorf("plivo %s — not registered", strings.Join(full, " "))
				}
			}
		})
	}
}

// ─── Legacy aliases resolve through the arg-rewrite shim ─────────────────────

// Pre-grammar top-level commands (and their short aliases) must keep working
// via rewriteLegacyArgs, which expands them into their new grammar path.
func TestLegacyAliases_resolveViaShim(t *testing.T) {
	cases := []struct {
		legacy string // what the user types as the first word
		want   string // the command name it must ultimately resolve to
	}{
		{"call", "calls"},
		{"stream", "streams"},
		{"conference", "conferences"},
		{"conf", "conferences"},
		{"mpc", "multiparty"},
		{"recording", "recordings"},
		{"rec", "recordings"},
		{"endpoint", "endpoints"},
		{"ep", "endpoints"},
		{"message", "messages"},
		{"msg", "messages"},
		{"brand", "brands"},
		{"campaign", "campaigns"},
		{"camp", "campaigns"},
		{"link", "links"},
		{"powerpack", "powerpacks"},
		{"pp", "powerpacks"},
		{"tollfree", "tollfree"},
		{"tfv", "tollfree"},
		{"number", "numbers"},
		{"cnam", "cnam"},
		{"masking", "masking"},
		{"mask", "masking"},
		{"subaccount", "subaccounts"},
		{"sub", "subaccounts"},
		{"application", "applications"},
		{"app", "applications"},
		{"compliance", "compliance"},
	}
	for _, tc := range cases {
		t.Run(tc.legacy, func(t *testing.T) {
			rewritten := rewriteLegacyArgs([]string{tc.legacy})
			cmd, _, err := rootCmd.Find(rewritten)
			if err != nil || cmd == nil {
				t.Fatalf("legacy %q (→ %v) didn't resolve: %v", tc.legacy, rewritten, err)
			}
			if cmd.Name() != tc.want {
				t.Errorf("legacy %q (→ %v) resolved to %q, want %q", tc.legacy, rewritten, cmd.Name(), tc.want)
			}
		})
	}
}

// In-context cobra aliases (the short forms kept on each renamed command) must
// still resolve under their service parent.
func TestInContextAliases_resolve(t *testing.T) {
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"voice", "conf"}, "conferences"},
		{[]string{"voice", "rec"}, "recordings"},
		{[]string{"voice", "ep"}, "endpoints"},
		{[]string{"voice", "mpc"}, "multiparty"},
		{[]string{"voice", "calls", "stream"}, "streams"},
		{[]string{"sms", "msg"}, "messages"},
		{[]string{"sms", "pp"}, "powerpacks"},
		{[]string{"account", "sub"}, "subaccounts"},
		{[]string{"account", "app"}, "applications"},
		{[]string{"numbers", "mask"}, "masking"},
		{[]string{"verify", "session"}, "sessions"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, "_"), func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tc.path)
			if err != nil || cmd == nil {
				t.Fatalf("%v didn't resolve: %v", tc.path, err)
			}
			if cmd.Name() != tc.want {
				t.Errorf("%v resolved to %q, want %q", tc.path, cmd.Name(), tc.want)
			}
		})
	}
}

func TestLegacyAlias_mpcPartIsParticipant(t *testing.T) {
	// 'mpc part' → shim → 'voice multiparty part' → participant.
	rewritten := rewriteLegacyArgs([]string{"mpc", "part"})
	cmd, _, err := rootCmd.Find(rewritten)
	if err != nil || cmd == nil {
		t.Fatalf("'mpc part' (→ %v) didn't resolve: %v", rewritten, err)
	}
	if cmd.Name() != "participant" {
		t.Errorf("'mpc part' resolved to %q, want participant", cmd.Name())
	}
}

// ─── Required flags ──────────────────────────────────────────────────────────

func TestRequiredFlags(t *testing.T) {
	cases := []struct {
		path  []string
		flags []string
	}{
		{[]string{"sms", "messages", "send"}, []string{"src", "dst", "text"}},
		{[]string{"voice", "calls", "make"}, []string{"from", "to"}},
		{[]string{"voice", "calls", "play"}, []string{"urls"}},
		{[]string{"voice", "calls", "speak"}, []string{"text"}},
		{[]string{"voice", "calls", "dtmf"}, []string{"digits"}},
		{[]string{"account", "applications", "create"}, []string{"app-name", "answer-url"}},
		{[]string{"voice", "endpoints", "create"}, []string{"username", "password"}},
		{[]string{"account", "subaccounts", "create"}, []string{"name"}},
		{[]string{"numbers", "search"}, []string{"country"}},
		{[]string{"verify", "sessions", "create"}, []string{"recipient", "app-uuid"}},
		{[]string{"verify", "sessions", "validate"}, []string{"otp"}},
		{[]string{"numbers", "masking", "sessions", "create"}, []string{"first-party", "second-party"}},
		{[]string{"sms", "10dlc", "brands", "create"}, []string{"alias", "legal-name"}},
		{[]string{"sms", "10dlc", "campaigns", "create"}, []string{"alias", "brand-id", "usecase", "description", "message-flow", "sample-message-1"}},
		{[]string{"sms", "10dlc", "links", "create"}, []string{"number", "campaign-id"}},
		{[]string{"sms", "tollfree", "submit"}, []string{"business-name", "use-case"}},
		{[]string{"voice", "multiparty", "create"}, []string{"name"}},
		{[]string{"voice", "multiparty", "participant", "add"}, []string{"from", "to"}},
		{[]string{"voice", "conferences", "member", "play"}, []string{"urls"}},
		{[]string{"voice", "conferences", "member", "speak"}, []string{"text"}},
		{[]string{"voice", "calls", "streams", "start"}, []string{"url"}},
		{[]string{"sms", "powerpacks", "create"}, []string{"name"}},
		// `auth token mint --modules` is internal-only.
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, "_"), func(t *testing.T) {
			cmd := findCmd(t, tc.path...)
			for _, f := range tc.flags {
				if !isFlagRequired(cmd, f) {
					t.Errorf("plivo %s — flag --%s should be required", strings.Join(tc.path, " "), f)
				}
			}
		})
	}
}

// ─── Persistent (global) flags on rootCmd ───────────────────────────────────

func TestRootPersistentFlags(t *testing.T) {
	expected := []string{
		"profile", "output", "quiet", "no-color", "log-level",
		"yes", "dry-run", "explain", "timeout", "all",
		// "hodor-server" is internal-only (verified in internal_registration_test.go)
	}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			if rootCmd.PersistentFlags().Lookup(name) == nil {
				t.Errorf("persistent flag --%s missing on rootCmd", name)
			}
		})
	}
}

func TestRootPersistentFlags_shortNames(t *testing.T) {
	// Single-letter shortcuts that must stay stable for muscle memory.
	cases := map[string]string{
		"o": "output",
		"q": "quiet",
		"y": "yes",
	}
	for short, long := range cases {
		t.Run(short, func(t *testing.T) {
			f := rootCmd.PersistentFlags().Lookup(long)
			if f == nil {
				t.Fatalf("flag --%s missing", long)
			}
			if f.Shorthand != short {
				t.Errorf("--%s shorthand = %q, want %q", long, f.Shorthand, short)
			}
		})
	}
}

// ─── Args validators on positional-arg commands ─────────────────────────────

func TestArgsValidators(t *testing.T) {
	cases := []struct {
		path   []string
		desc   string
		validN int // expected number of positional args
	}{
		{[]string{"numbers", "get"}, "numbers get <number>", 1},
		{[]string{"numbers", "buy"}, "numbers buy <number>", 1},
		{[]string{"numbers", "release"}, "numbers release <number>", 1},
		{[]string{"account", "applications", "delete"}, "applications delete <app_id>", 1},
		{[]string{"voice", "calls", "hangup"}, "calls hangup <uuid>", 1},
		{[]string{"voice", "calls", "dtmf"}, "calls dtmf <uuid>", 1},
		{[]string{"voice", "calls", "streams", "get"}, "streams get <call_uuid> <stream_id>", 2},
		{[]string{"voice", "conferences", "member", "kick"}, "kick <conf> <member>", 2},
		{[]string{"voice", "multiparty", "participant", "kick"}, "participant kick <mpc> <part>", 2},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			cmd := findCmd(t, tc.path...)
			if cmd.Args == nil {
				t.Errorf("plivo %s — Args validator is nil; should be ExactArgs(%d) or similar", strings.Join(tc.path, " "), tc.validN)
				return
			}
			// Probe: call validator with right + wrong arg counts.
			args := make([]string, tc.validN)
			for i := range args {
				args[i] = "x"
			}
			if err := cmd.Args(cmd, args); err != nil {
				t.Errorf("Args validator rejected the correct count (%d) for plivo %s: %v", tc.validN, strings.Join(tc.path, " "), err)
			}
			if tc.validN > 0 {
				if err := cmd.Args(cmd, args[:tc.validN-1]); err == nil {
					t.Errorf("Args validator accepted %d args for plivo %s (wants %d)", tc.validN-1, strings.Join(tc.path, " "), tc.validN)
				}
			}
		})
	}
}

// ─── All commands have a Short description (UX hygiene) ─────────────────────

func TestEveryRegisteredCmd_hasShortDescription(t *testing.T) {
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		// Skip auto-added commands.
		if c.Name() == "help" || c.Name() == "completion" {
			return
		}
		if c.Runnable() && c.Short == "" {
			t.Errorf("command %q has no Short description (shows blank in `plivo --help`)", c.CommandPath())
		}
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(rootCmd)
}

// findCmdNoFail is a non-fatal variant of findCmd for batch checks where we
// want to report all missing commands in one run rather than aborting on the
// first.
func findCmdNoFail(path ...string) *cobra.Command {
	cmd, _, err := rootCmd.Find(path)
	if err != nil || cmd == nil {
		return nil
	}
	if len(path) > 0 && cmd.Name() != path[len(path)-1] {
		return nil
	}
	return cmd
}
