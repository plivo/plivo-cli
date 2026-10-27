package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The SDK oracle holds the CLI to the Plivo Go SDK, kept in testdata/sdk:
// its fixtures are real responses, and requests.json lists the parameters
// each of its requests sends (make sdk-params SDK=<checkout> rewrites it).
// What the SDK lacks, qaDocsRequests and qaConfirmed take from the API docs.
// What it finds today is listed in qaKnownGaps, under a pending finding;
// anything else fails.

// qaFixtures maps each get and list command to the SDK fixture of its
// response.
var qaFixtures = map[string]string{
	"account applications get":           "applicationGetResponse",
	"account applications list":          "applicationListResponse",
	"account get":                        "accountGetResponse",
	"account subaccounts get":            "subaccountGetResponse",
	"account subaccounts list":           "subaccountListResponse",
	"messaging get":                      "messageGetResponse",
	"messaging mms list":                 "messageListResponse",
	"messaging sms 10dlc brands get":     "brandGetResponse",
	"messaging sms 10dlc brands list":    "brandListResponse",
	"messaging sms 10dlc campaigns get":  "campaignGetResponse",
	"messaging sms 10dlc campaigns list": "campaignListResponse",
	"messaging sms 10dlc links list":     "campaignNumbersGetResponse",
	"messaging sms list":                 "messageListResponse",
	"messaging sms tollfree get":         "tollFreeRequestVerificationGetResponse",
	"messaging sms tollfree list":        "tollFreeRequestVerificationListResponse",
	"messaging whatsapp list":            "messageListResponse",
	"numbers compliance list":            "phoneNumberComplianceListResponse",
	"numbers get":                        "numberGetResponse",
	"numbers list":                       "numberListResponse",
	"numbers masking sessions get":       "getMaskingSessionResponse",
	"numbers masking sessions list":      "listMaskingSessionResponse",
	"verify sessions get":                "verifySessionGetResponse",
	"verify sessions list":               "verifySessionListResponse",
	"voice calls get":                    "callGetResponse",
	"voice calls list":                   "callListResponse",
	"voice calls streams list":           "liveCallStreamGetAllResponse",
	"voice conferences get":              "conferenceGetResponse",
	"voice conferences list":             "conferenceListResponse",
	"voice endpoints get":                "endpointGetResponse",
	"voice endpoints list":               "endpointListResponse",
	"voice multiparty get":               "mPCGetResponse",
	"voice multiparty list":              "mPCListResponse",
	"voice multiparty participant list":  "mPCListParticipantsResponse",
	"voice recordings get":               "recordingGetResponse",
	"voice recordings list":              "recordingListResponse",
}

// qaOracleOwners names the open finding about a command, for what the
// oracle finds in it: its response for a get or list, its requests for the
// rest. sdk/fields, about every list type, applies to all of them.
var qaOracleOwners = map[string]string{
	"account applications get":             "r3/L9",
	"account applications list":            "r3/L9",
	"account subaccounts get":              "r3/L9",
	"account subaccounts list":             "r3/L9",
	"messaging sms 10dlc brands create":    "r3/D1",
	"messaging sms 10dlc brands get":       "r3/L4",
	"messaging sms 10dlc brands list":      "r3/L4",
	"messaging sms 10dlc brands update":    "r3/D1",
	"messaging sms 10dlc campaigns create": "r3/D2",
	"messaging sms 10dlc campaigns get":    "r3/L4",
	"messaging sms 10dlc campaigns list":   "r3/L4",
	"messaging sms 10dlc links create":     "r3/links",
	"messaging sms 10dlc links delete":     "r3/links",
	"messaging sms 10dlc links list":       "r3/links",
	"messaging sms tollfree get":           "r3/D3",
	"messaging sms tollfree list":          "r3/D3",
	"messaging sms tollfree submit":        "r3/D3",
	"numbers get":                          "sdk/numbers",
	"numbers list":                         "sdk/numbers",
	"numbers masking sessions create":      "sdk/masking",
	"numbers masking sessions get":         "sdk/masking",
	"numbers masking sessions list":        "sdk/masking",
	"verify sessions get":                  "sdk/verify",
	"verify sessions list":                 "sdk/verify",
	"verify sessions validate":             "sdk/verify",
	"voice calls record":                   "sdk/record",
	"voice calls streams list":             "sdk/streams",
	"voice calls streams start":            "r3/D7a",
	"voice conferences member play":        "r3/D6",
	"voice conferences record":             "sdk/record",
	"voice endpoints get":                  "r3/L9",
	"voice endpoints list":                 "r3/L9",
	"voice multiparty get":                 "sdk/multiparty",
	"voice multiparty list":                "sdk/multiparty",
	"voice multiparty participant hold":    "r3/D4",
	"voice multiparty participant list":    "sdk/multiparty",
	"voice multiparty participant mute":    "r3/D4",
	"voice multiparty participant unhold":  "r3/D4",
	"voice multiparty participant unmute":  "r3/D4",
}

