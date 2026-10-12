package cmd

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/plivo/plivo-cli/internal/api"
)

// outputTypes maps each get and list command to the api type its response
// decodes into, so --schema can list the fields the CLI maps. -o json passes
// the raw response through, so they are a subset of what comes back.
// TestOutputTypes_coverEveryGetAndList names the get/list commands left out,
// among them numbers compliance get: its response nests the application under
// "compliance", which ComplianceApplication does not describe.
var outputTypes = map[string]reflect.Type{
	"plivo account applications get":              reflect.TypeFor[api.Application](),
	"plivo account applications list":             reflect.TypeFor[api.ApplicationList](),
	"plivo account get":                           reflect.TypeFor[api.Account](),
	"plivo account subaccounts get":               reflect.TypeFor[api.Subaccount](),
	"plivo account subaccounts list":              reflect.TypeFor[api.SubaccountList](),
	"plivo agents get":                            reflect.TypeFor[api.Agent](),
	"plivo agents list":                           reflect.TypeFor[api.AgentList](),
	"plivo agents nodes get":                      reflect.TypeFor[api.AgentFlowNode](),
	"plivo agents nodes list":                     reflect.TypeFor[api.AgentFlowNodeList](),
	"plivo agents runs get":                       reflect.TypeFor[api.AgentRun](),
	"plivo agents runs list":                      reflect.TypeFor[api.AgentRunList](),
	"plivo messaging get":                         reflect.TypeFor[api.Message](),
	"plivo messaging mms list":                    reflect.TypeFor[api.MessageList](),
	"plivo messaging sms 10dlc brands get":        reflect.TypeFor[api.Brand10DLC](),
	"plivo messaging sms 10dlc brands list":       reflect.TypeFor[api.Brand10DLCList](),
	"plivo messaging sms 10dlc campaigns get":     reflect.TypeFor[api.Campaign10DLC](),
	"plivo messaging sms 10dlc campaigns list":    reflect.TypeFor[api.Campaign10DLCList](),
	"plivo messaging sms 10dlc links list":        reflect.TypeFor[api.NumberLink10DLCList](),
	"plivo messaging sms list":                    reflect.TypeFor[api.MessageList](),
	"plivo messaging sms powerpacks get":          reflect.TypeFor[api.Powerpack](),
	"plivo messaging sms powerpacks list":         reflect.TypeFor[api.PowerpackList](),
	"plivo messaging sms powerpacks numbers list": reflect.TypeFor[api.PowerpackNumberList](),
	"plivo messaging sms tollfree get":            reflect.TypeFor[api.TollFreeVerification](),
	"plivo messaging sms tollfree list":           reflect.TypeFor[api.TollFreeVerificationList](),
	"plivo messaging whatsapp list":               reflect.TypeFor[api.MessageList](),
	"plivo numbers compliance list":               reflect.TypeFor[api.ComplianceApplicationList](),
	"plivo numbers get":                           reflect.TypeFor[api.Number](),
	"plivo numbers list":                          reflect.TypeFor[api.NumberList](),
	"plivo numbers masking sessions get":          reflect.TypeFor[api.MaskingSession](),
	"plivo numbers masking sessions list":         reflect.TypeFor[api.MaskingSessionList](),
	"plivo sip calls get":                         reflect.TypeFor[api.SIPTrunkCall](),
	"plivo sip calls list":                        reflect.TypeFor[api.SIPTrunkCallList](),
	"plivo sip credentials get":                   reflect.TypeFor[api.SIPTrunkCredential](),
	"plivo sip credentials list":                  reflect.TypeFor[api.SIPTrunkCredentialList](),
	"plivo sip ip-acl get":                        reflect.TypeFor[api.SIPTrunkACL](),
	"plivo sip ip-acl list":                       reflect.TypeFor[api.SIPTrunkACLList](),
	"plivo sip trunks get":                        reflect.TypeFor[api.SIPTrunk](),
	"plivo sip trunks list":                       reflect.TypeFor[api.SIPTrunkList](),
	"plivo sip uris get":                          reflect.TypeFor[api.SIPTrunkURI](),
	"plivo sip uris list":                         reflect.TypeFor[api.SIPTrunkURIList](),
	"plivo verify sessions get":                   reflect.TypeFor[api.VerifySession](),
	"plivo verify sessions list":                  reflect.TypeFor[api.VerifySessionList](),
	"plivo voice calls get":                       reflect.TypeFor[api.Call](),
	"plivo voice calls list":                      reflect.TypeFor[api.CallList](),
	"plivo voice calls streams get":               reflect.TypeFor[api.AudioStream](),
	"plivo voice calls streams list":              reflect.TypeFor[api.AudioStreamList](),
	"plivo voice conferences get":                 reflect.TypeFor[api.Conference](),
	"plivo voice conferences list":                reflect.TypeFor[api.ConferenceList](),
	"plivo voice endpoints get":                   reflect.TypeFor[api.Endpoint](),
	"plivo voice endpoints list":                  reflect.TypeFor[api.EndpointList](),
	"plivo voice multiparty get":                  reflect.TypeFor[api.MPC](),
	"plivo voice multiparty list":                 reflect.TypeFor[api.MPCList](),
	"plivo voice multiparty participant list":     reflect.TypeFor[api.MPCParticipantList](),
	"plivo voice recordings get":                  reflect.TypeFor[api.Recording](),
	"plivo voice recordings list":                 reflect.TypeFor[api.RecordingList](),
}

// fieldSchema is one field of a command's output, as a path under data.
type fieldSchema struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

var rawMessageType = reflect.TypeFor[json.RawMessage]()

// outputFields lists t's JSON fields the way encoding/json reads them: by
// json tag, embedded structs flattened. Nested objects add "parent.child",
// arrays "parent[]" (and "parent[].child" for arrays of objects).
func outputFields(t reflect.Type) []fieldSchema {
	fields := []fieldSchema{}
	var walk func(t reflect.Type, prefix string, seen map[reflect.Type]bool)
	walk = func(t reflect.Type, prefix string, seen map[reflect.Type]bool) {
		if seen[t] {
			return // a type that contains itself
		}
		seen[t] = true
		defer delete(seen, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			ft := derefType(f.Type)
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				walk(ft, prefix, seen) // promoted, as encoding/json does
				continue
			}
			if !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			path := prefix + name
			for {
				fields = append(fields, fieldSchema{Path: path, Type: jsonType(ft)})
				if ft.Kind() != reflect.Slice && ft.Kind() != reflect.Array || ft == rawMessageType {
					break
				}
				ft, path = derefType(ft.Elem()), path+"[]"
			}
			if ft.Kind() == reflect.Struct {
				walk(ft, path+".", seen)
			}
		}
	}
	walk(derefType(t), "", map[reflect.Type]bool{})
	return fields
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// jsonType names a Go type by the JSON value encoding/json writes for it.
func jsonType(t reflect.Type) string {
	switch {
	case t == rawMessageType || t.Kind() == reflect.Interface:
		return "any"
	case t.Kind() == reflect.String:
		return "string"
	case t.Kind() == reflect.Bool:
		return "boolean"
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Float64:
		return "number"
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		return "array"
	}
	return "object"
}
