package cmd

import (
	"net/http"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// --live reads the call in progress (?status=live), whose fields are from and
// to, not the record's from_number and to_number. Once it ends, the hint
// says to drop --live.
func TestCallGet_liveReadsTheCallInProgress(t *testing.T) {
	setFakeCreds(t)
	urls, _ := sipServer(t, http.StatusOK, `{"direction":"inbound","from":"14155550100","call_status":"in-progress",`+
		`"api_id":"x","to":"14155550111","caller_name":"+14155550100","call_uuid":"c1","session_start":"2021-03-23 14:49:39.722551"}`)
	err, stdout, _ := execCmd(t, "voice", "calls", "get", "c1", "--live", "-o", "table")
	for _, want := range []string{"in-progress", "14155550100", "14155550111", "inbound", "2021-03-23 14:49:39.722551"} {
		if err != nil || !strings.Contains(stdout, want) || !strings.HasSuffix(urls()[0], "/Call/c1/?status=live") {
			t.Fatalf("%v, %v: want %q in\n%s", err, urls(), want, stdout)
		}
	}
	_, count := sipServer(t, http.StatusOK, `{}`)
	clientForTest.DryRun = true // as getClient sets it under --dry-run
	if err, _, stderr := execCmd(t, "voice", "calls", "get", "c1", "--live", "--dry-run"); err != nil || count() != 0 || !strings.Contains(stderr, "/Call/c1/?status=live") {
		t.Errorf("--dry-run: %v, %d requests, stderr %q", err, count(), stderr)
	}
	sipServer(t, http.StatusNotFound, `{"error":"not found"}`)
	err, _, _ = execCmd(t, "voice", "calls", "get", "c1", "--live")
	if e, ok := err.(*clierr.Error); !ok || !strings.Contains(e.Hint, "drop --live") {
		t.Errorf("an ended call: %v, want a hint to drop --live", err)
	}
}
