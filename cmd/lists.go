package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/plivo/plivo-cli/internal/clierr"
)

// Values the list filters accept, as the API reference documents them (the
// agents states come from the agents skill). Matching is case-sensitive: the
// docs give each value in one spelling only, and the SIP Trunking call list
// answers a wrong one with "(case sensitive)".
var (
	directionValues     = []string{dirInbound, dirOutbound}
	sipHangupSources    = []string{"customer", "carrier", "zentrunk"}
	stirValues          = []string{"Verified", "Not Verified", "Not Applicable"}
	messageStateValues  = []string{"queued", "sent", "delivered", "undelivered", "failed", "received"}
	numberTypeValues    = []string{"local", "mobile", "fixed", "national", "tollfree"}
	numberServices      = []string{"voice", "sms", "mms"}
	mpcStatusValues     = []string{"active", "initialized", "ended"}
	tollfreeStatuses    = []string{"SUBMITTED", "PROCESSING", "APPROVED", "REJECTED", "UPDATE_REQUIRED"}
	verifyStatusValues  = []string{"in-progress", "verified", "expired"}
	agentStateValues    = []string{"DRAFT", "ACTIVE", "PAUSED"}
	complianceStatuses  = []string{"draft", "submitted", "accepted", "rejected", "suspended", "expired"}
	complianceNumTypes  = []string{"local", "mobile", "tollfree"}
	complianceUserTypes = []string{"individual", "business"}
)

// oneOf renders an allowed set for a flag's usage line, so --help lists exactly
// what validateEnum accepts. When any value has a space, every value is quoted.
func oneOf(allowed []string) string {
	if !strings.Contains(strings.Join(allowed, ""), " ") {
		return strings.Join(allowed, "|")
	}
	out := make([]string, len(allowed))
	for i, v := range allowed {
		out[i] = strconv.Quote(v)
	}
	return strings.Join(out, "|")
}

// validateEnum refuses a filter value the API does not document, before any
// request. Sent as-is, an unknown value mostly comes back as an empty list with
// exit 0, which reads as "nothing matched" when the filter itself was wrong.
//
// Surrounding whitespace is trimmed in place; an empty value means the filter
// is unset and passes.
func validateEnum(flag string, value *string, allowed ...string) error {
	*value = strings.TrimSpace(*value)
	if *value == "" {
		return nil
	}
	for _, a := range allowed {
		if *value == a {
			return nil
		}
	}
	return badEnumValue(flag, *value, fmt.Sprintf("unknown value %q", *value), allowed)
}

// validateEnumList is validateEnum for a comma-separated filter such as
// --services voice,sms. Each entry must be allowed; the value is rewritten
// without the spaces around entries.
func validateEnumList(flag string, value *string, allowed ...string) error {
	if strings.TrimSpace(*value) == "" {
		*value = ""
		return nil
	}
	entries := strings.Split(*value, ",")
	for i := range entries {
		entries[i] = strings.TrimSpace(entries[i])
		if entries[i] == "" {
			return badEnumValue(flag, *value, fmt.Sprintf("empty entry in %q", *value), allowed)
		}
		if err := validateEnum(flag, &entries[i], allowed...); err != nil {
			return err
		}
	}
	*value = strings.Join(entries, ",")
	return nil
}

// badEnumValue builds the BAD_FLAG for a value outside the allowed set. A value
// that differs only in case names the spelling to use.
func badEnumValue(flag, value, reason string, allowed []string) error {
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = strconv.Quote(a)
	}
	e := clierr.BadFlag(flag, reason)
	e.Hint = "Allowed values: " + strings.Join(quoted, ", ") + "."
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			e.Hint = fmt.Sprintf("Values are case-sensitive: use %q. %s", a, e.Hint)
			break
		}
	}
	e.Context["value"] = value
	e.Context["allowed"] = allowed
	return e
}