// qaKnownGaps are what the oracle finds today, by the finding that reports
// it: the name of a field --schema lists that the response lacks, or of a
// parameter the references do not take; "*" covers anything in that
// finding's commands. Once a finding leaves qaPending its gaps stop counting,
// so what the oracle still finds fails, unless sdk/fields covers it.
var qaKnownGaps = map[string][]string{
	"r3/D1":          {"*"},
	"r3/D2":          {"*"},
	"r3/D3":          {"*"},
	"r3/D4":          {"*"},
	"r3/D6":          {"loop", "mix", "urls"},
	"r3/D7a":         {"stream_status_callback_url"},
	"r3/L4":          {"*"},
	"r3/L9":          {"app_id", "created_on", "default_number_app", "modified_on"},
	"r3/links":       {"*"},
	"sdk/fields":     {"api_id", "resource_uri", "text", "total_count"},
	"sdk/masking":    {"*"},
	"sdk/multiparty": {"billing_type", "caller_name", "created_at", "direction", "end_at", "from", "limit", "muted", "offset", "to"},
	"sdk/numbers":    {"app_id", "country", "type"},
	"sdk/record":     {"both_legs", "transcribe"},
	"sdk/streams":    {"resource_uri", "stream_url"},
	"sdk/verify":     {"*"},
}

// qaGapOwner is the first pending finding of owners whose known gaps hold
// name ("" for a whole request or response), or "" when none does.
func qaGapOwner(name string, owners ...string) string {
	for _, owner := range owners {
		if qaPending[owner] == "" {
			continue
		}
		for _, gap := range qaKnownGaps[owner] {
			if gap == name || gap == "*" {
				return owner
			}
		}
	}
	return ""
}

// qaConfirmed are response fields an SDK fixture lacks that another source
// confirms: the API docs, QA live, or for 10DLC and toll-free, which were
// rebuilt from it, the SDK's own response structs. By command prefix; a
// field below one of them is covered too.
var qaConfirmed = map[string][]string{
	"account applications": {"log_incoming_messages"},              // docs
	"voice calls":          {"hangup_cause_name", "hangup_source"}, // docs
	"voice calls streams":  {"audio_track", "bidirectional"},       // docs
	"numbers get":          {"mms_enabled"},                        // docs
	"numbers list":         {"mms_enabled"},
	"voice endpoints":      {"password"}, // QA saw the API return it (r3/L5)
	"numbers compliance": {"callback_url", "callback_method", "rejection_reason", // docs; the rest with expand
		"end_user", "documents", "linked_numbers"},
	"messaging sms 10dlc brands": {"meta", "brand_alias", "profile_uuid", "company_name", "entity_type", "vertical",
		"website", "ein", "stock_symbol", "stock_exchange", "vetting_status", "vetting_score", "authorized_contact"},
	"messaging sms 10dlc campaigns": {"meta", "campaign_alias", "vertical", "description", "campaign_source",
		"message_flow", "help_message", "help_keywords", "optin_message", "optin_keywords", "optout_message",
		"optout_keywords", "terms_and_conditions_link", "privacy_policy_link", "campaign_attributes", "error_reason"},
	"messaging sms tollfree": {"terms_and_conditions_link", "privacy_policy_link", "optin_message", "help_message"},
}

