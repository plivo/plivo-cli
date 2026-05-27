package cmd

import "github.com/spf13/cobra"

// Service-level command groups for the `plivo <service> <resource> <verb>`
// grammar. Resources register themselves under these parents from their own
// files (e.g. call.go does voiceCmd.AddCommand(callsCmd)). `numbers`, `account`,
// `verify`, `auth`, `lookup`, and `agent` are services in their own right and
// stay registered directly on rootCmd from their respective files.

var voiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Voice — calls, conferences, multiparty, recordings, endpoints",
}

var smsCmd = &cobra.Command{
	Use:   "sms",
	Short: "Messaging — SMS/MMS, 10DLC, powerpacks, toll-free",
}

var sms10dlcCmd = &cobra.Command{
	Use:   "10dlc",
	Short: "US A2P 10DLC registration — brands, campaigns, links",
}

func init() {
	smsCmd.AddCommand(sms10dlcCmd)
	rootCmd.AddCommand(voiceCmd, smsCmd)
}
