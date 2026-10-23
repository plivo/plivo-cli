package cmd

import (
	"errors"
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
