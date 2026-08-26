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
	// coming-soon stub; commands not registered in this build are verified
	// separately in internal_registration_test.go.
	groups := []string{
		"account", "agent", "api", "ask", "auth", "feedback", "login", "logout", "lookup", "messaging", "numbers", "support", "upgrade", "verify", "voice",
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
		{"auth", []string{"list", "use", "remove", "whoami"}}, // `token` is not registered in this build; login/logout are top-level
		{"account subaccounts", []string{"list", "get", "create", "update", "delete"}},
		{"account applications", []string{"create", "list", "get", "update", "delete"}},
		{"numbers", []string{"list", "get", "search", "buy", "update", "release", "cnam", "masking", "compliance"}},
		{"numbers masking", []string{"sessions"}},
		{"numbers compliance", []string{"requirements", "create", "get", "list", "update", "delete", "link"}},
		{"voice endpoints", []string{"list", "get", "create", "update", "delete"}},
		{"voice calls", []string{
			"make", "list", "get",
			"hangup", "transfer",
			"play", "stop-play",
			"speak", "stop-speak",
			"dtmf",
			"record", "stop-record",
			"streams",
			"diagnose",
		}},
		{"messaging sms", []string{"send", "list", "diagnose", "10dlc", "powerpacks", "tollfree"}},
		{"messaging whatsapp", []string{"send", "list", "diagnose"}},
		{"messaging mms", []string{"send", "list", "diagnose"}},
		{"voice conferences", []string{"list", "get", "hangup", "record", "stop-record", "member"}},
		{"voice multiparty", []string{"list", "get", "create", "end", "participant"}},
		{"voice calls streams", []string{"list", "get", "start", "stop"}},
		{"voice recordings", []string{"list", "get", "delete"}},
		{"verify", []string{"sessions"}},
		{"messaging", []string{"sms", "whatsapp", "mms", "get"}},
		{"messaging sms 10dlc brands", []string{"list", "get", "create", "update"}},
		{"messaging sms 10dlc campaigns", []string{"list", "get", "create", "update"}},
		{"messaging sms 10dlc links", []string{"list", "create", "delete"}},
		{"messaging sms tollfree", []string{"list", "get", "submit"}},
		{"messaging sms powerpacks", []string{"list", "get", "create", "update", "delete", "numbers"}},
		// `agent` subcommands are not registered in this build; the public
		// build ships a flat coming-soon stub with no subcommands.
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
		"verify sessions":                  {"create", "get", "list", "validate"},
		"numbers masking sessions":         {"create", "get", "list", "delete"},
		"voice conferences member":         {"kick", "mute", "unmute", "deaf", "undeaf", "play", "stop-play", "speak", "stop-speak"},
		"voice multiparty participant":     {"list", "add", "kick", "mute", "unmute", "hold", "unhold"},
		"messaging sms powerpacks numbers": {"list", "add", "remove"},
		// `auth token` + `agent session` are not registered in this build (see internal_registration_test.go).
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

// ─── In-context cobra aliases resolve under their service parent ─────────────

// Cobra-level aliases on each canonical command (e.g. `call` on `voice calls`,
// `sms`/`msg`/`message` on `messaging`) must keep resolving. These are
// separate from the removed top-level argv-rewrite shim — they're tied to the
// actual command, not a pre-cobra string rewrite. The `messaging`/`message`/
// `msg`/`sms` set is the trickiest: `messaging` is canonical post-rename,
// with `message` retained as a back-compat alias for users who already typed
// `plivo message ...` from muscle memory.
func TestInContextAliases_resolve(t *testing.T) {
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"voice", "calls"}, "calls"},
		{[]string{"voice", "call"}, "calls"},
		{[]string{"voice", "conf"}, "conferences"},
		{[]string{"voice", "rec"}, "recordings"},
		{[]string{"voice", "ep"}, "endpoints"},
		{[]string{"voice", "mpc"}, "multiparty"},
		{[]string{"voice", "calls", "stream"}, "streams"},
		{[]string{"messaging"}, "messaging"},
		{[]string{"message"}, "messaging"},
		{[]string{"msg"}, "messaging"},
		{[]string{"sms"}, "messaging"},
		{[]string{"messaging", "sms", "pp"}, "powerpacks"},
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

// ─── Required flags ──────────────────────────────────────────────────────────

