package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// -o json on these writes printed nothing on stdout and exited 0, which a
// script cannot tell from an empty result. Each prints a JSON result now.
func TestActions_printJSONOnStdout(t *testing.T) {
	setFakeCreds(t)
	pointCommandsAtTestServer(t, `{"api_id": "test", "message": "ok"}`)
	for _, line := range []string{
		"voice calls hangup UUID --yes", "voice calls transfer UUID --aleg-url https://example.com/x",
		"voice calls play UUID --urls https://example.com/a.mp3", "voice calls stop-play UUID",
		"voice calls speak UUID --text hi", "voice calls stop-speak UUID", "voice calls dtmf UUID --digits 12",
		"voice calls stop-record UUID", "voice calls streams stop UUID --yes", "voice calls streams stop UUID UUID --yes",
	} {
		args := strings.Fields(strings.ReplaceAll(line, "UUID", "d1e2f3a4-0000-4000-8000-000000000001"))
		err, stdout, _ := execCmd(t, append(args, "-o", "json")...)
		var got struct{ Data map[string]any }
		if err != nil || json.Unmarshal([]byte(stdout), &got) != nil || len(got.Data) == 0 {
			t.Errorf("%s -o json: err %v, stdout %q; want a JSON result", line, err, stdout)
		}
	}
}
