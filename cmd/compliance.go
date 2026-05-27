package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// complianceCmd lives under `numbers` and wraps the unified number-compliance
// API at /v1/Account/{auth_id}/PhoneNumber/Compliance/.
var complianceCmd = &cobra.Command{
	Use:   "compliance",
	Short: "Regulatory compliance for phone numbers (requirements, applications, linking)",
}

// requirements
var (
	compReqCountry    string
	compReqNumberType string
	compReqUserType   string
)

var complianceRequirementsCmd = &cobra.Command{
	Use:     "requirements",
	Short:   "List documents/fields required to activate a regulated number",
	Long:    "Returns the document types and required data fields for a given country / number type\n/ user type — use the returned document_type_id values when building a `create` payload.",
	Example: "  plivo numbers compliance requirements --country US --number-type local --user-type business",
	RunE:    runComplianceRequirements,
}

// create
var (
	compCreateData  string
	compCreateFiles []string
)

var complianceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create + submit a compliance application (multipart; auto-submits)",
	Long: `Create and submit a compliance application for a regulated phone number.
Sent as multipart/form-data: the --data JSON is the "data" part and each --file
is an uploaded document.

--data (inline or @file.json) is the application payload:

  {
    "country_iso": "US",
    "number_type": "local",                 // local | mobile | tollfree
    "alias": "acme-us-local",
    "end_user": {
      "type": "business",                   // individual | business
      "name": "Acme Inc",
      "email": "ops@acme.com"
    },
    "documents": [
      { "document_type_id": "<id>", "data_fields": { "registration_number": "..." } }
    ],
    "callback_url": "https://example.com/compliance"   // optional
  }

--file attaches one upload per document; the field name maps positionally to the
documents[] array, so documents[0].file is the file for documents[0]. The path
may be bare or prefixed with @ (PDF/JPEG/PNG, max 5 MB).

Discover the document_type_id values and required data_fields first with
` + "`plivo numbers compliance requirements ...`" + `.`,
	Example: `  plivo numbers compliance create \
    --data @app.json \
    --file documents[0].file=@passport.pdf \
    --file documents[1].file=@address-proof.pdf`,
	RunE: runComplianceCreate,
}

// get
var compGetExpand string

var complianceGetCmd = &cobra.Command{
	Use:     "get <compliance_id>",
	Short:   "Get a compliance application by ID",
	Args:    cobra.ExactArgs(1),
	Example: "  plivo numbers compliance get <compliance_id> --expand end_user,documents,linked_numbers",
	RunE:    runComplianceGet,
}

// list
var (
	compListStatus     string
	compListCountry    string
	compListNumberType string
	compListUserType   string
	compListAlias      string
	compListLimit      int
	compListOffset     int
)

var complianceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List compliance applications",
	RunE:  runComplianceList,
}

// update
var (
	compUpdateData  string
	compUpdateFiles []string
)

var complianceUpdateCmd = &cobra.Command{
	Use:   "update <compliance_id>",
	Short: "Update a rejected compliance application (multipart; auto-resubmits)",
	Long: `Update a REJECTED compliance application and resubmit it. Same --data / --file
shape as create. Documents are FULLY REPLACED — re-attach every required file,
not only the changed ones. Only applications in 'rejected' status can be updated.`,
	Example: "  plivo numbers compliance update <compliance_id> --data @fixed.json --file documents[0].file=@passport.pdf",
	Args:    cobra.ExactArgs(1),
	RunE:    runComplianceUpdate,
}

// delete
var complianceDeleteCmd = &cobra.Command{
	Use:   "delete <compliance_id>",
	Short: "Delete a compliance application (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runComplianceDelete,
}

// link
var (
	compLinkPairs []string
	compLinkData  string
)

var complianceLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Bulk-link numbers to accepted compliance applications",
	Long: `Associate rented numbers with accepted compliance applications. Repeat --link
once per number, or pass the full JSON body via --data (inline or @file.json):
{"numbers":[{"number":"+14155551234","compliance_application_id":"<id>"}]}.`,
	Example: "  plivo numbers compliance link --link +14155551234=<compliance_id> --link +14155556789=<compliance_id>",
	RunE:    runComplianceLink,
}

