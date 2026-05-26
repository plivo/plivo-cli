package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

var brandCmd = &cobra.Command{
	Use:   "brand",
	Short: "10DLC brand registration (US A2P 10-digit-long-code messaging)",
}

var (
	brandListLimit  int
	brandListOffset int
)

var brandListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered brands",
	RunE:  runBrandList,
}

var brandGetCmd = &cobra.Command{
	Use:   "get <brand_id>",
	Short: "Get a brand by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runBrandGet,
}

var (
	brandCreateAlias       string
	brandCreateLegalName   string
	brandCreateEIN         string
	brandCreateEINCountry  string
	brandCreateVertical    string
	brandCreateWebsite     string
	brandCreateEmail       string
	brandCreatePhone       string
	brandCreateType        string
	brandCreateEntityType  string
	brandCreateStockSymbol string
	brandCreateStockExch   string
)

var brandCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new brand (spends money — TCR registration fee, requires --yes)",
	RunE:  runBrandCreate,
}

var (
	brandUpdateEmail   string
	brandUpdatePhone   string
	brandUpdateWebsite string
)

var brandUpdateCmd = &cobra.Command{
	Use:   "update <brand_id>",
	Short: "Update mutable brand fields",
	Args:  cobra.ExactArgs(1),
	RunE:  runBrandUpdate,
}

func init() {
	brandListCmd.Flags().IntVar(&brandListLimit, "limit", 20, "results per page")
	brandListCmd.Flags().IntVar(&brandListOffset, "offset", 0, "pagination offset")

	brandCreateCmd.Flags().StringVar(&brandCreateAlias, "alias", "", "human-friendly alias (required)")
	_ = brandCreateCmd.MarkFlagRequired("alias")
	brandCreateCmd.Flags().StringVar(&brandCreateLegalName, "legal-name", "", "legal entity name (required)")
	_ = brandCreateCmd.MarkFlagRequired("legal-name")
	brandCreateCmd.Flags().StringVar(&brandCreateEIN, "ein", "", "tax ID / EIN")
	brandCreateCmd.Flags().StringVar(&brandCreateEINCountry, "ein-issuing-country", "US", "ISO-2 country issuing the EIN")
	brandCreateCmd.Flags().StringVar(&brandCreateVertical, "vertical", "", "industry vertical (e.g. TECHNOLOGY, RETAIL)")
	brandCreateCmd.Flags().StringVar(&brandCreateWebsite, "website", "", "primary website URL")
	brandCreateCmd.Flags().StringVar(&brandCreateEmail, "email", "", "support email")
	brandCreateCmd.Flags().StringVar(&brandCreatePhone, "phone", "", "support phone E.164")
	brandCreateCmd.Flags().StringVar(&brandCreateType, "brand-type", "STANDARD", "STANDARD|LOW_VOLUME_STANDARD|SOLE_PROPRIETOR")
	brandCreateCmd.Flags().StringVar(&brandCreateEntityType, "entity-type", "PRIVATE_PROFIT", "PRIVATE_PROFIT|PUBLIC_PROFIT|NON_PROFIT|GOVERNMENT|SOLE_PROPRIETOR")
	brandCreateCmd.Flags().StringVar(&brandCreateStockSymbol, "stock-symbol", "", "ticker (PUBLIC_PROFIT only)")
	brandCreateCmd.Flags().StringVar(&brandCreateStockExch, "stock-exchange", "", "exchange code (PUBLIC_PROFIT only)")

	brandUpdateCmd.Flags().StringVar(&brandUpdateEmail, "email", "", "support email")
	brandUpdateCmd.Flags().StringVar(&brandUpdatePhone, "phone", "", "support phone")
	brandUpdateCmd.Flags().StringVar(&brandUpdateWebsite, "website", "", "website URL")

	brandCmd.AddCommand(brandListCmd, brandGetCmd, brandCreateCmd, brandUpdateCmd)
	rootCmd.AddCommand(brandCmd)
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
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "Brand"), nil, q, &resp)
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
		return output.JSONSuccess(os.Stdout, resp.Brands, resp.Meta)
	}
	rows := [][]string{{"BRAND_ID", "ALIAS", "LEGAL_NAME", "TYPE", "STATUS", "VERTICAL"}}
	for _, b := range resp.Brands {
		rows = append(rows, []string{b.BrandID, b.BrandAlias, b.LegalEntityName, b.BrandType, b.BrandStatus, b.Vertical})
	}
	return output.Table(os.Stdout, rows)
}

func runBrandGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var b api.Brand10DLC
	apiErr, err := client.Do("GET", client.AccountURL("10dlc", "Brand", id), nil, nil, &b)
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
		return output.JSONSuccess(os.Stdout, b, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"brand_id", b.BrandID},
		{"alias", b.BrandAlias},
		{"legal_name", b.LegalEntityName},
		{"brand_type", b.BrandType},
		{"entity_type", b.EntityType},
		{"status", b.BrandStatus},
		{"vertical", b.Vertical},
		{"website", b.Website},
		{"email", b.Email},
		{"phone", b.Phone},
	})
}

func runBrandCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"brand_alias":         brandCreateAlias,
		"legal_entity_name":   brandCreateLegalName,
		"ein_issuing_country": brandCreateEINCountry,
		"brand_type":          brandCreateType,
		"entity_type":         brandCreateEntityType,
	}
	addIfSet := func(k, v string) {
		if v != "" {
			body[k] = v
		}
	}
	addIfSet("ein", brandCreateEIN)
	addIfSet("vertical", brandCreateVertical)
	addIfSet("website", brandCreateWebsite)
	addIfSet("email", brandCreateEmail)
	addIfSet("phone", brandCreatePhone)
	addIfSet("stock_symbol", brandCreateStockSymbol)
	addIfSet("stock_exchange", brandCreateStockExch)

	effectiveDryRun := dryRunFlag || !yesFlag
	if effectiveDryRun {
		client.DryRun = true
		if !dryRunFlag {
			fmt.Fprintln(os.Stderr, "[dry-run] brand registration costs a TCR fee; pass --yes to actually submit")
		}
	}

	var resp struct {
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
	if effectiveDryRun {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"brand_id", resp.BrandID},
		{"message", resp.Message},
	})
}

func runBrandUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	body := map[string]any{}
	if brandUpdateEmail != "" {
		body["email"] = brandUpdateEmail
	}
	if brandUpdatePhone != "" {
		body["phone"] = brandUpdatePhone
	}
	if brandUpdateWebsite != "" {
		body["website"] = brandUpdateWebsite
	}
	if len(body) == 0 {
		return clierr.BadInput("at least one of --email, --phone, --website required")
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("10dlc", "Brand", id), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated brand %s: %s\n", id, resp.Message)
	return nil
}
