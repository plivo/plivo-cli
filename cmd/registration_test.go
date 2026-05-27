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

// ─── Top-level groups registered ─────────────────────────────────────────────

func TestRootCmd_allTopLevelGroupsRegistered(t *testing.T) {
	// The public-v1 top-level groups under `plivo`. `agent` is present as a
	// coming-soon stub; `contacto` + `auth token` are internal-only (build
	// tag `internal`) and verified separately in internal_registration_test.go.
	groups := []string{
		"account", "agent", "application", "auth", "brand", "call",
		"campaign", "cnam", "compliance", "conference",
		"endpoint", "link", "lookup", "masking", "message", "mpc",
		"number", "powerpack", "recording", "stream", "subaccount",
		"tollfree", "verify",
	}
	for _, g := range groups {
		t.Run(g, func(t *testing.T) {
			findCmd(t, g)
		})
	}
}

// ─── Subcommands registered under each group ────────────────────────────────

func TestSubcommands_registered(t *testing.T) {
	cases := []struct {
		group string
		verbs []string
	}{
		{"account", []string{"get", "update"}},
		{"auth", []string{"login", "list", "use", "remove", "whoami"}}, // `token` is internal-only
		{"subaccount", []string{"list", "get", "create", "update", "delete"}},
		{"number", []string{"list", "get", "search", "buy", "update", "release"}},
		{"application", []string{"create", "list", "get", "update", "delete"}},
		{"endpoint", []string{"list", "get", "create", "update", "delete"}},
		{"message", []string{"send", "list", "get"}},
		{"call", []string{
			"make", "list", "get",
			"hangup", "transfer",
			"play", "stop-play",
			"speak", "stop-speak",
			"dtmf",
			"record", "stop-record",
		}},
		{"conference", []string{"list", "get", "hangup", "record", "stop-record", "member"}},
		{"mpc", []string{"list", "get", "create", "end", "participant"}},
		{"stream", []string{"list", "get", "start", "stop"}},
		{"recording", []string{"list", "get", "delete"}},
		{"verify", []string{"session"}},
		{"masking", []string{"session"}},
		{"compliance", []string{"list", "get", "delete"}},
		{"brand", []string{"list", "get", "create", "update"}},
		{"campaign", []string{"list", "get", "create", "update"}},
		{"link", []string{"list", "create", "delete"}},
		{"tollfree", []string{"list", "get", "submit"}},
		{"powerpack", []string{"list", "get", "create", "update", "delete", "number"}},
		// `agent` subcommands are internal-only; the public build ships a
		// flat coming-soon stub with no subcommands.
	}
	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			for _, v := range tc.verbs {
				if cmd := findCmdNoFail(tc.group, v); cmd == nil {
					t.Errorf("plivo %s %s — not registered", tc.group, v)
				}
			}
		})
	}
}

// ─── Nested subcommands (group → sub → verb) ────────────────────────────────

func TestNestedSubcommands_registered(t *testing.T) {
	nests := map[string][]string{
		"verify session":    {"create", "get", "list", "validate"},
		"masking session":   {"create", "get", "list", "delete"},
		"conference member": {"kick", "mute", "unmute", "deaf", "undeaf", "play", "stop-play", "speak", "stop-speak"},
		"mpc participant":   {"list", "add", "kick", "mute", "unmute", "hold", "unhold"},
		"powerpack number":  {"list", "add", "remove"},
		// `auth token` + `agent session` are internal-only (see internal_registration_test.go).
	}
	for path, verbs := range nests {
		path := path
		verbs := verbs
		t.Run(path, func(t *testing.T) {
			parts := strings.Fields(path)
			for _, v := range verbs {
				full := append(parts, v)
				if cmd := findCmdNoFail(full...); cmd == nil {
					t.Errorf("plivo %s — not registered", strings.Join(full, " "))
				}
			}
		})
	}
}

