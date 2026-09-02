package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// captureOneRequest returns an httptest server that records the last
// request's body and replies with a minimal MessageSendResponse.
func captureOneRequest(t *testing.T) (srv *httptest.Server, getBody func() map[string]any) {
	t.Helper()
	var body map[string]any
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_id":"x","message":"queued","message_uuid":["uuid-1"]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]any { return body }
}

// mms send's --media-url must land as a media_urls array in the POST body —
// the flag didn't exist at all before, so MMS could never attach a picture.
func TestMmsSend_mediaURLs_includedInBody(t *testing.T) {
	setFakeCreds(t)
	srv, getBody := captureOneRequest(t)
	messageClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { messageClientForTest = nil }()

	err, _, _ := execCmd(t, "messaging", "mms", "send",
		"--src", "+14155550100", "--dst", "+14155550101", "--text", "hi",
		"--media-url", "https://example.com/a.jpg", "--media-url", "https://example.com/b.jpg",
		"--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := getBody()
	got, ok := body["media_urls"].([]any)
	if !ok {
		t.Fatalf("media_urls missing or wrong type in body: %#v", body)
	}
	want := []string{"https://example.com/a.jpg", "https://example.com/b.jpg"}
	if len(got) != len(want) {
		t.Fatalf("media_urls len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("media_urls[%d] = %v, want %q", i, got[i], w)
		}
	}
}

// Without --media-url, the body must not carry the key at all (no empty
// array noise on the wire for sms/whatsapp, which don't expose the flag).
func TestMmsSend_noMediaURL_keyOmitted(t *testing.T) {
	setFakeCreds(t)
	srv, getBody := captureOneRequest(t)
	messageClientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKE", AuthToken: "tok", HTTP: &http.Client{}}
	defer func() { messageClientForTest = nil }()

	err, _, _ := execCmd(t, "messaging", "mms", "send",
		"--src", "+14155550100", "--dst", "+14155550101", "--text", "hi", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := getBody()["media_urls"]; ok {
		t.Errorf("media_urls should be omitted when --media-url wasn't passed, got body: %#v", getBody())
	}
}

// --media-url must not exist on sms/whatsapp send — MMS is the only channel
// the ticket asked for, and Plivo's SMS API doesn't take media.
func TestSmsWhatsappSend_noMediaURLFlag(t *testing.T) {
	if messagingSmsSendCmd.Flags().Lookup("media-url") != nil {
		t.Error("sms send should not have --media-url")
	}
	if messagingWhatsappSendCmd.Flags().Lookup("media-url") != nil {
		t.Error("whatsapp send should not have --media-url")
	}
	if messagingMmsSendCmd.Flags().Lookup("media-url") == nil {
		t.Error("mms send should have --media-url")
	}
}

// --dst help text must render as a normal string flag, not the pflag
// type-name-override glitch ("--dst <").
func TestMmsSend_dstHelpText_notMalformed(t *testing.T) {
	var buf strings.Builder
	messagingMmsSendCmd.SetOut(&buf)
	defer messagingMmsSendCmd.SetOut(nil)
	_ = messagingMmsSendCmd.Help()
	out := buf.String()
	if strings.Contains(out, "--dst <") {
		t.Errorf("--dst help text still malformed, got:\n%s", out)
	}
	if !strings.Contains(out, "--dst string") {
		t.Errorf("--dst should render as a string flag, got:\n%s", out)
	}
}