func init() {
	complianceRequirementsCmd.Flags().StringVar(&compReqCountry, "country", "", "ISO country code, e.g. US (required)")
	complianceRequirementsCmd.Flags().StringVar(&compReqNumberType, "number-type", "", "local|mobile|tollfree (required)")
	complianceRequirementsCmd.Flags().StringVar(&compReqUserType, "user-type", "", "individual|business (required)")
	_ = complianceRequirementsCmd.MarkFlagRequired("country")
	_ = complianceRequirementsCmd.MarkFlagRequired("number-type")
	_ = complianceRequirementsCmd.MarkFlagRequired("user-type")

	complianceCreateCmd.Flags().StringVar(&compCreateData, "data", "", "application JSON; inline or @file.json (required)")
	complianceCreateCmd.Flags().StringArrayVar(&compCreateFiles, "file", nil, "document upload as field=path, e.g. documents[0].file=@id.pdf (repeatable)")
	_ = complianceCreateCmd.MarkFlagRequired("data")

	complianceGetCmd.Flags().StringVar(&compGetExpand, "expand", "", "comma-separated: end_user,documents,linked_numbers")

	complianceListCmd.Flags().StringVar(&compListStatus, "status", "", "filter by status")
	complianceListCmd.Flags().StringVar(&compListCountry, "country", "", "filter by ISO country code")
	complianceListCmd.Flags().StringVar(&compListNumberType, "number-type", "", "filter by number type")
	complianceListCmd.Flags().StringVar(&compListUserType, "user-type", "", "filter by user type")
	complianceListCmd.Flags().StringVar(&compListAlias, "alias", "", "filter by alias")
	complianceListCmd.Flags().IntVar(&compListLimit, "limit", 20, "results per page")
	complianceListCmd.Flags().IntVar(&compListOffset, "offset", 0, "pagination offset")

	complianceUpdateCmd.Flags().StringVar(&compUpdateData, "data", "", "updated application JSON; inline or @file.json (required)")
	complianceUpdateCmd.Flags().StringArrayVar(&compUpdateFiles, "file", nil, "document upload as field=path (repeatable; replaces all documents)")
	_ = complianceUpdateCmd.MarkFlagRequired("data")

	complianceLinkCmd.Flags().StringArrayVar(&compLinkPairs, "link", nil, "number=compliance_application_id (repeatable)")
	complianceLinkCmd.Flags().StringVar(&compLinkData, "data", "", "full link JSON body; inline or @file.json (alternative to --link)")

	complianceCmd.AddCommand(
		complianceRequirementsCmd, complianceCreateCmd, complianceGetCmd,
		complianceListCmd, complianceUpdateCmd, complianceDeleteCmd, complianceLinkCmd,
	)
	numberCmd.AddCommand(complianceCmd)
}

// readDataArg returns the bytes for a --data value: @file reads the file,
// anything else is treated as an inline JSON string. The result is validated
// as JSON so malformed payloads fail before any upload.
func readDataArg(v string) ([]byte, error) {
	var b []byte
	if strings.HasPrefix(v, "@") {
		data, err := os.ReadFile(v[1:])
		if err != nil {
			return nil, fmt.Errorf("reading --data file: %w", err)
		}
		b = data
	} else {
		b = []byte(v)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("--data is not valid JSON")
	}
	return b, nil
}

// parseFileFlags turns ["documents[0].file=@id.pdf"] into {field: path}. A
// leading @ on the path (curl-style) is optional and stripped.
func parseFileFlags(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		i := strings.Index(p, "=")
		if i <= 0 {
			return nil, fmt.Errorf("--file must be field=path, got %q", p)
		}
		field, path := p[:i], strings.TrimPrefix(p[i+1:], "@")
		if path == "" {
			return nil, fmt.Errorf("--file %q has no path", p)
		}
		out[field] = path
	}
	return out, nil
}

func runComplianceRequirements(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("country_iso", compReqCountry)
	q.Set("number_type", compReqNumberType)
	q.Set("user_type", compReqUserType)
	var resp api.ComplianceRequirements
	apiErr, err := client.Do("GET", client.AccountURL("PhoneNumber", "Compliance", "Requirements"), nil, q, &resp)
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
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	rows := [][]string{{"DOCUMENT_TYPE_ID", "NAME", "PROOF_REQUIRED", "REQUIRED_FIELDS"}}
	for _, dt := range resp.DocumentTypes {
		rows = append(rows, []string{dt.DocumentTypeID, dt.Name, strconv.FormatBool(dt.ProofRequired), strconv.Itoa(len(dt.RequiredFields))})
	}
	return output.Table(os.Stdout, rows)
}

