package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// A participant ID is escaped like any path part: a # cut the URL to the whole
// participant list.
func TestMPCParticipantKick_escapesTheID(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 200, `{}`)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: srv.Client()}
	t.Cleanup(func() { clientForTest = nil })
	err, _, _ := execCmd(t, "voice", "multiparty", "participant", "kick", "room", "#", "--yes")
	if got := hits(); err != nil || len(got) != 1 || got[0] != "DELETE /v1/Account/MAFAKEFORTEST/MultiPartyCall/name_room/Participant/#/" {
		t.Errorf("err %v, sent %q; want one DELETE of participant #", err, got)
	}
}

// create must send nothing and point at `participant add`, and nothing may stop
// an old invocation first: not a missing credential, --yes or --name, nor one
// of its old flags.
func TestMPCCreate_pointsToParticipantAdd(t *testing.T) {
	setEmptyHome(t)
	srv, hits := startCapturingHTTPServer(t, 200, `{}`)
	addPath := findCmd(t, "voice", "multiparty", "participant", "add").CommandPath()

	cases := []struct {
		name   string
		args   []string
		client *api.Client
	}{
		{
			// Any request would reach srv: the client points there and --yes is set.
			name:   "old flags with --yes",
			args:   []string{"voice", "multiparty", "create", "--name", "standup", "--max-participants", "5", "--record", "--yes"},
			client: &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: srv.Client()},
		},
		{
			name: "bare via the mpc alias, logged out",
			args: []string{"voice", "mpc", "create"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clientForTest = tc.client
			t.Cleanup(func() { clientForTest = nil })

			before := len(hits())
			err, _, _ := execCmd(t, tc.args...)
			if sent := hits()[before:]; len(sent) > 0 {
				t.Errorf("create must not call the API, sent %v", sent)
			}
			var ce *clierr.Error
			if !errors.As(err, &ce) {
				t.Fatalf("want a *clierr.Error, got %T: %v", err, err)
			}
			if ce.Code != clierr.CodeBadInput || ce.ExitCode() != ExitUserError {
				t.Errorf("got %s (exit %d), want %s (exit %d)", ce.Code, ce.ExitCode(), clierr.CodeBadInput, ExitUserError)
			}
			if !strings.Contains(ce.Hint, addPath) {
				t.Errorf("hint should point at `%s`, got %q", addPath, ce.Hint)
			}
		})
	}
}

// Each verb sends the request the API documents for it.
func TestParticipantVerbs_sendWhatTheAPITakes(t *testing.T) {
	setFakeCreds(t)
	for args, want := range map[string]string{
		"multiparty participant mute room 11":                             `/MultiPartyCall/name_room/Participant/11/ body: { "mute": true }`,
		"multiparty participant unmute room 11":                           `/MultiPartyCall/name_room/Participant/11/ body: { "mute": false }`,
		"multiparty participant hold room 11":                             `/MultiPartyCall/name_room/Participant/11/ body: { "hold": true }`,
		"multiparty participant unhold room 11":                           `/MultiPartyCall/name_room/Participant/11/ body: { "hold": false }`,
		"conferences member kick room 11":                                 "/Conference/room/Member/11/Kick/ body: {}",
		"conferences member play room 11 --url https://example.com/a.mp3": `/Conference/room/Member/11/Play/ body: { "url": "https://example.com/a.mp3" }`,
	} {
		err, _, stderr := execCmd(t, append(strings.Fields("voice "+args), "--yes", "--dry-run")...)
		if got := strings.Join(strings.Fields(stderr), " "); err != nil || !strings.HasPrefix(got, "[dry-run] POST ") || !strings.HasSuffix(got, want) {
			t.Errorf("%s: %v, sent %q; want a POST to ...%s", args, err, got, want)
		}
	}
}

// A blank call ID is refused, not sent as the call named "name_".
func TestMPCEnd_refusesABlankID(t *testing.T) {
	setFakeCreds(t)
	srv, hits := startCapturingHTTPServer(t, 200, `{}`)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: srv.Client()}
	t.Cleanup(func() { clientForTest = nil })
	if err, _, _ := execCmd(t, "voice", "multiparty", "end", " ", "--yes"); !strings.Contains(fmt.Sprint(err), "BAD_INPUT") || len(hits()) != 0 {
		t.Errorf("got %v after %q, want BAD_INPUT before any request", err, hits())
	}
}