func TestRequiredFlags(t *testing.T) {
	cases := []struct {
		path  []string
		flags []string
	}{
		{[]string{"messaging", "sms", "send"}, []string{"src", "dst", "text"}},
		{[]string{"messaging", "whatsapp", "send"}, []string{"src", "dst", "text"}},
		{[]string{"messaging", "mms", "send"}, []string{"src", "dst", "text"}},
		{[]string{"voice", "calls", "make"}, []string{"from", "to"}},
		{[]string{"voice", "calls", "play"}, []string{"urls"}},
		{[]string{"voice", "calls", "speak"}, []string{"text"}},
		{[]string{"voice", "calls", "dtmf"}, []string{"digits"}},
		{[]string{"account", "applications", "create"}, []string{"app-name", "answer-url"}},
		{[]string{"voice", "endpoints", "create"}, []string{"username", "password"}},
		{[]string{"account", "subaccounts", "create"}, []string{"name"}},
		{[]string{"numbers", "search"}, []string{"country"}},
		{[]string{"numbers", "compliance", "requirements"}, []string{"country", "number-type", "user-type"}},
		{[]string{"numbers", "compliance", "create"}, []string{"data"}},
		{[]string{"numbers", "compliance", "update"}, []string{"data"}},
		{[]string{"verify", "sessions", "create"}, []string{"recipient", "app-uuid"}},
		{[]string{"verify", "sessions", "validate"}, []string{"otp"}},
		{[]string{"numbers", "masking", "sessions", "create"}, []string{"first-party", "second-party"}},
		{[]string{"messaging", "sms", "10dlc", "brands", "create"}, []string{"alias", "legal-name"}},
		{[]string{"messaging", "sms", "10dlc", "campaigns", "create"}, []string{"alias", "brand-id", "usecase", "description", "message-flow", "sample-message-1"}},
		{[]string{"messaging", "sms", "10dlc", "links", "create"}, []string{"number", "campaign-id"}},
		{[]string{"messaging", "sms", "tollfree", "submit"}, []string{"business-name", "use-case"}},
		{[]string{"voice", "multiparty", "create"}, []string{"name"}},
		{[]string{"voice", "multiparty", "participant", "add"}, []string{"from", "to"}},
		{[]string{"voice", "conferences", "member", "play"}, []string{"urls"}},
		{[]string{"voice", "conferences", "member", "speak"}, []string{"text"}},
		{[]string{"voice", "calls", "streams", "start"}, []string{"url"}},
		{[]string{"messaging", "sms", "powerpacks", "create"}, []string{"name"}},
		// `auth token mint --modules` is not registered in this build.
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
		"yes", "dry-run", "explain", "timeout",
		// Additional admin-only persistent flags are verified in internal_registration_test.go.
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
		validN int // a positional-arg count the validator must accept
		// rejectBelow: validN-1 args must be rejected (exact-arg commands).
		// When false, fewer args are allowed too (e.g. `ask` takes an optional
		// message, so 0 args is valid; the message-required check is a runtime
		// concern, not an Args validator one).
		rejectBelow bool
		// rejectAbove: validN+1 args must be rejected (the validator has an
		// upper bound even when fewer args are allowed).
		rejectAbove bool
	}{
		{[]string{"numbers", "get"}, "numbers get <number>", 1, true, false},
		{[]string{"numbers", "buy"}, "numbers buy <number>", 1, true, false},
		{[]string{"numbers", "release"}, "numbers release <number>", 1, true, false},
		{[]string{"ask"}, "ask [msg]", 1, false, true},
		{[]string{"numbers", "compliance", "get"}, "compliance get <id>", 1, true, false},
		{[]string{"numbers", "compliance", "update"}, "compliance update <id>", 1, true, false},
		{[]string{"numbers", "compliance", "delete"}, "compliance delete <id>", 1, true, false},
		{[]string{"account", "applications", "delete"}, "applications delete <app_id>", 1, true, false},
		{[]string{"voice", "calls", "hangup"}, "calls hangup <uuid>", 1, true, false},
		{[]string{"voice", "calls", "dtmf"}, "calls dtmf <uuid>", 1, true, false},
		{[]string{"voice", "calls", "streams", "get"}, "streams get <call_uuid> <stream_id>", 2, true, false},
		{[]string{"voice", "conferences", "member", "kick"}, "kick <conf> <member>", 2, true, false},
		{[]string{"voice", "multiparty", "participant", "kick"}, "participant kick <mpc> <part>", 2, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			cmd := findCmd(t, tc.path...)
			if cmd.Args == nil {
				t.Errorf("plivo %s — Args validator is nil; should be ExactArgs(%d) or similar", strings.Join(tc.path, " "), tc.validN)
				return
			}
			mkArgs := func(n int) []string {
				args := make([]string, n)
				for i := range args {
					args[i] = "x"
				}
				return args
			}
			if err := cmd.Args(cmd, mkArgs(tc.validN)); err != nil {
				t.Errorf("Args validator rejected the correct count (%d) for plivo %s: %v", tc.validN, strings.Join(tc.path, " "), err)
			}
			if tc.rejectBelow && tc.validN > 0 {
				if err := cmd.Args(cmd, mkArgs(tc.validN-1)); err == nil {
					t.Errorf("Args validator accepted %d args for plivo %s (wants %d)", tc.validN-1, strings.Join(tc.path, " "), tc.validN)
				}
			}
			if tc.rejectAbove {
				if err := cmd.Args(cmd, mkArgs(tc.validN+1)); err == nil {
					t.Errorf("Args validator accepted %d args for plivo %s (max %d)", tc.validN+1, strings.Join(tc.path, " "), tc.validN)
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
