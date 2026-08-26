package cmd

import (
	"net/url"
	"os"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var lookupType string

var lookupCmd = &cobra.Command{
	Use:   "lookup <number>",
	Short: "Carrier + format lookup for an E.164 number (lookup.plivo.com)",
	Args:  cobra.ExactArgs(1),
	RunE:  runLookup,
}

func init() {
	lookupCmd.Flags().StringVar(&lookupType, "type", "carrier", "lookup type (currently only 'carrier' is supported by Plivo)")
	rootCmd.AddCommand(lookupCmd)
}

func runLookup(cmd *cobra.Command, args []string) error {
	number := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("type", lookupType)
	var n api.LookupNumber
	apiErr, err := client.Do("GET", client.LookupURL(number), nil, q, &n)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, n.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"phone_number", n.PhoneNumber},
		{"country", n.Country.Name + " (" + n.Country.ISO2 + ")"},
		{"format_e164", n.Format.E164},
		{"format_international", n.Format.International},
		{"format_national", n.Format.National},
		{"carrier_type", n.Carrier.Type},
		{"carrier_name", n.Carrier.Name},
		{"mcc", n.Carrier.MobileCountryCode},
		{"mnc", n.Carrier.MobileNetworkCode},
		{"ported", n.Carrier.Ported},
	})
}
