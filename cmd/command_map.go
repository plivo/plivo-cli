package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// `plivo --map` and `--schema` describe the CLI from its own command tree, so
// an agent learns it in one call instead of running --help dozens of times.

var (
	mapFlag    bool
	schemaFlag bool
)

// runRoot prints the command map for --map. Without it, help, which is what
// a bare `plivo` printed before root could run.
func runRoot(cmd *cobra.Command, _ []string) error {
	if !mapFlag {
		return cmd.Help()
	}
	return printMap()
}

type argSchema struct {
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	Repeatable bool   `json:"repeatable"`
}

type flagSchema struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	Type      string `json:"type"`
	Default   string `json:"default"`
	Required  bool   `json:"required"`
	Usage     string `json:"usage"`
}

type commandSchema struct {
	Path  string       `json:"path"`
	Short string       `json:"short"`
	Args  []argSchema  `json:"args"`
	Flags []flagSchema `json:"flags"`
}

// commandMap is the --map document. Global flags are listed once; every
// command's flags are the rest of what it accepts.
type commandMap struct {
	GlobalFlags []flagSchema    `json:"global_flags"`
	Commands    []commandSchema `json:"commands"`
}

// commandDetail is the --schema document for one command.
type commandDetail struct {
	commandSchema
	GlobalFlags      []flagSchema  `json:"global_flags"`
	OutputFields     []fieldSchema `json:"output_fields"`
	OutputFieldsNote string        `json:"output_fields_note"`
}

const (
	outputFieldsNote = "Paths under data, so --query 'data.<path>'. The fields this CLI maps, a subset of the raw API response " +
		"that -o json passes through whole: the response can hold more, and a listed field can be missing."
	noOutputFieldsNote = "Not described for this command. Run it with -o json to see its output."
)

func describe(c *cobra.Command) commandSchema {
	return commandSchema{Path: c.CommandPath(), Short: c.Short, Args: commandArgs(c), Flags: commandFlags(c)}
}

// commandArgs reads the positional arguments off the Use line: <x> is
// required, [x] optional, and a trailing ... repeatable.
func commandArgs(c *cobra.Command) []argSchema {
	args := []argSchema{}
	for _, tok := range strings.Fields(c.Use)[1:] {
		name := strings.Trim(tok, "<>[]")
		a := argSchema{Required: strings.HasPrefix(tok, "<")}
		if trimmed, ok := strings.CutSuffix(name, "..."); ok {
			name, a.Repeatable = trimmed, true
		}
		a.Name = name
		args = append(args, a)
	}
	return args
}

// commandFlags is every visible flag c accepts other than the global ones
// and cobra's own --help and --version: its own, and those inherited from a
// parent group.
func commandFlags(c *cobra.Command) []flagSchema {
	flags := []flagSchema{}
	seen := map[string]bool{}
	add := func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" || f.Name == "version" || seen[f.Name] || rootCmd.PersistentFlags().Lookup(f.Name) == f {
			return
		}
		seen[f.Name] = true
		flags = append(flags, describeFlag(f))
	}
	c.LocalFlags().VisitAll(add)
	c.InheritedFlags().VisitAll(add)
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

func globalFlags() []flagSchema {
	flags := []flagSchema{}
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			flags = append(flags, describeFlag(f))
		}
	})
	return flags
}

func describeFlag(f *pflag.Flag) flagSchema {
	return flagSchema{
		Name:      f.Name,
		Shorthand: f.Shorthand,
		Type:      f.Value.Type(),
		Default:   f.DefValue,
		Required:  len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0,
		Usage:     f.Usage,
	}
}

// visibleCommands is every command a user can see, root first, in path
// order. cobra's help and completion are left out, as in the docs.
func visibleCommands() []*cobra.Command {
	cmds := []*cobra.Command{rootCmd}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			cmds = append(cmds, child)
			walk(child)
		}
	}
	walk(rootCmd)
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].CommandPath() < cmds[j].CommandPath() })
	return cmds
}

func printMap() error {
	m := commandMap{GlobalFlags: globalFlags()}
	for _, c := range visibleCommands() {
		m.Commands = append(m.Commands, describe(c))
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, m, nil)
	}
	rows := [][]string{{"COMMAND", "DESCRIPTION"}}
	for _, c := range m.Commands {
		rows = append(rows, []string{strings.TrimSpace(c.Path + " " + argsUsage(c.Args)), c.Short})
	}
	if err := output.Table(os.Stdout, rows); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "\n-o json adds every flag; `plivo <command> --schema` describes one command.")
	return nil
}

func printSchema(c *cobra.Command) error {
	d := commandDetail{commandSchema: describe(c), GlobalFlags: globalFlags(), OutputFieldsNote: noOutputFieldsNote}
	if t, ok := outputTypes[d.Path]; ok {
		d.OutputFields, d.OutputFieldsNote = outputFields(t), outputFieldsNote
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, d, nil)
	}
	_ = output.KV(os.Stdout, [][2]string{
		{"command", strings.TrimSpace(d.Path + " " + argsUsage(d.Args))},
		{"about", d.Short},
	})
	if len(d.Flags) > 0 {
		rows := [][]string{{"FLAG", "TYPE", "DEFAULT", "REQUIRED", "USAGE"}}
		for _, f := range d.Flags {
			rows = append(rows, []string{"--" + f.Name, f.Type, f.Default, yesNo(f.Required), f.Usage})
		}
		fmt.Fprintln(os.Stdout)
		if err := output.Table(os.Stdout, rows); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stdout)
	if d.OutputFields == nil {
		fmt.Fprintln(os.Stdout, "Output fields: "+d.OutputFieldsNote)
		return nil
	}
	fmt.Fprintln(os.Stdout, "Output fields. "+d.OutputFieldsNote)
	rows := [][]string{{"FIELD", "TYPE"}}
	for _, f := range d.OutputFields {
		rows = append(rows, []string{f.Path, f.Type})
	}
	return output.Table(os.Stdout, rows)
}

// argsUsage writes the arguments back as the Use line has them.
func argsUsage(args []argSchema) string {
	parts := make([]string, len(args))
	for i, a := range args {
		name := a.Name
		if a.Repeatable {
			name += "..."
		}
		if a.Required {
			parts[i] = "<" + name + ">"
		} else {
			parts[i] = "[" + name + "]"
		}
	}
	return strings.Join(parts, " ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