// qaIsConfirmed reports whether a field of cmd's output, by its names, is in
// qaConfirmed.
func qaIsConfirmed(cmd string, names []string) bool {
	for prefix, fields := range qaConfirmed {
		if strings.HasPrefix(cmd, prefix) && slices.ContainsFunc(names, func(n string) bool { return slices.Contains(fields, n) }) {
			return true
		}
	}
	return false
}

// qaFieldNames splits an output field path into its names.
func qaFieldNames(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '.' || r == '[' || r == ']' })
}

// qaFixture reads an SDK fixture: from testdata/sdk, or testdata/plivo-go,
// where the 10DLC and toll-free commands keep theirs once #171 is in. The
// two overlap: after #171 merges, testdata/sdk moves into testdata/plivo-go,
// under one LICENSE.
func qaFixture(name string) (map[string]any, bool) {
	for _, dir := range []string{"sdk", "plivo-go"} {
		b, err := os.ReadFile(filepath.Join("testdata", dir, name+".json"))
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(b, &doc) == nil {
			return doc, true
		}
	}
	return nil, false
}

// qaJSONPaths lists every path in doc the way outputFields names them:
// "a.b" for a nested field, "a[]" and "a[].b" inside an array.
func qaJSONPaths(v any, prefix string, out map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			out[prefix+k] = true
			qaJSONPaths(child, prefix+k+".", out)
		}
	case []any:
		arr := strings.TrimSuffix(prefix, ".") + "[]"
		out[arr] = true
		for _, child := range v {
			qaJSONPaths(child, arr+".", out)
		}
	}
}

// TestQA_sdkSchemaPaths checks that every output field --schema lists for a
// command is in the SDK's fixture of its response.
func TestQA_sdkSchemaPaths(t *testing.T) {
	for _, cmd := range qaSorted(qaFixtures) {
		t.Run(cmd, func(t *testing.T) {
			owner := qaOracleOwners[cmd]
			doc, ok := qaFixture(qaFixtures[cmd])
			if !ok {
				qaVerdict(t, map[string][]string{qaGapOwner("", owner): {"no fixture " + qaFixtures[cmd]}})
				return
			}
			have := map[string]bool{"api_id": true} // every response has one
			qaJSONPaths(doc, "", have)
			missing := map[string][]string{}
			for _, f := range outputFields(outputTypes["plivo "+cmd]) {
				names := qaFieldNames(f.Path)
				if have[f.Path] || qaIsConfirmed(cmd, names) {
					continue
				}
				o := qaGapOwner(names[len(names)-1], owner, "sdk/fields")
				missing[o] = append(missing[o], f.Path)
			}
			problems := map[string][]string{}
			for o, paths := range missing {
				problems[o] = []string{fmt.Sprintf("--schema lists %s, which the SDK's %s lacks", strings.Join(paths, ", "), qaFixtures[cmd])}
			}
			qaVerdict(t, problems)
		})
	}
}

// TestQA_sdkDecoding answers each get and list command with its SDK fixture:
// it must succeed in every format, and its table must show the record.
func TestQA_sdkDecoding(t *testing.T) {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	t.Cleanup(func() { srv.handler = nil })
	samples := map[string]qaSample{}
	for _, s := range qaAPISamples() {
		samples[s.path] = s
	}
	for _, cmd := range qaSorted(qaFixtures) {
		t.Run(cmd, func(t *testing.T) {
			owner := qaGapOwner("", qaOracleOwners[cmd])
			doc, ok := qaFixture(qaFixtures[cmd])
			s, sampled := samples[cmd]
			if !ok || !sampled {
				qaVerdict(t, map[string][]string{owner: {"no fixture or no sample"}})
				return
			}
			body, _ := json.Marshal(doc)
			srv.handler = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method != http.MethodGet {
					return false
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
				return true
			}
			var p []string
			for _, format := range []string{"json", "table", "jsonl", "yaml", "csv"} {
				r := srv.run(t, qaStdin, append(s.argv, "-o", format)...)
				if r.exit != 0 {
					p = append(p, fmt.Sprintf("-o %s: exit %d: %v", format, r.exit, r.err))
					continue
				}
				if format == "table" && !qaShowsRecord(r.stdout, doc) {
					p = append(p, "-o table shows none of the record's values:\n"+qaClip(r.stdout))
				}
			}
			if len(p) > 0 {
				qaVerdict(t, map[string][]string{owner: p})
			}
		})
	}
}