func runComplianceCreate(cmd *cobra.Command, args []string) error {
	data, err := readDataArg(compCreateData)
	if err != nil {
		return err
	}
	files, err := parseFileFlags(compCreateFiles)
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.ComplianceCreateResp
	apiErr, err := client.DoMultipart("POST", client.AccountURL("PhoneNumber", "Compliance"), data, files, &resp)
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
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"compliance_id", resp.ComplianceID},
		{"message", resp.Message},
	})
}

func runComplianceGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	if compGetExpand != "" {
		q.Set("expand", compGetExpand)
	}
	var app api.ComplianceApplication
	apiErr, err := client.Do("GET", client.AccountURL("PhoneNumber", "Compliance", id), nil, q, &app)
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
		return output.JSONSuccess(os.Stdout, app, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"compliance_id", app.ComplianceID},
		{"alias", app.Alias},
		{"status", app.Status},
		{"country_iso", app.CountryISO},
		{"number_type", app.NumberType},
		{"user_type", app.UserType},
		{"rejection_reason", app.RejectionReason},
		{"created_at", app.CreatedAt},
		{"updated_at", app.UpdatedAt},
	})
}

func runComplianceList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(compListLimit))
	q.Set("offset", strconv.Itoa(compListOffset))
	for k, v := range map[string]string{
		"status": compListStatus, "country_iso": compListCountry,
		"number_type": compListNumberType, "user_type": compListUserType, "alias": compListAlias,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	var resp api.ComplianceApplicationList
	apiErr, err := client.Do("GET", client.AccountURL("PhoneNumber", "Compliance"), nil, q, &resp)
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
		return output.JSONSuccess(os.Stdout, resp.Objects, resp.Meta)
	}
	rows := [][]string{{"COMPLIANCE_ID", "ALIAS", "STATUS", "COUNTRY", "NUMBER_TYPE", "CREATED"}}
	for _, a := range resp.Objects {
		rows = append(rows, []string{a.ComplianceID, a.Alias, a.Status, a.CountryISO, a.NumberType, a.CreatedAt})
	}
	return output.Table(os.Stdout, rows)
}

func runComplianceUpdate(cmd *cobra.Command, args []string) error {
	id := args[0]
	data, err := readDataArg(compUpdateData)
	if err != nil {
		return err
	}
	files, err := parseFileFlags(compUpdateFiles)
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var app api.ComplianceApplication
	apiErr, err := client.DoMultipart("PATCH", client.AccountURL("PhoneNumber", "Compliance", id), data, files, &app)
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
		return output.JSONSuccess(os.Stdout, app, nil)
	}
	fmt.Fprintf(os.Stderr, "Updated compliance application %s\n", id)
	return nil
}

func runComplianceDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete compliance application " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("PhoneNumber", "Compliance", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted compliance application %s\n", id)
	return nil
}

func runComplianceLink(cmd *cobra.Command, args []string) error {
	var body any
	if compLinkData != "" {
		raw, err := readDataArg(compLinkData)
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("--data: %w", err)
		}
		body = m
	} else {
		if len(compLinkPairs) == 0 {
			return fmt.Errorf("provide --link number=compliance_application_id (repeatable) or --data")
		}
		numbers := make([]map[string]string, 0, len(compLinkPairs))
		for _, p := range compLinkPairs {
			i := strings.Index(p, "=")
			if i <= 0 {
				return fmt.Errorf("--link must be number=compliance_application_id, got %q", p)
			}
			numbers = append(numbers, map[string]string{
				"number":                    p[:i],
				"compliance_application_id": p[i+1:],
			})
		}
		body = map[string]any{"numbers": numbers}
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.ComplianceLinkResp
	apiErr, err := client.Do("POST", client.AccountURL("PhoneNumber", "Compliance", "Link"), body, nil, &resp)
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
		return output.JSONSuccess(os.Stdout, resp, nil)
	}
	rows := [][]string{{"NUMBER", "STATUS", "REMARKS"}}
	for _, r := range resp.Report {
		rows = append(rows, []string{r.Number, r.Status, r.Remarks})
	}
	fmt.Fprintf(os.Stderr, "linked %d/%d\n", resp.UpdatedCount, resp.TotalCount)
	return output.Table(os.Stdout, rows)
}
