package cmd

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/clierr"
)

func TestValidateEnum(t *testing.T) {
	t.Run("unset passes", func(t *testing.T) {
		for _, in := range []string{"", "   "} {
			v := in
			if err := validateEnum("direction", &v, directionValues...); err != nil || v != "" {
				t.Fatalf("validateEnum(%q) = %v, value %q; want nil, \"\"", in, err, v)
			}
		}
	})

	t.Run("documented value passes, trimmed", func(t *testing.T) {
		v := "  inbound "
		if err := validateEnum("direction", &v, directionValues...); err != nil || v != "inbound" {
			t.Fatalf("got %v, value %q; want nil, \"inbound\"", err, v)
		}
	})

	t.Run("unknown value is a BAD_FLAG naming the allowed set", func(t *testing.T) {
		v := "sideways"
		var ce *clierr.Error
		if !errors.As(validateEnum("direction", &v, directionValues...), &ce) || ce.Code != clierr.CodeBadFlag {
			t.Fatalf("want BAD_FLAG, got %#v", ce)
		}
		if ce.Context["flag"] != "direction" || ce.Context["value"] != "sideways" {
			t.Errorf("context = %v", ce.Context)
		}
		if !strings.Contains(ce.Hint, `"inbound", "outbound"`) {
			t.Errorf("hint does not list the allowed values: %q", ce.Hint)
		}
	})

	t.Run("wrong case names the right spelling", func(t *testing.T) {
		v := "UPDATE_required"
		var ce *clierr.Error
		if !errors.As(validateEnum("status", &v, tollfreeStatuses...), &ce) {
			t.Fatal("want an error")
		}
		if !strings.Contains(ce.Hint, `case-sensitive: use "UPDATE_REQUIRED"`) {
			t.Errorf("hint = %q", ce.Hint)
		}
	})
}

func TestValidateEnumList(t *testing.T) {
	v := " voice , sms"
	if err := validateEnumList("services", &v, numberServices...); err != nil || v != "voice,sms" {
		t.Fatalf("got %v, value %q; want nil, \"voice,sms\"", err, v)
	}
	for _, bad := range []string{"voice,fax", "voice,,sms", "sms,", ","} {
		v := bad
		var ce *clierr.Error
		if !errors.As(validateEnumList("services", &v, numberServices...), &ce) || ce.Code != clierr.CodeBadFlag {
			t.Errorf("%q: want BAD_FLAG, got %#v", bad, ce)
		}
	}
}

// The whole point is to fail before the API is asked: an unknown value sent
// through comes back as an empty list with exit 0.
func TestListFilters_unknownValueFailsBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"direction", []string{"sip", "calls", "list", "--direction", "sideways"}},
		{"hangup-source", []string{"sip", "calls", "list", "--hangup-source", "plivo"}},
		{"stir-verification", []string{"sip", "calls", "list", "--stir-verification", "verified"}},
		{"direction", []string{"sip", "trunks", "list", "--direction", "both"}},
		{"direction", []string{"voice", "calls", "list", "--direction", "Inbound"}},
		{"state", []string{"messaging", "sms", "list", "--state", "read"}},
		{"direction", []string{"messaging", "whatsapp", "list", "--direction", "out"}},
		{"state", []string{"messaging", "mms", "list", "--state", "bogus"}},
		{"type", []string{"numbers", "list", "--type", "bogus"}},
		{"services", []string{"numbers", "list", "--services", "voice,fax"}},
		{"type", []string{"numbers", "search", "--country", "US", "--type", "toll-free"}},
		{"status", []string{"voice", "multiparty", "list", "--status", "live"}},
		{"status", []string{"messaging", "sms", "tollfree", "list", "--status", "IN_REVIEW"}},
		{"status", []string{"verify", "sessions", "list", "--status", "pending"}},
		{"state", []string{"agents", "list", "--state", "draft"}},
		{"status", []string{"numbers", "compliance", "list", "--status", "approved"}},
		{"number-type", []string{"numbers", "compliance", "list", "--number-type", "national"}},
		{"user-type", []string{"numbers", "compliance", "list", "--user-type", "company"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			_, count := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			err, _, _ := execCmd(t, tc.args...)
			var ce *clierr.Error
			if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
				t.Fatalf("want BAD_FLAG, got %v", err)
			}
			if ce.Context["flag"] != tc.flag {
				t.Errorf("flag in context = %v, want %s", ce.Context["flag"], tc.flag)
			}
			if n := count(); n != 0 {
				t.Fatalf("%d request(s) went out before validation", n)
			}
		})
	}
}

// Every documented value must still reach the API, exactly as spelled there;
// --dry-run must not skip the check.
func TestListFilters_documentedValuesReachTheQuery(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"sip", "calls", "list", "--stir-verification", "Not Verified", "--hangup-source", "zentrunk"},
			[]string{"stir_verification=Not+Verified", "hangup_source=zentrunk"}},
		{[]string{"sip", "trunks", "list", "--direction", "inbound"}, []string{"trunk_direction=inbound"}},
		{[]string{"voice", "calls", "list", "--direction", " outbound "}, []string{"call_direction=outbound"}},
		{[]string{"messaging", "whatsapp", "list", "--state", "received", "--direction", "inbound"},
			[]string{"message_state=received", "message_direction=inbound"}},
		{[]string{"numbers", "list", "--type", "national", "--services", "voice, sms"},
			[]string{"type=national", "services=voice%2Csms"}},
		{[]string{"numbers", "search", "--country", "US", "--type", "national"}, []string{"type=national"}},
		{[]string{"voice", "multiparty", "list", "--status", "initialized"}, []string{"status=initialized"}},
		{[]string{"messaging", "sms", "tollfree", "list", "--status", "UPDATE_REQUIRED"}, []string{"status=UPDATE_REQUIRED"}},
		{[]string{"verify", "sessions", "list", "--status", "in-progress"}, []string{"status=in-progress"}},
		{[]string{"agents", "list", "--state", "PAUSED"}, []string{"state=PAUSED"}},
		{[]string{"numbers", "compliance", "list", "--status", "accepted", "--number-type", "mobile", "--user-type", "individual"},
			[]string{"status=accepted", "number_type=mobile", "user_type=individual"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, `{"meta":{},"objects":[]}`)

			if err, _, _ := execCmd(t, tc.args...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := urls()
			if len(got) != 1 {
				t.Fatalf("expected 1 request, got %v", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got[0], w) {
					t.Errorf("query missing %s\n  got: %s", w, got[0])
				}
			}
		})
	}

	t.Run("dry-run still validates", func(t *testing.T) {
		setFakeCreds(t)
		err, _, _ := execCmd(t, "numbers", "list", "--type", "bogus", "--dry-run")
		var ce *clierr.Error
		if !errors.As(err, &ce) || ce.Code != clierr.CodeBadFlag {
			t.Fatalf("want BAD_FLAG under --dry-run, got %v", err)
		}
	})
}
