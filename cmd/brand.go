package cmd

import (
	"net/url"
	"os"
	"strconv"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var brandCmd = &cobra.Command{
	Use:     "brands",
	Aliases: []string{"brand"},
	Short:   "10DLC brand registration (US A2P 10-digit-long-code messaging)",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	brandListLimit  int
	brandListOffset int
)

var brandListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered brands",
	Args:  cobra.NoArgs,
	RunE:  runBrandList,
}

var brandGetCmd = &cobra.Command{
	Use:   "get <brand_id>",
	Short: "Get a brand by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runBrandGet,
}

var (
	brandCreateProfile string
	brandCreateAlias   string
	brandCreateType    string
	brandCreateVetting bool
)

var brandTypes = []string{"STARTER", "STANDARD"}

var brandCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new brand (spends money — TCR registration fee, requires --yes)",
	Long: `Register a 10DLC brand from a profile, which holds the company's details:
legal name, EIN, address, website and authorized contact. A profile registers
one brand. List your profiles with plivo api GET /Profile/, and create one in
the Plivo console or with plivo api POST /Profile/ (fields: plivo docs show
messaging/api/10dlc/profile).

STARTER is the default. STANDARD needs a profile with an EIN, and also sends
the brand for secondary vetting, a separate one-time fee, unless
--secondary-vetting=false.`,
	Args: cobra.NoArgs,
	RunE: runBrandCreate,
}

// Neither the Go SDK nor the API reference has a brand update. The command
// stays, hidden, so a script that calls it learns why and where to go.
var brandUpdateCmd = &cobra.Command{
	Use:    "update <brand_id>",
	Short:  "Not available: the API has no brand update",
	Hidden: true,
	Args:   cobra.ArbitraryArgs,
	// The old --email, --phone and --website reach the message too.
	FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
	RunE: func(*cobra.Command, []string) error {
		e := clierr.BadInput("brands update: the 10DLC API has no endpoint that updates a brand")
		e.Hint = "A brand takes its company details from its profile, which can be updated: " +
			"plivo api POST /Profile/<profile_uuid>/ --body @changes.json --yes " +
			"(fields: plivo docs show messaging/api/10dlc/profile)."
		return e
	},
}

func init() {
	registerListFlags(brandListCmd, &brandListLimit, &brandListOffset)

	brandCreateCmd.Flags().StringVar(&brandCreateProfile, "profile-uuid", "", "the 10DLC profile holding the company's details (required)")
	_ = brandCreateCmd.MarkFlagRequired("profile-uuid")
	brandCreateCmd.Flags().StringVar(&brandCreateAlias, "alias", "", "human-friendly alias (required)")
	_ = brandCreateCmd.MarkFlagRequired("alias")
	brandCreateCmd.Flags().StringVar(&brandCreateType, "brand-type", "STARTER", oneOf(brandTypes))
	brandCreateCmd.Flags().BoolVar(&brandCreateVetting, "secondary-vetting", true, "STANDARD only: send the brand for secondary vetting, a separate fee")

	brandCmd.AddCommand(brandListCmd, brandGetCmd, brandCreateCmd, brandUpdateCmd)
	sms10dlcCmd.AddCommand(brandCmd)
}

func runBrandList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(brandListLimit))
	q.Set("offset", strconv.Itoa(brandListOffset))
	var resp api.Brand10DLCList
	if err := fetchList(client, client.AccountURL("10dlc", "Brand"), q, "brands", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "brands")
	}
	rows := [][]string{{"BRAND_ID", "ALIAS", "LEGAL_NAME", "TYPE", "STATUS", "VERTICAL"}}
	for _, b := range resp.Brands {
		rows = append(rows, []string{b.BrandID, b.BrandAlias, b.CompanyName, b.BrandType, b.RegistrationStatus, b.Vertical})
	}
	return output.Table(os.Stdout, rows)
}

func runBrandGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.Brand10DLCResponse
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "Brand", id), nil, nil, &resp)
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
	b := resp.Brand
	return output.KV(os.Stdout, [][2]string{
		{"brand_id", b.BrandID},
		{"alias", b.BrandAlias},
		{"profile_uuid", b.ProfileUUID},
		{"legal_name", b.CompanyName},
		{"brand_type", b.BrandType},
		{"entity_type", b.EntityType},
		{"status", b.RegistrationStatus},
		{"vetting_status", b.VettingStatus},
		{"vertical", b.Vertical},
		{"website", b.Website},
		{"email", b.AuthorizedContact.Email},
		{"phone", b.AuthorizedContact.Phone},
		{"created_at", b.CreatedAt},
	})
}

func runBrandCreate(cmd *cobra.Command, args []string) error {
	if err := validateEnum("brand-type", &brandCreateType, brandTypes...); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"brand_alias":  brandCreateAlias,
		"profile_uuid": brandCreateProfile,
		"brand_type":   brandCreateType,
	}
	if cmd.Flags().Changed("secondary-vetting") {
		body["secondary_vetting"] = brandCreateVetting
	}

	proceed, dryRun, gerr := guardSpend("register 10DLC brand " + brandCreateAlias)
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	var resp struct {
		api.RawBody
		APIID   string `json:"api_id"`
		BrandID string `json:"brand_id"`
		Message string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "Brand"), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, resp.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"brand_id", resp.BrandID},
		{"message", resp.Message},
	})
}
