package cmd

import "github.com/spf13/cobra"

// Service-level command groups for the `plivo <service> <resource> <verb>`
// grammar. Resources register themselves under these parents from their own
// files (e.g. call.go does voiceCmd.AddCommand(callsCmd)). The `message`
// service (see message.go), `numbers`, `account`, `verify`, `auth`, `lookup`,
// and `agent` are services in their own right and register on rootCmd from
// their respective files.

var voiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Voice — calls, conferences, multiparty, recordings, endpoints",
}

// sms10dlcCmd groups the US A2P 10DLC resources (brands/campaigns/links). It
// hangs off the `message` service, which registers it in message.go.
var sms10dlcCmd = &cobra.Command{
	Use:   "10dlc",
	Short: "US A2P 10DLC registration — brands, campaigns, links",
}

func init() {
	rootCmd.AddCommand(voiceCmd)
}
