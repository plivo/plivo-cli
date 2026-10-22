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

// make refuses what it cannot dial before the spend guard: --yes sends nothing.
func TestCallMake_refusesWhatItCannotDial(t *testing.T) {
	setFakeCreds(t)
	for _, tc := range []struct{ flag, from, to string }{
		{"to", "+14155550100", "hello"},
		{"to", "+14155550100", "14155550101<sip:"},
		{"from", "sip:alice@example.com", "14155550101"},
	} {
		_, count := sipServer(t, http.StatusCreated, `{}`)
		err, _, _ := execCmd(t, "voice", "calls", "make", "--from", tc.from, "--to", tc.to, "--yes")
		if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeBadFlag || e.Context["flag"] != tc.flag || count() != 0 {
			t.Errorf("--from %s --to %s: %v, %d requests; want BAD_FLAG on --%s, nothing sent", tc.from, tc.to, err, count(), tc.flag)
		}
	}
	_, count := sipServer(t, http.StatusCreated, `{"request_uuid":"r1"}`)
	if err, _, _ := execCmd(t, "voice", "calls", "make", "--from", "14155550100", "--to", "+14155550101<sip:alice@example.com", "--yes"); err != nil || count() != 1 {
		t.Errorf("a number and a sip: URI: %v, %d requests", err, count())
	}
}
