package cmd

import (
	"fmt"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// execute runs one invocation: the argv pre-scan, then cobra.
func execute(args []string) error {
	if done, err := scanArgs(args); done {
		return err
	}
	err := rootCmd.Execute()
	if err != nil && strings.HasPrefix(err.Error(), "unknown command ") {
		// cobra's own error for a word that is no subcommand (a group's
		// NoArgs, or root's legacy check) gets the --help path's code and
		// hint. A leaf's NoArgs says the same and stays as it is.
		if unknown := unknownSubcommand(args); unknown != nil {
			return unknown
		}
	}
	return err
}

// scanArgs looks at argv before cobra does, for what cobra's own order gets
// wrong: it checks a command's positional arguments and required flags
// before any hook runs, so `numbers get --schema` would demand a number it
// never uses, and with --help it prints a group's help for a subcommand
// that does not exist. done reports that the invocation was answered here.
func scanArgs(args []string) (done bool, err error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "__complete") {
		return false, nil // shell completion requests stay cobra's
	}
	schema := hasFlag(args, "--schema")
	if schema || hasFlag(args, "--help") || hasFlag(args, "-h") {
		if err := unknownSubcommand(args); err != nil {
			return true, err
		}
	}
	if schema {
		return describeCommand(args)
	}
	return false, nil
}

// hasFlag reports whether the flag (or flag=value) appears before "--". It
// may also be another flag's value; the real parse decides.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// unknownSubcommand returns BAD_INPUT when args reach a command group and
// then a word that is none of its subcommands, as in `sip trunks diagnose
// --help`, which cobra answers with the group's help and exit 0. The run
// ends there, so the group's flags stay parsed and -o shapes the error. nil,
// with the flags as they were, when args reach a real command or the flags
// do not parse (cobra reports those itself).
func unknownSubcommand(args []string) error {
	// cobra adds these lazily, on Execute; without them `help sip -h` and
	// `completion --help` would look like unknown subcommands of root.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	cmd, rest, _ := rootCmd.Find(args)
	if cmd == nil || !cmd.HasSubCommands() {
		return nil
	}
	cmd.InitDefaultHelpFlag()
	if err := cmd.ParseFlags(rest); err != nil || cmd.Flags().NArg() == 0 {
		resetParsedFlags(cmd)
		return nil
	}
	return unknownCommandError(cmd, cmd.Flags().Arg(0))
}

// unknownCommandError names the word and the group it is not a command of,
// with cobra's own suggestions in the hint.
func unknownCommandError(group *cobra.Command, word string) *clierr.Error {
	if group.SuggestionsMinimumDistance <= 0 {
		group.SuggestionsMinimumDistance = 2 // cobra's default, which it sets lazily
	}
	var suggestions []string
	if !group.DisableSuggestions {
		for _, name := range group.SuggestionsFor(word) {
			suggestions = append(suggestions, "`"+group.CommandPath()+" "+name+"`")
		}
	}
	err := clierr.BadInput(fmt.Sprintf("unknown command %q for %q", word, group.CommandPath()))
	err.Hint = "Run `" + group.CommandPath() + " --help` to see its commands."
	if len(suggestions) > 0 {
		err.Hint = "Did you mean " + strings.Join(suggestions, " or ") + "? " + err.Hint
	}
	err.Context = map[string]any{"command": group.CommandPath(), "unknown": word}
	return err
}

// describeCommand answers --schema: it parses the command's flags but never
// validates its arguments or runs it, so nothing is sent.
func describeCommand(args []string) (bool, error) {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	cmd, rest, err := rootCmd.Find(args)
	if err != nil {
		return false, nil // an unknown command: cobra reports it
	}
	if err := cmd.ParseFlags(rest); err != nil || !schemaFlag {
		// A bad flag is cobra's to report, and "--schema" may have been
		// another flag's value. Either way cobra parses again from scratch.
		resetParsedFlags(cmd)
		return false, nil
	}
	if err := checkOutputFormat(); err != nil {
		return true, err
	}
	if err := applyOutputFlags(); err != nil {
		return true, err
	}
	return true, printSchema(cmd)
}

// resetParsedFlags undoes a ParseFlags, so a second parse does not append to
// repeatable flags.
func resetParsedFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Changed {
			return
		}
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})
}
