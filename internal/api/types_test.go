package api

import (
	"encoding/json"
	"testing"
)

// Each test below feeds real-shaped Plivo JSON into the matching response type
// and asserts the critical fields parse correctly. Catches struct-tag drift
// (e.g. someone renaming `from_number` to `From` without updating the tag).

func TestNumber_ResolvedAppID(t *testing.T) {
	cases := []struct {
		name        string
		n           Number
		wantAppID   string
	}{
		{
			name:      "AppID directly populated",
			n:         Number{AppID: "12345"},
			wantAppID: "12345",
		},
		{
			name:      "fallback to Application URI",
			n:         Number{Application: "/v1/Account/MAabc/Application/67890/"},
			wantAppID: "67890",
		},
		{
			name:      "Application URI without trailing slash",
			n:         Number{Application: "/v1/Account/MAabc/Application/67890"},
			wantAppID: "67890",
		},
		{
			name:      "AppID wins over Application URI",
			n:         Number{AppID: "win", Application: "/v1/Account/MAabc/Application/lose/"},
			wantAppID: "win",
		},
		{
			name:      "neither set returns empty",
			n:         Number{},
			wantAppID: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.n.ResolvedAppID()
			if got != tc.wantAppID {
				t.Errorf("ResolvedAppID() = %q, want %q", got, tc.wantAppID)
			}
		})
	}
}

func TestAccount_unmarshal(t *testing.T) {
	body := []byte(`{
		"api_id": "abc-123",
		"auth_id": "MAxxxxxxxxxxxxxxxxxxx",
		"name": "Bruce Wayne",
		"account_type": "standard",
		"billing_mode": "prepaid",
		"cash_credits": "18.65",
		"address": "-",
		"timezone": "UTC",
		"auto_recharge": false
	}`)
	var a Account
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.AuthID != "MAxxxxxxxxxxxxxxxxxxx" {
		t.Errorf("AuthID = %q", a.AuthID)
	}
	if a.Name != "Bruce Wayne" {
		t.Errorf("Name = %q", a.Name)
	}
	if a.BillingMode != "prepaid" {
		t.Errorf("BillingMode = %q", a.BillingMode)
	}
	if a.CashCredits != "18.65" {
		t.Errorf("CashCredits = %q", a.CashCredits)
	}
}

func TestNumber_unmarshal(t *testing.T) {
	body := []byte(`{
		"number": "+14155551234",
		"type": "local",
		"region": "California",
		"country": "United States",
		"app_id": "12345",
		"sub_account": "SAxxx",
		"alias": "support line",
		"monthly_rental_rate": "0.80000",
		"voice_enabled": true,
		"sms_enabled": true,
		"mms_enabled": false,
		"added_on": "2026-01-01T00:00:00Z",
		"renewal_date": "2026-02-01",
		"resource_uri": "/v1/Account/MAabc/Number/+14155551234/"
	}`)
	var n Number
	if err := json.Unmarshal(body, &n); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n.Number != "+14155551234" || n.Type != "local" || n.Country != "United States" {
		t.Errorf("basic fields wrong: %+v", n)
	}
	if !n.VoiceEnabled || !n.SMSEnabled || n.MMSEnabled {
		t.Errorf("enabled flags wrong: voice=%v sms=%v mms=%v", n.VoiceEnabled, n.SMSEnabled, n.MMSEnabled)
	}
	if n.AppID != "12345" {
		t.Errorf("AppID = %q", n.AppID)
	}
}

