package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// execute runs one invocation: the argv pre-scan, then cobra.
func execute(args []string) error {
	if done, err := scanArgs(args); done {
		return err
	}
	return rootCmd.Execute()
}

// scanArgs looks at argv before cobra does, for what cobra's own order gets
// wrong: it checks a command's positional arguments and required flags
// before any hook runs, so `numbers get --schema` would demand a number it
// never uses. done reports that the invocation was answered here.
func scanArgs(args []string) (done bool, err error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "__complete") {
		return false, nil // shell completion requests stay cobra's
	}
	if hasFlag(args, "schema") {
		return describeCommand(args)
	}
	return false, nil
}

// hasFlag reports whether --name (or --name=value) appears before "--". It
// may also be another flag's value; the real parse decides.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--"+name || strings.HasPrefix(a, "--"+name+"=") {
			return true
		}
	}
	return false
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