// qaShowsRecord reports whether table holds one of the text values of the
// fixture's record: the object itself, an object it nests the record in, or
// the first row of its list.
func qaShowsRecord(table string, doc map[string]any) bool {
	var texts []string
	add := func(obj map[string]any) {
		for k, v := range obj {
			if s, ok := v.(string); ok && len(s) >= 4 && k != "api_id" {
				texts = append(texts, s)
			}
		}
	}
	add(doc)
	for _, v := range doc {
		switch v := v.(type) {
		case map[string]any:
			add(v)
		case []any:
			if len(v) == 0 {
				continue
			}
			switch row := v[0].(type) {
			case map[string]any:
				add(row)
			case string:
				texts = append(texts, row)
			}
		}
	}
	return slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(table, s) })
}

// qaSDKRequest is one request of requests.json or qaDocsRequests: its
// method, its path below the account ({} for a segment) and the names of
// its parameters (nil when unknown).
type qaSDKRequest struct {
	Method string   `json:"method"`
	Path   string   `json:"path"`
	Keys   []string `json:"keys"`
}

// TestQA_sdkRequestKeys runs every command and holds each request it sends to
// the SDK's request with that method and path (or the docs', in
// qaDocsRequests): it may send only parameters they take. A query filter
// such as end_time__gte counts as its field, end_time.
func TestQA_sdkRequestKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "sdk", "requests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sdk struct{ Requests []qaSDKRequest }
	if err := json.Unmarshal(raw, &sdk); err != nil {
		t.Fatal(err)
	}
	reference := qaMergeRequests(append(sdk.Requests, qaDocsRequests...))
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	for _, s := range qaAPISamples() {
		t.Run(s.path, func(t *testing.T) {
			r := srv.run(t, qaStdin, append(s.argv, "--yes")...)
			qaControl(t, s, r)
			problems := map[string][]string{}
			for _, req := range r.reqs {
				if strings.HasPrefix(req.Path, "/") || qaUnreferenced(req.Path) {
					continue // not an account API, or one neither reference covers
				}
				for _, p := range qaKeyProblems(req, reference) {
					owner := qaGapOwner(p.name, qaOracleOwners[s.path])
					problems[owner] = append(problems[owner], p.text)
				}
			}
			qaVerdict(t, problems)
		})
	}
}

// qaKeyProblem is a parameter the reference does not take (name), or a
// request it does not have (name "").
type qaKeyProblem struct{ name, text string }

