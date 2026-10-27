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
		"voice conferences hangup room --yes", "voice conferences stop-record room --yes",
		"voice conferences member mute room 11", "voice conferences member unmute room 11",
		"voice conferences member deaf room 11", "voice conferences member undeaf room 11",
		"voice conferences member kick room 11 --yes", "voice conferences member stop-play room 11",
		"voice conferences member play room 11 --url https://example.com/a.mp3",
		"voice conferences member speak room 11 --text hi", "voice conferences member stop-speak room 11",
		"voice multiparty end room --yes", "voice multiparty participant kick room 11 --yes",
		"voice multiparty participant add room --from +14155550100 --to +14155550101 --yes",
		"voice multiparty participant mute room 11", "voice multiparty participant unmute room 11",
		"voice multiparty participant hold room 11", "voice multiparty participant unhold room 11",
		"numbers update +14155550100 --alias qa", "numbers release +14155550100 --yes",
		"account update --name qa", "account applications update 31000000000000001 --app-name qa",
		"account applications delete 31000000000000001 --yes", "account subaccounts update SAFAKESUBACCOUNT0001 --name qa",
		"account subaccounts delete SAFAKESUBACCOUNT0001 --yes", "voice recordings delete UUID --yes",
		"voice endpoints update 12345678 --alias qa", "voice endpoints delete 12345678 --yes",
		"messaging sms powerpacks update UUID --name qa", "messaging sms powerpacks delete UUID --yes",
		"messaging sms powerpacks numbers add UUID +14155550100",
		"messaging sms powerpacks numbers remove UUID +14155550100 --yes",
		"messaging sms 10dlc links delete CFAKE01 +14155550100 --yes",
		"messaging sms 10dlc campaigns update CFAKE01 --description qa --yes", "numbers compliance delete UUID --yes",
		"numbers masking sessions delete UUID --yes",
		"agents publish UUID", "agents pause UUID", "agents resume UUID",
		"agents update UUID --name qa", "agents delete UUID --yes",
	} {
		args := strings.Fields(strings.ReplaceAll(line, "UUID", "d1e2f3a4-0000-4000-8000-000000000001"))
		err, stdout, _ := execCmd(t, append(args, "-o", "json")...)
		var got struct{ Data map[string]any }
		if err != nil || json.Unmarshal([]byte(stdout), &got) != nil || len(got.Data) == 0 {
			t.Errorf("%s -o json: err %v, stdout %q; want a JSON result", line, err, stdout)
		}
	}
}
