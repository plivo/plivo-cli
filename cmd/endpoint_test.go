package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

const epAgent = "0e2fa000-1111-4222-8333-444455556666"

// create --agent creates the endpoint with no app_id, then links the new
// endpoint; a failed link names it and the command that links it.
func TestEndpointsCreate_agentLinksTheNewEndpoint(t *testing.T) {
	setFakeCreds(t)
	argv := qaArgs("voice endpoints create --username qauser01 --password QaPass1! --alias qa --agent " + epAgent)
	created := `{"api_id": "qa", "endpoint_id": "123456", "username": "qauser01", "message": "created"}`

	r := newQAServer(t, map[string]string{"POST Endpoint/": created}).run(t, "", argv...)
	if r.err != nil || len(r.reqs) != 2 || r.reqs[0].Body["app_id"] != nil ||
		r.reqs[1].String() != "POST AgentFlow/"+epAgent+"/Endpoint/" || r.reqs[1].Body["endpoint_id"] != "123456" {
		t.Fatalf("err %v, requests %v", r.err, r.reqs)
	}

	r = newQAServer(t, map[string]string{"POST Endpoint/": created, "POST AgentFlow/*": `404 {"error": "Agent not found"}`}).run(t, "", argv...)
	var ce *clierr.Error
	if !errors.As(r.err, &ce) || ce.Code != clierr.CodeResourceNotFound || !strings.Contains(ce.Message, "Endpoint 123456 was created") ||
		!strings.Contains(ce.Hint, "`plivo voice endpoints update 123456 --agent "+epAgent+"`") {
		t.Errorf("a failed link: %v\n%s", r.err, r.stderr)
	}
}

// --agent is refused before any request: with --app-id, the field it sets,
// or when it is no agent UUID.
func TestEndpoints_agentRefusedBeforeAnyRequest(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, nil)
	for _, line := range []string{
		"create --username qauser01 --password QaPass1! --alias qa --app-id 1 --agent " + epAgent,
		"update 123456 --app-id 1 --agent " + epAgent,
		"update 123456 --agent 123456",
	} {
		var ce *clierr.Error
		if r := srv.run(t, "", qaArgs("voice endpoints "+line)...); !errors.As(r.err, &ce) || ce.Code != clierr.CodeBadFlag || len(r.reqs) != 0 {
			t.Errorf("%s: err %v, requests %v; want BAD_FLAG and none", line, r.err, r.reqs)
		}
	}
}
