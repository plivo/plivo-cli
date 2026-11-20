package cmd

import (
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var pricingCountry string

var accountPricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Show Plivo's rates for a country: SMS, MMS, voice, numbers, WhatsApp",
	Long: `Show what Plivo charges in one country: SMS and MMS, voice by kind of number
and by prefix, number rental and setup, and WhatsApp conversations. These are
the rates for this account, as the API sends them; it names neither a currency
nor a unit, so check the console for those.

The table has one row for each rate. -o json has the whole answer, including
the SMS rate of every network.`,
	Example: `  plivo account pricing --country IN
  plivo account pricing --country US -o json`,
	Args: cobra.NoArgs,
	RunE: runAccountPricing,
}

func init() {
	accountPricingCmd.Flags().StringVar(&pricingCountry, "country", "", "two-letter country code, e.g. US (required)")
	_ = accountPricingCmd.MarkFlagRequired("country")
	accountCmd.AddCommand(accountPricingCmd)
}

func runAccountPricing(cmd *cobra.Command, args []string) error {
	iso := strings.ToUpper(strings.TrimSpace(pricingCountry))
	if !countryCode.MatchString(iso) {
		e := clierr.BadFlag("country", fmt.Sprintf("must be a two-letter country code, got %q", pricingCountry))
		e.Hint = "Pass an ISO 3166-1 alpha-2 country code, such as US, GB or IN."
		return e
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.Pricing
	apiErr, err := client.Do("GET", client.AccountURL("Pricing"), nil, url.Values{"country_iso": {iso}}, &resp)
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
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	fmt.Fprintf(os.Stdout, "%s (%s)\n\n", resp.Country, resp.CountryISO)
	if err := output.Table(os.Stdout, pricingRows(&resp)); err != nil {
		return err
	}
	if n := len(resp.Message.Networks); n > 0 && !quietFlag {
		fmt.Fprintf(os.Stderr, "SMS is priced per network too (%d SMS networks): see -o json.\n", n)
	}
	return nil
}

// pricingRows is one row for each rate the API sends, in the order a reader
// looks: messages, voice, numbers, WhatsApp. A rate it sends as null reads "-".
func pricingRows(p *api.Pricing) [][]string {
	rows := [][]string{{"SECTION", "ITEM", "RATE", "NOTE"}}
	add := func(section, item string, rate api.Price, note string) {
		rows = append(rows, []string{section, item, dash(string(rate)), note})
	}
	for _, m := range []struct {
		name string
		msg  api.PricingMessage
	}{{"sms", p.Message}, {"mms", p.MMS}} {
		add(m.name, "outbound", m.msg.Outbound.Rate, "")
		add(m.name, "inbound", m.msg.Inbound.Rate, "")
	}
	for _, side := range []struct {
		name string
		v    api.PricingVoiceSide
	}{{"inbound", p.Voice.Inbound}, {"outbound", p.Voice.Outbound}} {
		add("voice", side.name+" local", side.v.Local.Rate, "")
		add("voice", side.name+" tollfree", side.v.Tollfree.Rate, "")
		add("voice", side.name+" ip", side.v.IP.Rate, "")
	}
	for _, r := range p.Voice.Outbound.Rates {
		note := "prefix " + strings.Join(r.Prefix, ",")
		if from := strings.Join(r.OriginationPrefix, ","); from != "" {
			note += ", calls from " + from
		}
		if r.Unit > 0 {
			note += fmt.Sprintf(", voice_unit %d", r.Unit)
		}
		add("voice", "outbound "+r.NetworkGroup, r.Rate, note)
	}
	for _, kind := range slices.Sorted(maps.Keys(p.PhoneNumbers)) {
		n := p.PhoneNumbers[kind]
		note := ""
		for _, r := range n.Rates {
			note += fmt.Sprintf("%s: rental %s, setup %s; ", strings.Join(r.Capabilities, "+"), dash(string(r.RentalRate)), dash(string(r.SetupRate)))
		}
		add("numbers", kind, n.Rate, strings.TrimSuffix(note, "; "))
	}
	for _, name := range slices.Sorted(maps.Keys(p.WhatsApp)) {
		add("whatsapp", name, p.WhatsApp[name].Rate, "")
	}
	return rows
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
