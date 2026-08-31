package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/config"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// configCmd hosts CLI-local settings persisted in ~/.plivo/config.toml —
// distinct from credential profiles, which live under cmd/auth.go.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI settings (telemetry on/off, get/set)",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var configTelemetryCmd = &cobra.Command{
	Use:   "telemetry <on|off|status>",
	Short: "Turn identity telemetry on/off, or show its current state",
	Long: `Turn identity telemetry on/off, or show its current state.

When on (the default), CLI requests carry identity headers (email, auth
ID, region, AOM UUID) alongside the CLI-version/OS/arch metadata that
always goes out. "off" drops just the identity headers — every command
still works the same.`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigTelemetry,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Print a CLI setting",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Change a CLI setting",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

func init() {
	configCmd.AddCommand(configTelemetryCmd, configGetCmd, configSetCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigTelemetry(_ *cobra.Command, args []string) error {
	switch args[0] {
	case "status":
		return printTelemetryStatus()
	case "on":
		return setTelemetryEnabled(true)
	case "off":
		return setTelemetryEnabled(false)
	default:
		return clierr.BadInput(fmt.Sprintf(`telemetry expects "on", "off", or "status", got %q`, args[0]))
	}
}

// setTelemetryEnabled persists the on/off choice and prints a short
// confirmation, including a heads-up when PLIVO_CLI_TELEMETRY=0 in this
// shell would otherwise mask a `telemetry on`.
func setTelemetryEnabled(on bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Telemetry.Enabled = &on
	if err := config.Save(cfg); err != nil {
		return err
	}
	state := "off"
	if on {
		state = "on"
	}
	fmt.Fprintf(os.Stderr, "Telemetry: %s\n", state)
	if on && os.Getenv(config.TelemetryEnvVar) == "0" {
		fmt.Fprintf(os.Stderr, "  note: %s=0 is set in this shell and still disables it\n", config.TelemetryEnvVar)
	}
	return nil
}

func printTelemetryStatus() error {
	state := "off"
	if config.TelemetryEnabled() {
		state = "on"
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, map[string]string{"telemetry": state}, nil)
	}
	fmt.Fprintf(os.Stdout, "telemetry: %s\n", state)
	if os.Getenv(config.TelemetryEnvVar) == "0" {
		fmt.Fprintf(os.Stdout, "  (forced off by %s=0)\n", config.TelemetryEnvVar)
	}
	return nil
}

// configKeyEntry backs the small get/set registry below. get reads the
// current value off a loaded Config; set validates + applies a new one.
type configKeyEntry struct {
	get func(*config.Config) string
	set func(*config.Config, string) error
}

// configKeys enumerates the settings `plivo config get/set` understands.
// Just telemetry today; add entries here as more settings need a knob.
var configKeys = map[string]configKeyEntry{
	"telemetry": {
		get: func(c *config.Config) string {
			if c.Telemetry.Enabled == nil || *c.Telemetry.Enabled {
				return "on"
			}
			return "off"
		},
		set: func(c *config.Config, value string) error {
			on, err := parseOnOff(value)
			if err != nil {
				return err
			}
			c.Telemetry.Enabled = &on
			return nil
		},
	},
}

func parseOnOff(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, clierr.BadInput(fmt.Sprintf("invalid value %q: expected on/off", v))
	}
}

func supportedConfigKeys() string {
	keys := make([]string, 0, len(configKeys))
	for k := range configKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func runConfigGet(_ *cobra.Command, args []string) error {
	key := args[0]
	entry, ok := configKeys[key]
	if !ok {
		return clierr.BadInput(fmt.Sprintf("unknown config key %q (supported: %s)", key, supportedConfigKeys()))
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	value := entry.get(cfg)
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, map[string]string{key: value}, nil)
	}
	fmt.Fprintln(os.Stdout, value)
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	entry, ok := configKeys[key]
	if !ok {
		return clierr.BadInput(fmt.Sprintf("unknown config key %q (supported: %s)", key, supportedConfigKeys()))
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := entry.set(cfg, value); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s = %s\n", key, entry.get(cfg))
	return nil
}
