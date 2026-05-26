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

var complianceCmd = &cobra.Command{
	Use:   "compliance",
	Short: "Manage compliance documents (KYC, business proofs)",
}

var (
	cdListLimit  int
	cdListOffset int
)

var cdListCmd = &cobra.Command{
	Use:   "list",
	Short: "List compliance documents",
	RunE:  runComplianceList,
}

var cdGetCmd = &cobra.Command{
	Use:   "get <document_id>",
	Short: "Get a compliance document by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runComplianceGet,
}

var cdDeleteCmd = &cobra.Command{
	Use:   "delete <document_id>",
	Short: "Delete a compliance document (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runComplianceDelete,
}

func init() {
	cdListCmd.Flags().IntVar(&cdListLimit, "limit", 20, "results per page")
	cdListCmd.Flags().IntVar(&cdListOffset, "offset", 0, "pagination offset")

	complianceCmd.AddCommand(cdListCmd, cdGetCmd, cdDeleteCmd)
	rootCmd.AddCommand(complianceCmd)
}

func runComplianceList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(cdListLimit))
	q.Set("offset", strconv.Itoa(cdListOffset))
	var resp api.ComplianceDocumentList
	apiErr, err := client.Do("GET", client.AccountURL("ComplianceDocument"), nil, q, &resp)
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
	rows := [][]string{{"ID", "TYPE", "ALIAS", "FILE", "CREATED"}}
	for _, d := range resp.Objects {
		rows = append(rows, []string{d.ID, d.DocumentTypeID, d.Alias, d.FileName, d.CreatedAt})
	}
	return output.Table(os.Stdout, rows)
}

func runComplianceGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var d api.ComplianceDocument
	apiErr, err := client.Do("GET", client.AccountURL("ComplianceDocument", id), nil, nil, &d)
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
		return output.JSONSuccess(os.Stdout, d, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"id", d.ID},
		{"type", d.DocumentTypeID},
		{"alias", d.Alias},
		{"file_name", d.FileName},
		{"created_at", d.CreatedAt},
	})
}

func runComplianceDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete compliance document " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("ComplianceDocument", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted compliance document %s\n", id)
	return nil
}
