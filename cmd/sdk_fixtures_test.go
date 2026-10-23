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

// The json names of the Go SDK's request params: all a command may send.
const (
	sdkBrandCreate    = "brand_alias brand_type profile_uuid secondary_vetting url method" // BrandCreationParams
	sdkCampaignUpdate = "description sample1 sample2 sample3 sample4 sample5 message_flow help_message help_keywords " +
		"optin_message optin_keywords optout_message optout_keywords terms_and_conditions_link privacy_policy_link" // CampaignUpdateParams
	sdkCampaignCreate = sdkCampaignUpdate + " brand_id campaign_alias vertical usecase sub_usecases embedded_link embedded_phone " +
		"age_gated direct_lending affiliate_marketing subscriber_optin subscriber_optout subscriber_help url method" // CampaignCreationParams
	sdkNumberLink     = "numbers url method" // CampaignNumberLinkParams
	sdkTollfreeCreate = "profile_uuid number usecase usecase_summary message_sample optin_image_url optin_type volume " +
		"additional_information extra_data callback_url callback_method terms_and_conditions_link privacy_policy_link " +
		"optin_message help_message" // TollfreeVerificationCreateParams
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
		{"10dlc brands create --profile-uuid P1 --alias A1 --yes", `{"api_id":"x","brand_id":"B1","message":"ok"}`, "POST 10dlc/Brand/",
			sdkBrandCreate, map[string]any{"profile_uuid": "P1", "brand_alias": "A1", "brand_type": "STARTER"}, []string{"B1"}},
		{"10dlc campaigns create --alias a --brand-id B1 --vertical TECHNOLOGY --usecase MIXED --sub-usecases 2FA,MARKETING " +
			"--description d --message-flow f --sample-message-1 s1 --sample-message-2 s2 --help-message h --opt-out-message o " +
			"--subscriber-optin --subscriber-optout --subscriber-help --yes", `{"api_id":"x","campaign_id":"C1","message":"ok"}`,
			"POST 10dlc/Campaign/", sdkCampaignCreate, map[string]any{"sample1": "s1", "sample2": "s2", "vertical": "TECHNOLOGY",
				"optin_keywords": "START", "optout_message": "o", "subscriber_help": true, "sub_usecases": []any{"2FA", "MARKETING"}}, []string{"C1"}},
		{"10dlc campaigns update CY5NVUA --sample-message-1 s1", "campaignUpdateResponse.json", "POST 10dlc/Campaign/CY5NVUA/",
			sdkCampaignUpdate, map[string]any{"sample1": "s1"}, []string{"FAILED"}},
		{"10dlc links list --campaign-id CY5NVUA", "campaignNumbersGetResponse.json", "GET 10dlc/Campaign/CY5NVUA/Number/", "", nil, []string{"14155550101", "COMPLETED"}},
		{"10dlc links list --campaign-id CY5NVUA --number 14155550100", "campaignNumbersGetResponse.json", "GET 10dlc/Campaign/CY5NVUA/Number/14155550100/", "", nil, nil},
		{"10dlc links create --campaign-id C1 --number 14155550100", `{"api_id":"x","message":"ok"}`, "POST 10dlc/Campaign/C1/Number/",
			sdkNumberLink, map[string]any{"numbers": []any{"14155550100"}}, nil},
		{"10dlc links delete C1 14155550100 --yes", `{"api_id":"x","message":"ok"}`, "DELETE 10dlc/Campaign/C1/Number/14155550100/", "", nil, nil},
		{"tollfree get 8163b5f2-7955-498b-780f-b5e6f1062396", "tollFreeRequestVerificationGetResponse.json",
			"GET TollfreeVerification/8163b5f2-7955-498b-780f-b5e6f1062396/", "", nil, []string{"18005550100", "MOBILE_QR_CODE", "Demo Testing Sanity"}},
		{"tollfree list", "tollFreeRequestVerificationListResponse.json", "GET TollfreeVerification/", "", nil, []string{"96ec880d-b7ee-4eb5-7c9a-3ff28826e77c", "18005550100"}},
		{"tollfree submit --profile-uuid P1 --number 18005550100 --use-case 2FA --use-case-summary s --production-message-content m " +
			"--opt-in-type WEB_FORM --opt-in-image-url https://example.com/o --message-volume 1000 " +
			"--terms-and-conditions-link https://example.com/t --privacy-policy-link https://example.com/p",
			"tollFreeRequestVerificationCreateResponse.json", "POST TollfreeVerification/", sdkTollfreeCreate, map[string]any{"number": "18005550100",
				"usecase": "2FA", "message_sample": "m", "optin_type": "WEB_FORM", "volume": "1000"}, []string{"f19c4773-4ae6-4b75-92ea-9cf3ea4227d6"}},
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

// Neither the SDK nor the API reference has a brand update: the command says so
// and sends nothing.
func TestBrandsUpdate_sendsNothing(t *testing.T) {
	setFakeCreds(t)
	sent := serveFixture(t, `{}`)
	err, _, _ := execCmd(t, "messaging", "sms", "10dlc", "brands", "update", "B1", "--email", "a@example.com")
	if calls, _ := sent(); err == nil || !strings.Contains(err.Error(), "no endpoint") || len(calls) != 0 {
		t.Errorf("got %v after %d requests, want a refusal and none", err, len(calls))
	}
}