func TestNumberList_unmarshal(t *testing.T) {
	body := []byte(`{
		"api_id": "abc",
		"meta": {"limit": 20, "offset": 0, "total_count": 2, "next": null, "previous": null},
		"objects": [
			{"number": "+1", "type": "local"},
			{"number": "+2", "type": "tollfree"}
		]
	}`)
	var nl NumberList
	if err := json.Unmarshal(body, &nl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if nl.Meta.TotalCount != 2 {
		t.Errorf("Meta.TotalCount = %d, want 2", nl.Meta.TotalCount)
	}
	if len(nl.Objects) != 2 {
		t.Fatalf("Objects len = %d", len(nl.Objects))
	}
	if nl.Objects[0].Number != "+1" || nl.Objects[1].Type != "tollfree" {
		t.Errorf("objects content wrong: %+v", nl.Objects)
	}
}

func TestApplication_unmarshal(t *testing.T) {
	body := []byte(`{
		"app_id": "12345",
		"app_name": "my-app",
		"answer_url": "https://example.com/answer",
		"answer_method": "POST",
		"hangup_url": "https://example.com/hangup",
		"message_url": "https://example.com/sms",
		"default_number_app": true,
		"public_uri": false,
		"enabled": true,
		"sip_uri": "sip:my-app@phone.plivo.com"
	}`)
	var a Application
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.AppID != "12345" || a.AppName != "my-app" || !a.DefaultNumberApp || !a.Enabled {
		t.Errorf("app fields wrong: %+v", a)
	}
}

func TestMessage_unmarshal(t *testing.T) {
	body := []byte(`{
		"message_uuid": "abc-uuid",
		"from_number": "+14155551234",
		"to_number": "+14155556789",
		"text": "hello",
		"message_type": "sms",
		"message_direction": "outbound",
		"message_state": "delivered",
		"total_amount": "0.0035",
		"units": 1,
		"message_time": "2026-01-01T00:00:00Z"
	}`)
	var m Message
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.From != "+14155551234" {
		t.Errorf("From = %q (json tag from_number); did somebody rename the tag?", m.From)
	}
	if m.To != "+14155556789" {
		t.Errorf("To = %q (json tag to_number)", m.To)
	}
	if m.State != "delivered" || m.Type != "sms" || m.Direction != "outbound" {
		t.Errorf("state/type/direction wrong: %+v", m)
	}
	if m.Units != 1 {
		t.Errorf("Units = %d", m.Units)
	}
}

func TestMessageSendResponse_unmarshal(t *testing.T) {
	body := []byte(`{
		"api_id": "send-api-id",
		"message": "message(s) queued",
		"message_uuid": ["uuid-1", "uuid-2"]
	}`)
	var r MessageSendResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.MessageUUID) != 2 || r.MessageUUID[0] != "uuid-1" {
		t.Errorf("MessageUUID = %v", r.MessageUUID)
	}
}

func TestCall_unmarshal(t *testing.T) {
	body := []byte(`{
		"call_uuid": "call-uuid",
		"from_number": "+14155551234",
		"to_number": "+14155556789",
		"call_direction": "outbound",
		"call_duration": 42,
		"bill_duration": 60,
		"hangup_cause_name": "NORMAL_CLEARING",
		"hangup_source": "Caller",
		"total_amount": "0.0150",
		"answer_time": "2026-01-01T00:00:00Z",
		"end_time": "2026-01-01T00:01:00Z",
		"initiation_time": "2026-01-01T00:00:00Z"
	}`)
	var c Call
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.From != "+14155551234" || c.To != "+14155556789" {
		t.Errorf("from/to wrong")
	}
	if c.CallDuration != 42 || c.BillDuration != 60 {
		t.Errorf("durations wrong: call=%d bill=%d", c.CallDuration, c.BillDuration)
	}
	if c.HangupCause != "NORMAL_CLEARING" {
		t.Errorf("HangupCause = %q (json tag hangup_cause_name)", c.HangupCause)
	}
	if c.InitTime != "2026-01-01T00:00:00Z" {
		t.Errorf("InitTime = %q (json tag initiation_time)", c.InitTime)
	}
}

func TestRecording_unmarshal(t *testing.T) {
	body := []byte(`{
		"recording_id": "rec-uuid",
		"call_uuid": "call-uuid",
		"recording_type": "call",
		"recording_format": "mp3",
		"recording_url": "https://s3.amazonaws.com/recording.mp3",
		"recording_duration_ms": 30000,
		"add_time": "2026-01-01T00:00:00Z"
	}`)
	var r Recording
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.RecordingID != "rec-uuid" || r.CallUUID != "call-uuid" {
		t.Errorf("ids wrong: %+v", r)
	}
	if r.RecordingDurationMS != 30000 {
		t.Errorf("duration_ms wrong: %d", r.RecordingDurationMS)
	}
}

func TestVerifySession_unmarshal(t *testing.T) {
	body := []byte(`{
		"session_uuid": "sess-uuid",
		"app_uuid": "app-uuid",
		"recipient": "+14155551234",
		"channel": "sms",
		"status": "verified",
		"count_of_attempts": 1,
		"locale_used": "en-US",
		"destination_country_iso2": "US",
		"charge_amount": "0.05",
		"charge_amount_currency": "USD"
	}`)
	var v VerifySession
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.SessionUUID != "sess-uuid" || v.Recipient != "+14155551234" || v.Status != "verified" {
		t.Errorf("fields wrong: %+v", v)
	}
	if v.CountOfAttempts != 1 {
		t.Errorf("CountOfAttempts = %d", v.CountOfAttempts)
	}
}

