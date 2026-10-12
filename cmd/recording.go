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
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var (
	recListLimit    int
	recListOffset   int
	recListCallUUID string
	recListConf     string
	recListRange    listTimeRange
)

var recordingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recordings",
	Long: `List recordings. Without --since, Plivo returns the last 7 days.

--since and --until (or --from-date and --to-date) filter on when a recording
was added, in UTC, and a bare --until date includes that day. --until needs
--since. Plivo searches at most 31 days per request; a longer range is refused
before any request, with the ranges to run instead.`,
	Example: `  plivo voice recordings list --since 2026-10-01 -o json`,
	Args:    cobra.NoArgs,
	RunE:    runRecordingList,
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
	registerListFlags(recordingListCmd, &recListLimit, &recListOffset)
	recordingListCmd.Flags().StringVar(&recListCallUUID, "call-uuid", "", "filter by call uuid")
	recordingListCmd.Flags().StringVar(&recListConf, "conference-name", "", "filter by conference name")
	registerTimeRangeFlags(recordingListCmd, &recListRange, "recordings added", rangeLimits{spanDays: listSpanDays})
	// --from-time and --to-time were sent as from_time and to_time, which the
	// API ignores, so they filtered nothing. They now set the same range.
	recListRange.deprecatedNames(recordingListCmd, "from-time", "to-time")

	recordingCmd.AddCommand(recordingListCmd, recordingGetCmd, recordingDeleteCmd)
	voiceCmd.AddCommand(recordingCmd)
}

func runRecordingList(cmd *cobra.Command, args []string) error {
	since, until, err := recListRange.bounds(cmd)
	if err != nil {
		return err
	}
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
	setTimeRange(q, "add_time", since, until)

	var resp api.RecordingList
	if err := fetchList(client, client.AccountURL("Recording"), q, "objects", &resp); err != nil {
		return err
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
	}
	rows := [][]string{{"RECORDING_ID", "CALL_UUID", "TYPE", "FORMAT", "DURATION_MS", "ADDED"}}
	for _, r := range resp.Objects {
		rows = append(rows, []string{r.RecordingID, r.CallUUID, r.RecordingType, r.RecordingFormat, r.RecordingDurationMS, r.AddTime})
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
		{"duration_ms", r.RecordingDurationMS},
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
