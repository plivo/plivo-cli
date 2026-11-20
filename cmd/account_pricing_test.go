package cmd

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A trimmed answer of GET /Pricing/: the shape is the API's, the rates made up.
const pricingBody = `{"api_id":"a","country":"Examplia","country_code":99,"country_iso":"EX",
"message":{"inbound":{"rate":null},"outbound":{"rate":"0.08000"},
  "outbound_networks_list":[{"group_name":"Net A","rate":"0.08000"},{"group_name":"Net B","rate":"0.09000"}]},
"mms":{"inbound":{"rate":null},"outbound":{"rate":null},"outbound_networks_list":[]},
"phone_numbers":{"local":{"rate":"2.50000","rates":[{"capabilities":["voice","sms"],"rental_rate":"2.50000","setup_rate":"0.50000"}]}},
"voice":{"inbound":{"ip":{"rate":"0.00312"},"local":{"rate":"0.00250"},"tollfree":{"rate":"0.20000"}},
  "outbound":{"ip":{"rate":"0.00312"},"local":{"rate":"0.00250"},"tollfree":{"rate":null},
    "rates":[{"origination_prefix":[""],"prefix":["99"],"rate":"0.05000","voice_network_group":"Examplia International","voice_unit":60}]}},
"whatsapp":{"marketing_conversation":{"rate":"0.01204"}}}`

func TestAccountPricing_sendsTheCountryUpperCased(t *testing.T) {
	setFakeCreds(t)
	urls, _ := sipServer(t, http.StatusOK, pricingBody)

	if err, _, _ := execCmd(t, "account", "pricing", "--country", " ex "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := url.Parse(urls()[0])
	if !strings.HasSuffix(u.Path, "/Pricing/") || u.Query().Get("country_iso") != "EX" {
		t.Errorf("want GET .../Pricing/?country_iso=EX, got %s", u)
	}
}

func TestAccountPricing_tableShowsTheHeadlineRates(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, pricingBody)

	err, stdout, stderr := execCmd(t, "account", "pricing", "--country", "EX", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"Examplia (EX)",
		"sms  outbound  0.08000",
		"sms  inbound   -",
		"voice  inbound  local     0.00250",
		"voice  outbound  tollfree  -",
		"Examplia International",
		"numbers  local  2.50000",
		"whatsapp  marketing_conversation  0.01204",
	} {
		if !strings.Contains(squeeze(stdout), squeeze(want)) {
			t.Errorf("table lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "2 SMS networks") || !strings.Contains(stderr, "-o json") {
		t.Errorf("stderr should point at the per-network SMS rates in -o json, got %q", stderr)
	}
}

func TestAccountPricing_jsonIsTheWholeAnswer(t *testing.T) {
	setFakeCreds(t)
	sipServer(t, http.StatusOK, pricingBody)

	err, stdout, _ := execCmd(t, "account", "pricing", "--country", "EX", "-o", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"outbound_networks_list", "Net B", "voice_network_group", "setup_rate"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("-o json lacks %q:\n%s", want, stdout)
		}
	}
}

func TestAccountPricing_badCountryIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"account", "pricing", "--country", "USA"},
		{"account", "pricing", "--country", "1"},
		{"account", "pricing", "--country", ""},
		{"account", "pricing"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setFakeCreds(t)
			urls, _ := sipServer(t, http.StatusOK, pricingBody)
			if err, _, _ := execCmd(t, args...); err == nil {
				t.Fatal("want an error")
			}
			if n := len(urls()); n != 0 {
				t.Errorf("a bad country reached the network: %d requests", n)
			}
		})
	}
}

// squeeze collapses runs of spaces, so a column's width does not matter.
func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }
