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

// The SDK oracle holds the CLI to the Plivo Go SDK, kept in testdata/plivo-go:
// its fixtures are real responses, and requests.json lists the parameters
// each of its requests sends (make sdk-params SDK=<checkout> rewrites it).
// What the SDK lacks, qaDocsRequests and qaConfirmed take from the API docs.
// What it finds today is listed in qaKnownGaps, under a pending finding;
// anything else fails, and so does a known gap it no longer finds.

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
// rest.
var qaOracleOwners = map[string]string{}

// qaKnownGaps are what the oracle finds today, by the finding that reports
// it: the name of a field --schema lists that the response lacks, or of a
// parameter the references do not take; "*" covers anything in that
// finding's commands. A gap counts only while its finding is pending, and
// each must still turn up: the fix that clears one deletes it.
var qaKnownGaps = map[string][]string{}

// qaGapOwner is the first pending finding of owners whose known gaps hold
// name ("" for a whole request or response), or "" when none does. It marks
// the gap found, in qaGapsFound.
func qaGapOwner(name string, owners ...string) string {
	for _, owner := range owners {
		if qaPending[owner] == "" {
			continue
		}
		for _, gap := range qaKnownGaps[owner] {
			if gap == name || gap == "*" {
				qaGapsFound[[2]string{owner, gap}] = true
				return owner
			}
		}
	}
	return ""
}

// qaGapsFound are the known gaps, as {owner, gap}, that TestQA_sdk found.
var qaGapsFound = map[[2]string]bool{}

// TestQA_sdk holds the CLI to the SDK: --schema paths, decoding and request
// keys. When all of them have run, every gap in qaKnownGaps must have turned
// up under a pending finding.
func TestQA_sdk(t *testing.T) {
	clear(qaGapsFound)
	ran := 0
	t.Run("schema", func(t *testing.T) { ran += qaSDKSchemaPaths(t) })
	t.Run("decoding", func(t *testing.T) { ran += qaSDKDecoding(t) })
	t.Run("requests", func(t *testing.T) { ran += qaSDKRequestKeys(t) })
	if ran < 2*len(qaFixtures)+len(qaAPISamples()) {
		return // -run left some out, so a gap may not have been looked for
	}
	for _, owner := range qaSorted(qaKnownGaps) {
		if qaPending[owner] == "" {
			t.Errorf("qaKnownGaps lists %s, which is not pending: delete its entry", owner)
			continue
		}
		for _, gap := range qaKnownGaps[owner] {
			if !qaGapsFound[[2]string{owner, gap}] {
				t.Errorf("qaKnownGaps lists %q under %s, which the oracle no longer finds: delete it", gap, owner)
			}
		}
	}
}

// qaConfirmed are response fields an SDK fixture lacks that another source
// confirms: the API docs, QA live, or for 10DLC and toll-free, which were
// rebuilt from it, the SDK's own response structs. By command prefix; a
// field below one of them is covered too.
var qaConfirmed = map[string][]string{
	"account applications": {"log_incoming_messages"},              // docs
	"voice calls":          {"hangup_cause_name", "hangup_source"}, // docs
	"voice calls streams":  {"audio_track", "bidirectional"},       // docs
	"numbers get":          {"mms_enabled", "country", "type"},     // docs; country and type QA saw live
	"numbers list":         {"mms_enabled", "country", "type"},
	"voice endpoints":      {"password"}, // QA saw the API return it (r3/L5)
	"numbers compliance": {"callback_url", "callback_method", "rejection_reason", // docs; the rest with expand
		"end_user", "documents", "linked_numbers"},
	"messaging sms 10dlc brands": {"meta", "brand_alias", "profile_uuid", "company_name", "entity_type", "vertical",
		"website", "ein", "stock_symbol", "stock_exchange", "vetting_status", "vetting_score", "authorized_contact"},
	"messaging sms 10dlc campaigns": {"meta", "campaign_alias", "vertical", "description", "campaign_source",
		"message_flow", "help_message", "help_keywords", "optin_message", "optin_keywords", "optout_message",
		"optout_keywords", "terms_and_conditions_link", "privacy_policy_link", "campaign_attributes", "error_reason"},
	"messaging sms tollfree": {"terms_and_conditions_link", "privacy_policy_link", "optin_message", "help_message"},
	"verify sessions":        {"locale", "requestor_ip", "destination_country_iso2"}, // docs: a session, in a list too

	"voice calls streams list":    {"total_count"}, // QA saw it live, where the fixture has count
	"messaging sms tollfree list": {"total_count"}, // docs and the SDK's struct, where the fixture has count
}

// qaNotSent are fields an SDK fixture has that the API does not send: the
// oracle reads the fixture without them, so --schema must not list them.
var qaNotSent = map[string]string{
	"voice calls list":            "meta.total_count", // QA live
	"voice recordings list":       "meta.total_count", // QA live
	"voice calls streams list":    "meta.count",       // QA live: total_count
	"messaging sms tollfree list": "meta.count",       // docs and the SDK's struct: total_count (live is 403)
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

// qaFixture reads an SDK fixture from testdata/plivo-go.
func qaFixture(name string) (map[string]any, bool) {
	b, err := os.ReadFile(filepath.Join("testdata", "plivo-go", name+".json"))
	if err != nil {
		return nil, false
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return nil, false
	}
	return doc, true
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

// qaSDKSchemaPaths checks that every output field --schema lists for a
// command is in the SDK's fixture of its response. It returns how many
// commands it checked.
func qaSDKSchemaPaths(t *testing.T) int {
	n := 0
	for _, cmd := range qaSorted(qaFixtures) {
		t.Run(cmd, func(t *testing.T) {
			n++
			owner := qaOracleOwners[cmd]
			doc, ok := qaFixture(qaFixtures[cmd])
			if !ok {
				qaVerdict(t, map[string][]string{qaGapOwner("", owner): {"no fixture " + qaFixtures[cmd]}})
				return
			}
			have := map[string]bool{"api_id": true} // every response has one
			qaJSONPaths(doc, "", have)
			delete(have, qaNotSent[cmd])
			missing := map[string][]string{}
			for _, f := range outputFields(outputTypes["plivo "+cmd]) {
				names := qaFieldNames(f.Path)
				if have[f.Path] || qaIsConfirmed(cmd, names) {
					continue
				}
				o := qaGapOwner(names[len(names)-1], owner)
				missing[o] = append(missing[o], f.Path)
			}
			problems := map[string][]string{}
			for o, paths := range missing {
				problems[o] = []string{fmt.Sprintf("--schema lists %s, which the SDK's %s lacks or qaNotSent says the API does not send", strings.Join(paths, ", "), qaFixtures[cmd])}
			}
			qaVerdict(t, problems)
		})
	}
	return n
}

// qaSDKDecoding answers each get and list command with its SDK fixture: it
// must succeed in every format, and its table must show the record. It
// returns how many commands it checked.
func qaSDKDecoding(t *testing.T) int {
	setFakeCreds(t)
	srv := newQAServer(t, qaSweepReplies)
	t.Cleanup(func() { srv.handler = nil })
	samples := map[string]qaSample{}
	for _, s := range qaAPISamples() {
		samples[s.path] = s
	}
	n := 0
	for _, cmd := range qaSorted(qaFixtures) {
		t.Run(cmd, func(t *testing.T) {
			n++
			doc, ok := qaFixture(qaFixtures[cmd])
			s, sampled := samples[cmd]
			if !ok || !sampled {
				qaVerdict(t, map[string][]string{qaGapOwner("", qaOracleOwners[cmd]): {"no fixture or no sample"}})
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
				qaVerdict(t, map[string][]string{qaGapOwner("", qaOracleOwners[cmd]): p})
			}
		})
	}
	return n
}

// qaShowsRecord reports whether table holds one of the text values of the
// fixture's record: the object itself, an object it nests the record in, or
// the first row of its list, at the top or in such an object.
func qaShowsRecord(table string, doc map[string]any) bool {
	var texts []string
	add := func(obj map[string]any) {
		for k, v := range obj {
			if s, ok := v.(string); ok && len(s) >= 4 && k != "api_id" {
				texts = append(texts, s)
			}
		}
	}
	first := func(v any) {
		if list, ok := v.([]any); ok && len(list) > 0 {
			switch row := list[0].(type) {
			case map[string]any:
				add(row)
			case string:
				texts = append(texts, row)
			}
		}
	}
	add(doc)
	for _, v := range doc {
		first(v)
		if obj, ok := v.(map[string]any); ok {
			add(obj)
			for _, inner := range obj {
				first(inner)
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

// qaSDKRequestKeys runs every command and holds each request it sends to the
// SDK's request with that method and path (or the docs', in qaDocsRequests):
// it may send only parameters they take. A query filter such as
// end_time__gte counts as its field, end_time. It returns how many commands
// it checked.
func qaSDKRequestKeys(t *testing.T) int {
	raw, err := os.ReadFile(filepath.Join("testdata", "plivo-go", "requests.json"))
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
	n := 0
	for _, s := range qaAPISamples() {
		t.Run(s.path, func(t *testing.T) {
			n++
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
	return n
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
