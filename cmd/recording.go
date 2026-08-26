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

var recordingCmd = &cobra.Command{
	Use:     "recordings",
	Aliases: []string{"rec", "recording"},
	Short:   "List, fetch, and delete call/conference recordings",
}

var (
	recListLimit    int
	recListOffset   int
	recListCallUUID string
	recListConf     string
	recListFromTime string
	recListToTime   string
)

var recordingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recordings",
	RunE:  runRecordingList,
}

var recordingGetCmd = &cobra.Command{
	Use:   "get <recording_id>",
	Short: "Get a recording by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runRecordingGet,
}

var recordingDeleteCmd = &cobra.Command{
	Use:   "delete <recording_id>",
	Short: "Delete a recording (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRecordingDelete,
}

func init() {
	recordingListCmd.Flags().IntVar(&recListLimit, "limit", 20, "results per page")
	recordingListCmd.Flags().IntVar(&recListOffset, "offset", 0, "pagination offset")
	recordingListCmd.Flags().StringVar(&recListCallUUID, "call-uuid", "", "filter by call uuid")
	recordingListCmd.Flags().StringVar(&recListConf, "conference-name", "", "filter by conference name")
	recordingListCmd.Flags().StringVar(&recListFromTime, "from-time", "", "filter recordings after this ISO time")
	recordingListCmd.Flags().StringVar(&recListToTime, "to-time", "", "filter recordings before this ISO time")

	recordingCmd.AddCommand(recordingListCmd, recordingGetCmd, recordingDeleteCmd)
	voiceCmd.AddCommand(recordingCmd)
}

func runRecordingList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(recListLimit))
	q.Set("offset", strconv.Itoa(recListOffset))
	if recListCallUUID != "" {
		q.Set("call_uuid", recListCallUUID)
	}
	if recListConf != "" {
		q.Set("conference_name", recListConf)
	}
	if recListFromTime != "" {
		q.Set("from_time", recListFromTime)
	}
	if recListToTime != "" {
		q.Set("to_time", recListToTime)
	}

	var resp api.RecordingList
	apiErr, err := client.Do("GET", client.AccountURL("Recording"), nil, q, &resp)
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
	rows := [][]string{{"RECORDING_ID", "CALL_UUID", "TYPE", "FORMAT", "DURATION_MS", "ADDED"}}
	for _, r := range resp.Objects {
		rows = append(rows, []string{r.RecordingID, r.CallUUID, r.RecordingType, r.RecordingFormat, strconv.Itoa(r.RecordingDurationMS), r.AddTime})
	}
	return output.Table(os.Stdout, rows)
}

func runRecordingGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var r api.Recording
	apiErr, err := client.Do("GET", client.AccountURL("Recording", id), nil, nil, &r)
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
		return output.JSONRaw(os.Stdout, r.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"recording_id", r.RecordingID},
		{"call_uuid", r.CallUUID},
		{"conference_name", r.ConferenceName},
		{"type", r.RecordingType},
		{"format", r.RecordingFormat},
		{"url", r.RecordingURL},
		{"duration_ms", strconv.Itoa(r.RecordingDurationMS)},
		{"add_time", r.AddTime},
	})
}

func runRecordingDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	if !yesFlag {
		return clierr.DestructiveRefused("delete recording " + id)
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("Recording", id), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Deleted recording %s\n", id)
	return nil
}