func TestLookupNumber_unmarshal(t *testing.T) {
	// Shape returned by lookup.plivo.com/v1/Number/{n}?type=carrier
	body := []byte(`{
		"api_id": "lookup-api-id",
		"phone_number": "+14155551234",
		"country": {"name": "United States", "iso2": "US", "iso3": "USA"},
		"format": {
			"e164": "+14155551234",
			"international": "+1 415-555-1234",
			"national": "(415) 555-1234",
			"rfc3966": "tel:+1-415-555-1234"
		},
		"carrier": {
			"mobile_country_code": "311",
			"mobile_network_code": "490",
			"name": "AT&T Wireless",
			"type": "mobile",
			"ported": "no"
		},
		"resource_uri": "/v1/Number/+14155551234"
	}`)
	var n LookupNumber
	if err := json.Unmarshal(body, &n); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n.PhoneNumber != "+14155551234" {
		t.Errorf("PhoneNumber = %q", n.PhoneNumber)
	}
	if n.Country.Name != "United States" || n.Country.ISO2 != "US" {
		t.Errorf("country nested object wrong: %+v", n.Country)
	}
	if n.Format.E164 != "+14155551234" || n.Format.National != "(415) 555-1234" {
		t.Errorf("format wrong: %+v", n.Format)
	}
	if n.Carrier.Name != "AT&T Wireless" || n.Carrier.Type != "mobile" {
		t.Errorf("carrier wrong: %+v", n.Carrier)
	}
}

func TestSubaccount_unmarshal(t *testing.T) {
	body := []byte(`{
		"auth_id": "SAxxxxxxxxxxxxxxxxxxx",
		"name": "test-sub",
		"auth_token": "secret-token",
		"enabled": true,
		"created_on": "2026-01-01T00:00:00Z",
		"account": "/v1/Account/MAabc/"
	}`)
	var s Subaccount
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.AuthID != "SAxxxxxxxxxxxxxxxxxxx" || s.Name != "test-sub" || !s.Enabled {
		t.Errorf("subaccount fields wrong: %+v", s)
	}
}

func TestEndpoint_unmarshal(t *testing.T) {
	body := []byte(`{
		"endpoint_id": "12345",
		"username": "alice",
		"alias": "alice-desk",
		"sip_uri": "sip:alice@phone.plivo.com",
		"app_id": "app-67890"
	}`)
	var e Endpoint
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.EndpointID != "12345" || e.Username != "alice" || e.AppID != "app-67890" {
		t.Errorf("endpoint fields wrong: %+v", e)
	}
}

func TestConference_unmarshal(t *testing.T) {
	body := []byte(`{
		"api_id": "conf-api-id",
		"conference_name": "room-1",
		"conference_run_time": "120",
		"conference_member_count": "2",
		"members": [
			{"member_id": "1", "from": "+1", "to": "+2", "call_uuid": "u1", "muted": false, "deaf": false},
			{"member_id": "2", "from": "+3", "to": "+4", "call_uuid": "u2", "muted": true, "deaf": false}
		]
	}`)
	var c Conference
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.ConferenceName != "room-1" {
		t.Errorf("ConferenceName = %q", c.ConferenceName)
	}
	if len(c.Members) != 2 {
		t.Fatalf("Members len = %d", len(c.Members))
	}
	if !c.Members[1].Muted || c.Members[0].Muted {
		t.Errorf("member mute states wrong")
	}
}

func TestConferenceList_namesArray(t *testing.T) {
	// Plivo's GET /Conference/ returns a flat string list, not full objects.
	body := []byte(`{"api_id": "x", "conferences": ["room-1", "room-2", "lobby"]}`)
	var cl ConferenceList
	if err := json.Unmarshal(body, &cl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cl.Conferences) != 3 {
		t.Errorf("Conferences len = %d", len(cl.Conferences))
	}
	if cl.Conferences[0] != "room-1" || cl.Conferences[2] != "lobby" {
		t.Errorf("conference names wrong: %v", cl.Conferences)
	}
}

func TestMPC_unmarshal(t *testing.T) {
	body := []byte(`{
		"mpc_uuid": "mpc-uuid",
		"friendly_name": "sales-call",
		"status": "active",
		"billing_type": "per_minute",
		"created_at": "2026-01-01T00:00:00Z"
	}`)
	var m MPC
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.MPCUUID != "mpc-uuid" || m.FriendlyName != "sales-call" || m.Status != "active" {
		t.Errorf("mpc fields wrong: %+v", m)
	}
}

