package cmd

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// Record sends the fields the Record APIs take, those that are set.
func TestRecord_sendsTheAPIsFields(t *testing.T) {
	transcribe := " --transcription-type auto --transcription-url https://example.com/t"
	for args, want := range map[string]string{
		"voice calls record c1": "file_format time_limit",
		"voice calls record c1 --record-channel-type stereo" + transcribe: "file_format record_channel_type time_limit transcription_type transcription_url",
		"voice conferences record room" + transcribe:                      "file_format time_limit transcription_type transcription_url",
	} {
		setFakeCreds(t)
		sent := serveFixture(t, `{"api_id":"x","message":"ok"}`)
		err, _, _ := execCmd(t, strings.Fields(args)...)
		if _, bodies := sent(); err != nil || len(bodies) != 1 || strings.Join(slices.Sorted(maps.Keys(bodies[0])), " ") != want {
			t.Errorf("%s: err %v, sent %v; want the keys %s", args, err, bodies, want)
		}
	}
}

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

// streams start sends status_callback_url, the API's name for it, and refuses
// a two-way stream on the outbound or both track, as the API would.
func TestStreamStart_sendsTheAPIsFieldNamesAndTracks(t *testing.T) {
	setFakeCreds(t)
	start := []string{"voice", "calls", "streams", "start", "c1", "--url", "wss://example.com/ws", "--dry-run"}
	if err, _, stderr := execCmd(t, append(start, "--stream-status-callback", "https://example.com/cb")...); err != nil || !strings.Contains(stderr, `"status_callback_url": "https://example.com/cb"`) {
		t.Errorf("%v, %q; want status_callback_url", err, stderr)
	}
	if err, _, stderr := execCmd(t, append(start, "--bidirectional", "--audio-track", "")...); err != nil || !strings.Contains(stderr, `"audio_track": "inbound"`) {
		t.Errorf(`--audio-track "": %v, %q; want the inbound track`, err, stderr)
	}
	for _, track := range []string{"outbound", "both"} {
		err, _, stderr := execCmd(t, append(start, "--bidirectional", "--audio-track", track)...)
		if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeBadFlag || strings.Contains(stderr, "[dry-run]") {
			t.Errorf("--bidirectional --audio-track %s: %v, %q; want BAD_FLAG and nothing sent", track, err, stderr)
		}
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

// list --live reads the calls in progress (?status=live), which come back as
// UUIDs alone, and refuses the list flags it cannot send.
func TestCallList_liveListsTheCallsInProgress(t *testing.T) {
	setFakeCreds(t)
	urls, count := sipServer(t, http.StatusOK, `{"api_id":"x","calls":["c1","c2"]}`)
	err, stdout, _ := execCmd(t, "voice", "calls", "list", "--live", "-o", "table")
	if err != nil || !slices.Equal(strings.Fields(stdout), []string{"UUID", "c1", "c2"}) || !strings.HasSuffix(urls()[0], "/Call/?status=live") {
		t.Fatalf("%v, %v: got\n%s", err, urls(), stdout)
	}
	err, _, _ = execCmd(t, "voice", "calls", "list", "--live", "--direction", "inbound", "--since", "2026-10-01")
	if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeBadFlag || !strings.Contains(e.Message, "--direction, --since") || count() != 1 {
		t.Errorf("--live with filters: %v after %d requests, want BAD_FLAG naming them and no request", err, count())
	}
	sipServer(t, http.StatusOK, `{"api_id":"x"}`)
	if err, stdout, _ := execCmd(t, "voice", "calls", "list", "--live", "-o", "json"); err != nil || !strings.Contains(stdout, `"calls": []`) {
		t.Errorf("no calls in progress: %v, want calls: [] in\n%s", err, stdout)
	}
}

// GET is for the static demo XML; another answer URL gets Plivo's default POST.
func TestCallMake_answerMethodGETOnlyForTheDemoURL(t *testing.T) {
	setFakeCreds(t)
	for args, get := range map[string]bool{"": true, "--answer-url https://example.com/a": false, "--answer-url https://example.com/a --answer-method GET": true} {
		err, _, stderr := execCmd(t, append([]string{"voice", "calls", "make", "--from", "+14155550100", "--to", "+14155550101", "--dry-run"}, strings.Fields(args)...)...)
		if err != nil || strings.Contains(stderr, `"answer_method": "GET"`) != get || strings.Contains(stderr, "answer_method") != get {
			t.Errorf("%q: %v, want answer_method GET sent = %v:\n%s", args, err, get, stderr)
		}
	}
}

// machine_detection is true or hangup: none, which help offered, is refused here.
func TestCallMake_refusesMachineDetectionNone(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "voice", "calls", "make", "--from", "+14155550100", "--to", "+14155550101", "--machine-detection", "none", "--dry-run")
	if e, ok := err.(*clierr.Error); !ok || e.Code != clierr.CodeBadFlag || e.Context["flag"] != "machine-detection" {
		t.Errorf("got %v, want BAD_FLAG on --machine-detection", err)
	}
}