// qaKeyProblems holds one request to the reference request it matches.
func qaKeyProblems(req qaReq, reference []qaSDKRequest) []qaKeyProblem {
	path := strings.Trim(req.Path, "/")
	var keys []string
	if req.Method == http.MethodGet {
		q, _ := url.ParseQuery(req.Query)
		for k := range q {
			keys = append(keys, k)
		}
	} else {
		for k := range req.Body {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, ref := range reference {
		if ref.Method != req.Method || !qaPathMatches(ref.Path, path) {
			continue
		}
		var p []qaKeyProblem
		for _, k := range keys {
			field, _, _ := strings.Cut(k, "__")
			if ref.Keys != nil && !slices.Contains(ref.Keys, k) && !slices.Contains(ref.Keys, field) {
				p = append(p, qaKeyProblem{k, fmt.Sprintf("%s %s sends %s, which the reference does not take (%s)", req.Method, path, k, strings.Join(ref.Keys, ", "))})
			}
		}
		return p
	}
	return []qaKeyProblem{{"", fmt.Sprintf("the reference has no %s %s", req.Method, path)}}
}

// qaPathMatches matches a path against a reference path, where {} stands for
// one segment.
func qaPathMatches(ref, path string) bool {
	rs, ps := strings.Split(ref, "/"), strings.Split(path, "/")
	if len(rs) != len(ps) {
		return false
	}
	for i := range rs {
		if rs[i] != "{}" && rs[i] != ps[i] {
			return false
		}
	}
	return true
}

// qaMergeRequests joins the references' entries for the same request.
func qaMergeRequests(all []qaSDKRequest) []qaSDKRequest {
	var out []qaSDKRequest
	at := map[string]int{}
	for _, r := range all {
		key := r.Method + " " + r.Path
		i, seen := at[key]
		switch {
		case !seen:
			at[key] = len(out)
			out = append(out, r)
		case out[i].Keys != nil && r.Keys != nil:
			out[i].Keys = slices.Concat(out[i].Keys, r.Keys)
		}
	}
	return out
}

// qaUnreferenced reports the APIs neither reference describes: CX agents,
// usage, CNAM lookup, and the retired Powerpacks.
func qaUnreferenced(path string) bool {
	for _, prefix := range []string{"AgentFlow", "UsageSummary/", "CnamLookup/", "Powerpack/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// qaDocsRequests are requests as the API docs define them where the SDK has
// none or takes fewer parameters: SIP trunking (sip-trunking/api), Speak's
// voice and DTMF's leg (voice/api/calls), and the compliance update
// (numbers/compliance, a form).
var qaDocsRequests = func() []qaSDKRequest {
	trunk := []string{"name", "trunk_direction", "trunk_status", "secure", "ipacl_uuid", "credential_uuid", "primary_uri_uuid", "fallback_uri_uuid"}
	credential := []string{"name", "username", "password"}
	acl := []string{"name", "ip_addresses"}
	uri := []string{"name", "uri", "authentication_needed", "username", "password"}
	calls := []string{"from_number", "to_number", "call_direction", "hangup_cause_code", "hangup_source", "stir_verification", "bill_duration", "end_time"}
	page, none := []string{"limit", "offset"}, []string{}
	reqs := []qaSDKRequest{
		{"GET", "Zentrunk/Trunk", slices.Concat(trunk[1:], page)},
		{"POST", "Zentrunk/Trunk", trunk},
		{"POST", "Zentrunk/Trunk/{}", trunk},
		{"POST", "Zentrunk/Credential", credential},
		{"POST", "Zentrunk/Credential/{}", credential},
		{"POST", "Zentrunk/IPAccessControlList", acl},
		{"POST", "Zentrunk/IPAccessControlList/{}", acl},
		{"POST", "Zentrunk/URI", uri},
		{"POST", "Zentrunk/URI/{}", uri},
		{"GET", "Zentrunk/Call", slices.Concat(calls, page)},
		{"GET", "Zentrunk/Call/{}", none},
		{"GET", "Zentrunk/Call/{}/Insights", none},
		{"POST", "Call/{}/Speak", []string{"voice"}},
		{"POST", "Call/{}/DTMF", []string{"leg"}},
		{"PATCH", "PhoneNumber/Compliance/{}", none},
	}
	for _, r := range []string{"Credential", "IPAccessControlList", "URI"} {
		reqs = append(reqs, qaSDKRequest{"GET", "Zentrunk/" + r, page})
	}
	for _, r := range []string{"Trunk", "Credential", "IPAccessControlList", "URI"} {
		reqs = append(reqs, qaSDKRequest{"GET", "Zentrunk/" + r + "/{}", none}, qaSDKRequest{"DELETE", "Zentrunk/" + r + "/{}", none})
	}
	return reqs
}()

func qaSorted[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