func TestBrand10DLCList_brandsKey(t *testing.T) {
	// 10DLC list response uses "brands" key, not "objects".
	body := []byte(`{
		"api_id": "x",
		"meta": {"limit": 20, "offset": 0, "total_count": 1},
		"brands": [{
			"brand_id": "b1",
			"brand_alias": "acme",
			"legal_entity_name": "ACME Inc",
			"brand_type": "STANDARD",
			"brand_status": "VERIFIED",
			"vertical": "TECHNOLOGY"
		}]
	}`)
	var bl Brand10DLCList
	if err := json.Unmarshal(body, &bl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(bl.Brands) != 1 {
		t.Fatalf("Brands len = %d", len(bl.Brands))
	}
	if bl.Brands[0].LegalEntityName != "ACME Inc" || bl.Brands[0].BrandStatus != "VERIFIED" {
		t.Errorf("brand fields wrong: %+v", bl.Brands[0])
	}
}

func TestCampaign10DLCList_campaignsKey(t *testing.T) {
	// 10DLC list response uses "campaigns" key.
	body := []byte(`{
		"api_id": "x",
		"meta": {"limit": 20, "offset": 0, "total_count": 1},
		"campaigns": [{
			"campaign_id": "c1",
			"campaign_alias": "promo",
			"brand_id": "b1",
			"usecase": "MARKETING",
			"sub_usecases": ["PROMOTIONAL", "OTT"],
			"campaign_status": "ACTIVE",
			"embedded_link": true,
			"age_gated": false
		}]
	}`)
	var cl Campaign10DLCList
	if err := json.Unmarshal(body, &cl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cl.Campaigns) != 1 {
		t.Fatalf("Campaigns len = %d", len(cl.Campaigns))
	}
	if cl.Campaigns[0].Usecase != "MARKETING" || !cl.Campaigns[0].EmbeddedLink {
		t.Errorf("campaign fields wrong: %+v", cl.Campaigns[0])
	}
	if len(cl.Campaigns[0].SubUsecases) != 2 {
		t.Errorf("sub_usecases len = %d", len(cl.Campaigns[0].SubUsecases))
	}
}

func TestPowerpack_unmarshal(t *testing.T) {
	body := []byte(`{
		"uuid": "pp-uuid",
		"name": "high-volume-pool",
		"sticky_sender": true,
		"local_connect": true,
		"application_type": "default_message",
		"created_on": "2026-01-01T00:00:00Z"
	}`)
	var p Powerpack
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.UUID != "pp-uuid" || p.Name != "high-volume-pool" || !p.StickySender || !p.LocalConnect {
		t.Errorf("powerpack fields wrong: %+v", p)
	}
}

func TestScopedToken_unmarshal(t *testing.T) {
	body := []byte(`{
		"id": "tok-id",
		"token": "stk_abc123",
		"scopes": ["agent:read", "agent:write"],
		"description": "ci-agent",
		"expires_at": "2026-12-31T23:59:59Z",
		"created_at": "2026-01-01T00:00:00Z"
	}`)
	var s ScopedToken
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Token != "stk_abc123" {
		t.Errorf("Token = %q", s.Token)
	}
	if len(s.Scopes) != 2 || s.Scopes[1] != "agent:write" {
		t.Errorf("Scopes wrong: %v", s.Scopes)
	}
}

func TestGenericResponse_unmarshal(t *testing.T) {
	body := []byte(`{"api_id": "abc", "message": "Updated 1 row"}`)
	var r GenericResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Message != "Updated 1 row" {
		t.Errorf("Message = %q", r.Message)
	}
}

func TestRoundTrip_Number(t *testing.T) {
	// Marshal a populated Number, unmarshal back, and check key fields survived.
	orig := Number{
		Number:        "+14155551234",
		Type:          "local",
		Country:       "US",
		AppID:         "12345",
		Alias:         "support",
		MonthlyRental: "0.80000",
		VoiceEnabled:  true,
		SMSEnabled:    true,
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var round Number
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round.Number != orig.Number || round.AppID != orig.AppID ||
		round.VoiceEnabled != orig.VoiceEnabled || round.MonthlyRental != orig.MonthlyRental {
		t.Errorf("round-trip lost data:\norig:  %+v\nround: %+v", orig, round)
	}
}

func TestRoundTrip_Message(t *testing.T) {
	orig := Message{
		MessageUUID: "abc",
		From:        "+1",
		To:          "+2",
		Text:        "hi",
		Type:        "sms",
		State:       "delivered",
		Units:       1,
	}
	b, _ := json.Marshal(orig)
	var round Message
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round.From != orig.From || round.State != orig.State || round.Units != orig.Units {
		t.Errorf("round-trip lost data:\norig:  %+v\nround: %+v", orig, round)
	}
}
