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

var applicationCmd = &cobra.Command{
	Use:     "applications",
	Aliases: []string{"app", "application"},
	Short:   "Manage Plivo applications (voice/messaging webhooks)",
}

var (
	appCreateName              string
	appCreateAnswerURL         string
	appCreateAnswerMethod      string
	appCreateHangupURL         string
	appCreateMessageURL        string
	appCreateFallbackAnswerURL string
	appCreateDefaultNumberApp  bool
	appCreateLogIncoming       bool
)

var applicationCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new application",
	RunE:  runAppCreate,
}

var (
	appListLimit  int
	appListOffset int
)

var applicationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List applications",
	RunE:  runAppList,
}

var applicationGetCmd = &cobra.Command{
	Use:   "get <app_id>",
	Short: "Get application details",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppGet,
}

var (
	appUpdateName         string
	appUpdateAnswerURL    string
	appUpdateAnswerMethod string
	appUpdateHangupURL    string
	appUpdateMessageURL   string
)

var applicationUpdateCmd = &cobra.Command{
	Use:   "update <app_id>",
	Short: "Update an application",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppUpdate,
}

var appDeleteCascade bool

var applicationDeleteCmd = &cobra.Command{
	Use:   "delete <app_id>",
	Short: "Delete an application (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppDelete,
}

func init() {
	applicationCreateCmd.Flags().StringVar(&appCreateName, "app-name", "", "application name (required)")
	_ = applicationCreateCmd.MarkFlagRequired("app-name")
	applicationCreateCmd.Flags().StringVar(&appCreateAnswerURL, "answer-url", "", "webhook for incoming calls (required)")
	_ = applicationCreateCmd.MarkFlagRequired("answer-url")
	applicationCreateCmd.Flags().StringVar(&appCreateAnswerMethod, "answer-method", "POST", "GET|POST")
	applicationCreateCmd.Flags().StringVar(&appCreateHangupURL, "hangup-url", "", "webhook for call hangup")
	applicationCreateCmd.Flags().StringVar(&appCreateMessageURL, "message-url", "", "webhook for inbound SMS")
	applicationCreateCmd.Flags().StringVar(&appCreateFallbackAnswerURL, "fallback-answer-url", "", "backup webhook if answer-url fails")
	applicationCreateCmd.Flags().BoolVar(&appCreateDefaultNumberApp, "default-number-app", false, "set as default for new numbers")
	applicationCreateCmd.Flags().BoolVar(&appCreateLogIncoming, "log-incoming-messages", true, "log inbound SMS content")

	applicationListCmd.Flags().IntVar(&appListLimit, "limit", 20, "results per page")
	applicationListCmd.Flags().IntVar(&appListOffset, "offset", 0, "pagination offset")

	applicationUpdateCmd.Flags().StringVar(&appUpdateName, "app-name", "", "new application name")
	applicationUpdateCmd.Flags().StringVar(&appUpdateAnswerURL, "answer-url", "", "new answer URL")
	applicationUpdateCmd.Flags().StringVar(&appUpdateAnswerMethod, "answer-method", "", "GET|POST")
	applicationUpdateCmd.Flags().StringVar(&appUpdateHangupURL, "hangup-url", "", "new hangup URL")
	applicationUpdateCmd.Flags().StringVar(&appUpdateMessageURL, "message-url", "", "new message URL")

	applicationDeleteCmd.Flags().BoolVar(&appDeleteCascade, "cascade", false, "also detach numbers/endpoints")

	applicationCmd.AddCommand(applicationCreateCmd, applicationListCmd, applicationGetCmd, applicationUpdateCmd, applicationDeleteCmd)
	accountCmd.AddCommand(applicationCmd)
}

func runAppCreate(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"app_name":              appCreateName,
		"answer_url":            appCreateAnswerURL,
		"answer_method":         appCreateAnswerMethod,
		"log_incoming_messages": appCreateLogIncoming,
	}
	if appCreateHangupURL != "" {
		body["hangup_url"] = appCreateHangupURL
	}
	if appCreateMessageURL != "" {
		body["message_url"] = appCreateMessageURL
	}
	if appCreateFallbackAnswerURL != "" {
		body["fallback_answer_url"] = appCreateFallbackAnswerURL
	}
	if appCreateDefaultNumberApp {
		body["default_number_app"] = true
	}

	if explainFlag {
		fmt.Fprintf(os.Stderr, "Will POST %s\n", client.AccountURL("Application"))
	}

	var resp struct {
		APIID   string `json:"api_id"`
		AppID   string `json:"app_id"`
		Message string `json:"message"`
	}
	apiErr, err := client.Do("POST", client.AccountURL("Application"), body, nil, &resp)
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
	fmt.Fprintf(os.Stderr, "%s\n", resp.Message)
	return output.KV(os.Stdout, [][2]string{
		{"app_id", resp.AppID},
		{"api_id", resp.APIID},
	})
}

func runAppList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(appListLimit))
	q.Set("offset", strconv.Itoa(appListOffset))

	var resp api.ApplicationList
	apiErr, err := client.Do("GET", client.AccountURL("Application"), nil, q, &resp)
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
	rows := [][]string{{"APP_ID", "NAME", "ANSWER_URL", "MESSAGE_URL", "ENABLED"}}
	for _, a := range resp.Objects {
		rows = append(rows, []string{a.AppID, a.AppName, a.AnswerURL, a.MessageURL, fmt.Sprintf("%v", a.Enabled)})
	}
	return output.Table(os.Stdout, rows)
}

func runAppGet(cmd *cobra.Command, args []string) error {
	appID := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var a api.Application
	apiErr, err := client.Do("GET", client.AccountURL("Application", appID), nil, nil, &a)
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
		return output.JSONSuccess(os.Stdout, a, nil)
	}
	return output.KV(os.Stdout, [][2]string{
		{"app_id", a.AppID},
		{"app_name", a.AppName},
		{"answer_url", a.AnswerURL},
		{"answer_method", a.AnswerMethod},
		{"hangup_url", a.HangupURL},
		{"message_url", a.MessageURL},
		{"enabled", fmt.Sprintf("%v", a.Enabled)},
		{"sip_uri", a.SIPURI},
		{"public_uri", fmt.Sprintf("%v", a.PublicURI)},
	})
}

func runAppUpdate(cmd *cobra.Command, args []string) error {
	appID := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	body := map[string]any{}
	if appUpdateName != "" {
		body["app_name"] = appUpdateName
	}
	if appUpdateAnswerURL != "" {
		body["answer_url"] = appUpdateAnswerURL
	}
	if appUpdateAnswerMethod != "" {
		body["answer_method"] = appUpdateAnswerMethod
	}
	if appUpdateHangupURL != "" {
		body["hangup_url"] = appUpdateHangupURL
	}
	if appUpdateMessageURL != "" {
		body["message_url"] = appUpdateMessageURL
	}
	if len(body) == 0 {
		return fmt.Errorf("at least one update flag required")
	}
	var resp api.GenericResponse
	apiErr, err := client.Do("POST", client.AccountURL("Application", appID), body, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Updated %s: %s\n", appID, resp.Message)
	return nil
}

func runAppDelete(cmd *cobra.Command, args []string) error {
	appID := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete application " + appID)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	if appDeleteCascade {
		q.Set("cascade", "true")
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Application", appID), nil, q, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted application %s\n", appID)
	return nil
}