// ─── Aliases ─────────────────────────────────────────────────────────────────

func TestAliases_resolve(t *testing.T) {
	cases := []struct {
		alias    string
		expected string
	}{
		{"msg", "message"},
		{"app", "application"},
		{"conf", "conference"},
		{"pp", "powerpack"},
		{"sub", "subaccount"},
		{"ep", "endpoint"},
		{"mask", "masking"},
		{"tfv", "tollfree"},
		{"rec", "recording"},
		{"camp", "campaign"},
	}
	for _, tc := range cases {
		t.Run(tc.alias, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{tc.alias})
			if err != nil || cmd == nil {
				t.Fatalf("alias %q didn't resolve: %v", tc.alias, err)
			}
			if cmd.Name() != tc.expected {
				t.Errorf("alias %q resolved to %q, want %q", tc.alias, cmd.Name(), tc.expected)
			}
		})
	}
}

func TestAliases_mpcPartIsParticipant(t *testing.T) {
	// 'mpc part' alias for 'mpc participant'
	cmd, _, err := rootCmd.Find([]string{"mpc", "part"})
	if err != nil || cmd == nil {
		t.Fatalf("'mpc part' alias didn't resolve: %v", err)
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
		{[]string{"message", "send"}, []string{"src", "dst", "text"}},
		{[]string{"call", "make"}, []string{"from", "to"}},
		{[]string{"call", "play"}, []string{"urls"}},
		{[]string{"call", "speak"}, []string{"text"}},
		{[]string{"call", "dtmf"}, []string{"digits"}},
		{[]string{"application", "create"}, []string{"app-name", "answer-url"}},
		{[]string{"endpoint", "create"}, []string{"username", "password"}},
		{[]string{"subaccount", "create"}, []string{"name"}},
		{[]string{"number", "search"}, []string{"country"}},
		{[]string{"verify", "session", "create"}, []string{"recipient", "app-uuid"}},
		{[]string{"verify", "session", "validate"}, []string{"otp"}},
		{[]string{"masking", "session", "create"}, []string{"first-party", "second-party"}},
		{[]string{"brand", "create"}, []string{"alias", "legal-name"}},
		{[]string{"campaign", "create"}, []string{"alias", "brand-id", "usecase", "description", "message-flow", "sample-message-1"}},
		{[]string{"link", "create"}, []string{"number", "campaign-id"}},
		{[]string{"tollfree", "submit"}, []string{"business-name", "use-case"}},
		{[]string{"mpc", "create"}, []string{"name"}},
		{[]string{"mpc", "participant", "add"}, []string{"from", "to"}},
		{[]string{"conference", "member", "play"}, []string{"urls"}},
		{[]string{"conference", "member", "speak"}, []string{"text"}},
		{[]string{"stream", "start"}, []string{"url"}},
		{[]string{"powerpack", "create"}, []string{"name"}},
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
	// Note: no-arg commands like `account get` rely on cobra's default
	// behaviour, which silently accepts trailing garbage. Tightening those
	// to cobra.NoArgs is a small follow-up UX fix tracked separately —
	// not in P1 scope.
	cases := []struct {
		path   []string
		desc   string
		validN int // expected number of positional args
	}{
		{[]string{"number", "get"}, "number get <number>", 1},
		{[]string{"number", "buy"}, "number buy <number>", 1},
		{[]string{"number", "release"}, "number release <number>", 1},
		{[]string{"application", "delete"}, "application delete <app_id>", 1},
		{[]string{"call", "hangup"}, "call hangup <uuid>", 1},
		{[]string{"call", "dtmf"}, "call dtmf <uuid>", 1},
		{[]string{"stream", "get"}, "stream get <call_uuid> <stream_id>", 2},
		{[]string{"conference", "member", "kick"}, "kick <conf> <member>", 2},
		{[]string{"mpc", "participant", "kick"}, "participant kick <mpc> <part>", 2},
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
