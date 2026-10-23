package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/plivo/plivo-cli/internal/api"
)

// serveFixture answers every request with a response from the Go SDK's
// fixtures in testdata/plivo-go, or with fixture itself when it is not a file
// name, and returns the requests sent: "METHOD /path" and the JSON body.
func serveFixture(t *testing.T, fixture string) func() (calls []string, bodies []map[string]any) {
	t.Helper()
	resp := []byte(fixture)
	if strings.HasSuffix(fixture, ".json") {
		var err error
		if resp, err = os.ReadFile(filepath.Join("testdata", "plivo-go", fixture)); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var calls []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls, bodies = append(calls, r.Method+" "+r.URL.Path), append(bodies, body)
		mu.Unlock()
		_, _ = w.Write(resp)
	}))
	t.Cleanup(srv.Close)
	clientForTest = &api.Client{BaseURL: srv.URL, AuthID: "MAFAKEFORTEST", AuthToken: "tok", HTTP: &http.Client{}}
	t.Cleanup(func() { clientForTest = nil })
	return func() ([]string, []map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls), slices.Clone(bodies)
	}
}

// Each command sends what the SDK sends and reads what the SDK's fixtures hold.
func TestRegistrationCommands_matchTheSDK(t *testing.T) {
	for _, tc := range []struct {
		args    string         // after "messaging sms", split on spaces
		fixture string         // the response
		call    string         // the request, under /v1/Account/{auth_id}/
		keys    string         // the json names of the SDK's params; "" for no body
		body    map[string]any // values the body must carry
		out     []string       // in the -o table output
	}{
		{"10dlc brands get BRPXS6E", "brandGetResponse.json", "GET 10dlc/Brand/BRPXS6E/", "", nil, []string{"BRPXS6E", "COMPLETED"}},
		{"10dlc brands list", "brandListResponse.json", "GET 10dlc/Brand/", "", nil, []string{"ABC Inc.123", "COMPLETED"}},
		{"10dlc campaigns get CY5NVUA", "campaignGetResponse.json", "GET 10dlc/Campaign/CY5NVUA/", "", nil, []string{"BOZ5T2X", "2FA,MARKETING", "ACTIVE"}},
		{"10dlc campaigns list", "campaignListResponse.json", "GET 10dlc/Campaign/", "", nil, []string{"CUSDYQ0", "FAILED"}},
	} {
		t.Run(tc.args, func(t *testing.T) {
			setFakeCreds(t)
			sent := serveFixture(t, tc.fixture)
			err, stdout, stderr := execCmd(t, append(strings.Fields("messaging sms "+tc.args), "-o", "table")...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			calls, bodies := sent()
			if want := strings.Replace(tc.call, " ", " /v1/Account/MAFAKEFORTEST/", 1); !slices.Equal(calls, []string{want}) {
				t.Fatalf("sent %v, want %s", calls, want)
			}
			for k := range bodies[0] {
				if !slices.Contains(strings.Fields(tc.keys), k) {
					t.Errorf("sent %q, which the SDK does not send", k)
				}
			}
			for k, v := range tc.body {
				if !reflect.DeepEqual(bodies[0][k], v) {
					t.Errorf("%s = %#v, want %#v", k, bodies[0][k], v)
				}
			}
			for _, s := range tc.out {
				if !strings.Contains(stdout+stderr, s) {
					t.Errorf("no %q in the output:\n%s%s", s, stdout, stderr)
				}
			}
		})
	}
}
