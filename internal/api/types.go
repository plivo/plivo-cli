package api

import (
	"encoding/json"
	"strings"
)

// ListMeta is the pagination envelope present on every Plivo list response.
type ListMeta struct {
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	TotalCount int    `json:"total_count"`
	Next       string `json:"next,omitempty"`
	Previous   string `json:"previous,omitempty"`
}

type Account struct {
	RawBody
	APIID        string `json:"api_id,omitempty"`
	AuthID       string `json:"auth_id"`
	Name         string `json:"name"`
	AccountType  string `json:"account_type"`
	BillingMode  string `json:"billing_mode"`
	CashCredits  string `json:"cash_credits"`
	Address      string `json:"address,omitempty"`
	City         string `json:"city,omitempty"`
	State        string `json:"state,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	AutoRecharge bool   `json:"auto_recharge,omitempty"`
	ResourceURI  string `json:"resource_uri,omitempty"`
}

type Number struct {
	RawBody
	Number        string `json:"number"`
	Type          string `json:"type"`
	Region        string `json:"region,omitempty"`
	Country       string `json:"country,omitempty"`
	NumberType    string `json:"number_type,omitempty"`
	Application   string `json:"application,omitempty"`
	AppID         string `json:"app_id,omitempty"`
	Subaccount    string `json:"sub_account,omitempty"`
	Alias         string `json:"alias,omitempty"`
	MonthlyRental string `json:"monthly_rental_rate,omitempty"`
	VoiceEnabled  bool   `json:"voice_enabled,omitempty"`
	SMSEnabled    bool   `json:"sms_enabled,omitempty"`
	MMSEnabled    bool   `json:"mms_enabled,omitempty"`
	AddedOn       string `json:"added_on,omitempty"`
	RenewalDate   string `json:"renewal_date,omitempty"`
	ResourceURI   string `json:"resource_uri,omitempty"`
}

type NumberList struct {
	RawBody
	APIID   string   `json:"api_id"`
	Meta    ListMeta `json:"meta"`
	Objects []Number `json:"objects"`
}

// ResolvedAppID returns the attached application id.
// Plivo populates AppID directly on some endpoints and embeds the id in the
// Application resource URI on others; this method normalises both shapes.
func (n *Number) ResolvedAppID() string {
	if n.AppID != "" {
		return n.AppID
	}
	return lastSegment(n.Application)
}

// lastSegment returns the id at the end of a resource URI such as
// /v1/Account/<auth_id>/Application/<app_id>/, or "" for an empty one.
func lastSegment(uri string) string {
	if uri == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(uri, "/"), "/")
	return parts[len(parts)-1]
}

type Application struct {
	RawBody
	APIID               string `json:"api_id,omitempty"`
	AppID               string `json:"app_id"`
	AppName             string `json:"app_name"`
	AnswerURL           string `json:"answer_url,omitempty"`
	AnswerMethod        string `json:"answer_method,omitempty"`
	HangupURL           string `json:"hangup_url,omitempty"`
	HangupMethod        string `json:"hangup_method,omitempty"`
	FallbackAnswerURL   string `json:"fallback_answer_url,omitempty"`
	FallbackMethod      string `json:"fallback_method,omitempty"`
	MessageURL          string `json:"message_url,omitempty"`
	MessageMethod       string `json:"message_method,omitempty"`
	DefaultApp          bool   `json:"default_app,omitempty"`
	DefaultEndpointApp  bool   `json:"default_endpoint_app,omitempty"`
	SubAccount          string `json:"sub_account,omitempty"`
	LogIncomingMessages bool   `json:"log_incoming_messages,omitempty"`
	SIPURI              string `json:"sip_uri,omitempty"`
	PublicURI           bool   `json:"public_uri,omitempty"`
	Enabled             bool   `json:"enabled,omitempty"`
	ResourceURI         string `json:"resource_uri,omitempty"`
}

type ApplicationList struct {
	RawBody
	APIID   string        `json:"api_id"`
	Meta    ListMeta      `json:"meta"`
	Objects []Application `json:"objects"`
}

type Message struct {
	RawBody
	APIID       string `json:"api_id,omitempty"`
	MessageUUID string `json:"message_uuid"`
	From        string `json:"from_number"`
	To          string `json:"to_number"`
	Text        string `json:"text,omitempty"`
	Type        string `json:"message_type"`
	Direction   string `json:"message_direction,omitempty"`
	State       string `json:"message_state,omitempty"`
	TotalAmount string `json:"total_amount,omitempty"`
	TotalRate   string `json:"total_rate,omitempty"`
	Units       int    `json:"units,omitempty"`
	MessageTime string `json:"message_time,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

type MessageList struct {
	RawBody
	APIID   string    `json:"api_id"`
	Meta    ListMeta  `json:"meta"`
	Objects []Message `json:"objects"`
}

// MessageSendResponse is the response from POST /Message/.
type MessageSendResponse struct {
	RawBody
	APIID       string   `json:"api_id"`
	Message     string   `json:"message"`
	MessageUUID []string `json:"message_uuid"`
}

type Call struct {
	RawBody
	APIID        string `json:"api_id,omitempty"`
	CallUUID     string `json:"call_uuid"`
	From         string `json:"from_number"`
	To           string `json:"to_number"`
	Direction    string `json:"call_direction"`
	CallDuration int    `json:"call_duration,omitempty"`
	BillDuration int    `json:"bill_duration,omitempty"`
	TotalAmount  string `json:"total_amount,omitempty"`
	TotalRate    string `json:"total_rate,omitempty"`
	AnswerTime   string `json:"answer_time,omitempty"`
	EndTime      string `json:"end_time,omitempty"`
	InitTime     string `json:"initiation_time,omitempty"`
	HangupCause  string `json:"hangup_cause_name,omitempty"`
	HangupSource string `json:"hangup_source,omitempty"`
	ResourceURI  string `json:"resource_uri,omitempty"`
}

// LiveCall is a call in progress (GET Call/{uuid}/?status=live).
type LiveCall struct {
	RawBody
	CallUUID     string `json:"call_uuid"`
	CallStatus   string `json:"call_status,omitempty"`
	Direction    string `json:"direction,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	CallerName   string `json:"caller_name,omitempty"`
	SessionStart string `json:"session_start,omitempty"`
}

type CallList struct {
	RawBody
	APIID   string   `json:"api_id"`
	Meta    ListMeta `json:"meta"`
	Objects []Call   `json:"objects"`
}

// LiveCallList is the calls in progress (GET Call/?status=live): their UUIDs
// alone, not call objects.
type LiveCallList struct {
	RawBody
	APIID string   `json:"api_id"`
	Calls []string `json:"calls"`
}

// GenericResponse covers Plivo's typical "{message, api_id}" mutation response.
type GenericResponse struct {
	RawBody
	APIID   string `json:"api_id"`
	Message string `json:"message"`
}

// ScopedToken is the response shape from POST /v1/auth/token/.
// The Token field is populated only on creation; later reads omit it.
type ScopedToken struct {
	ID          string   `json:"id"`
	Token       string   `json:"token,omitempty"`
	Scopes      []string `json:"scopes"`
	Description string   `json:"description,omitempty"`
	ExpiresAt   string   `json:"expires_at"`
	CreatedAt   string   `json:"created_at,omitempty"`
	LastUsedAt  string   `json:"last_used_at,omitempty"`
}

// Recording — /Account/{id}/Recording/
type Recording struct {
	RawBody
	APIID               string `json:"api_id,omitempty"`
	RecordingID         string `json:"recording_id"`
	CallUUID            string `json:"call_uuid,omitempty"`
	ConferenceName      string `json:"conference_name,omitempty"`
	RecordingType       string `json:"recording_type,omitempty"`
	RecordingFormat     string `json:"recording_format,omitempty"`
	RecordingURL        string `json:"recording_url,omitempty"`
	RecordingDurationMS string `json:"recording_duration_ms,omitempty"`
	RecordingStartMS    string `json:"recording_start_ms,omitempty"`
	RecordingEndMS      string `json:"recording_end_ms,omitempty"`
	AddTime             string `json:"add_time,omitempty"`
	ResourceURI         string `json:"resource_uri,omitempty"`
}

type RecordingList struct {
	RawBody
	APIID   string      `json:"api_id"`
	Meta    ListMeta    `json:"meta"`
	Objects []Recording `json:"objects"`
}

// VerifySession — /Account/{id}/Verify/Session/
type VerifySession struct {
	RawBody
	APIID                    string `json:"api_id,omitempty"`
	SessionUUID              string `json:"session_uuid"`
	AppUUID                  string `json:"app_uuid,omitempty"`
	AlphaSender              string `json:"alpha_sender,omitempty"`
	Recipient                string `json:"recipient"`
	Channel                  string `json:"channel,omitempty"`
	Status                   string `json:"status,omitempty"`
	CountOfAttempts          int    `json:"count_of_attempts,omitempty"`
	RequestorIP              string `json:"requestor_ip,omitempty"`
	LocaleUsed               string `json:"locale_used,omitempty"`
	DestinationCountryISO2   string `json:"destination_country_iso2,omitempty"`
	DestinationCountryPrefix string `json:"destination_country_prefix,omitempty"`
	ChargeAmount             string `json:"charge_amount,omitempty"`
	ChargeAmountCurrency     string `json:"charge_amount_currency,omitempty"`
	CreationTime             string `json:"creation_time,omitempty"`
	ResourceURI              string `json:"resource_uri,omitempty"`
}

type VerifySessionList struct {
	RawBody
	APIID   string          `json:"api_id"`
	Meta    ListMeta        `json:"meta"`
	Objects []VerifySession `json:"objects"`
}

// LookupNumber — https://lookup.plivo.com/v1/Number/{number}?type=carrier
type LookupNumber struct {
	RawBody
	APIID       string `json:"api_id,omitempty"`
	PhoneNumber string `json:"phone_number"`
	Country     struct {
		Name string `json:"name,omitempty"`
		ISO2 string `json:"iso2,omitempty"`
		ISO3 string `json:"iso3,omitempty"`
	} `json:"country"`
	Format struct {
		E164          string `json:"e164,omitempty"`
		International string `json:"international,omitempty"`
		National      string `json:"national,omitempty"`
		RFC3966       string `json:"rfc3966,omitempty"`
	} `json:"format"`
	Carrier struct {
		Type              string `json:"type,omitempty"`
		Name              string `json:"name,omitempty"`
		MobileCountryCode string `json:"mobile_country_code,omitempty"`
		MobileNetworkCode string `json:"mobile_network_code,omitempty"`
		Ported            string `json:"ported,omitempty"`
	} `json:"carrier"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

// Subaccount — /Account/{id}/Subaccount/
type Subaccount struct {
	RawBody
	APIID       string `json:"api_id,omitempty"`
	AuthID      string `json:"auth_id"`
	Name        string `json:"name"`
	AuthToken   string `json:"auth_token,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
	Created     string `json:"created,omitempty"`
	Modified    string `json:"modified,omitempty"`
	Account     string `json:"account,omitempty"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

type SubaccountList struct {
	RawBody
	APIID   string       `json:"api_id"`
	Meta    ListMeta     `json:"meta"`
	Objects []Subaccount `json:"objects"`
}

// Endpoint — /Account/{id}/Endpoint/ (SIP endpoints)
type Endpoint struct {
	RawBody
	APIID       string `json:"api_id,omitempty"`
	EndpointID  string `json:"endpoint_id"`
	Username    string `json:"username"`
	Alias       string `json:"alias,omitempty"`
	SipURI      string `json:"sip_uri,omitempty"`
	Password    string `json:"password,omitempty"`
	Application string `json:"application,omitempty"` // the app's resource URI; there is no app_id
	Subaccount  string `json:"sub_account,omitempty"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

// ResolvedAppID returns the id of the attached application, from its URI.
func (e *Endpoint) ResolvedAppID() string { return lastSegment(e.Application) }

type EndpointList struct {
	RawBody
	APIID   string     `json:"api_id"`
	Meta    ListMeta   `json:"meta"`
	Objects []Endpoint `json:"objects"`
}

// CnamLookup — /Account/{id}/CnamLookup/{number}/
type CnamLookup struct {
	RawBody
	APIID      string `json:"api_id,omitempty"`
	Number     string `json:"number,omitempty"`
	CallerName string `json:"caller_name,omitempty"`
	CallerType string `json:"caller_type,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Charge     string `json:"charge,omitempty"`
}

// MaskingSession — /Account/{id}/MaskingSession/ (number-masking)
type MaskingSession struct {
	RawBody
	APIID         string `json:"api_id,omitempty"`
	SessionUUID   string `json:"session_uuid"`
	FirstParty    string `json:"first_party,omitempty"`
	SecondParty   string `json:"second_party,omitempty"`
	VirtualNumber string `json:"virtual_number,omitempty"`
	Status        string `json:"status,omitempty"`
	Mode          string `json:"mode,omitempty"`
	SessionExpiry int    `json:"session_expiry,omitempty"`
	CallTimeLimit int    `json:"call_time_limit,omitempty"`
	Recording     bool   `json:"record,omitempty"`
	CreatedOn     string `json:"created_on,omitempty"`
	ExpiresOn     string `json:"expires_on,omitempty"`
	ResourceURI   string `json:"resource_uri,omitempty"`
}

type MaskingSessionList struct {
	RawBody
	APIID   string           `json:"api_id"`
	Meta    ListMeta         `json:"meta"`
	Objects []MaskingSession `json:"objects"`
}

// Compliance — /Account/{id}/PhoneNumber/Compliance/ (unified number-compliance API).

// ComplianceRequiredField is one field a document type asks for.
type ComplianceRequiredField struct {
	FieldName    string `json:"field_name"`
	FriendlyName string `json:"friendly_name,omitempty"`
	FieldType    string `json:"field_type,omitempty"`
	Required     bool   `json:"required"`
	Format       string `json:"format,omitempty"`
	MinLength    int    `json:"min_length,omitempty"`
	MaxLength    int    `json:"max_length,omitempty"`
}

// ComplianceDocumentType is a document required for a jurisdiction.
type ComplianceDocumentType struct {
	DocumentTypeID string                    `json:"document_type_id"`
	Name           string                    `json:"name,omitempty"`
	Description    string                    `json:"description,omitempty"`
	ProofRequired  bool                      `json:"proof_required"`
	RequiredFields []ComplianceRequiredField `json:"required_fields,omitempty"`
}

// ComplianceRequirements — GET /PhoneNumber/Compliance/Requirements
type ComplianceRequirements struct {
	RawBody
	APIID         string                   `json:"api_id,omitempty"`
	RequirementID string                   `json:"requirement_id,omitempty"`
	CountryISO    string                   `json:"country_iso,omitempty"`
	NumberType    string                   `json:"number_type,omitempty"`
	UserType      string                   `json:"user_type,omitempty"`
	DocumentTypes []ComplianceDocumentType `json:"document_types,omitempty"`
}

// ComplianceEndUser — nested end-user object (expand=end_user).
type ComplianceEndUser struct {
	EndUserID string `json:"end_user_id,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
}

// ComplianceDoc — nested uploaded document (expand=documents).
type ComplianceDoc struct {
	DocumentID     string `json:"document_id,omitempty"`
	DocumentTypeID string `json:"document_type_id,omitempty"`
	FileName       string `json:"file_name,omitempty"`
	DownloadURL    string `json:"download_url,omitempty"`
}

// ComplianceLinkedNumber — nested linked number (expand=linked_numbers).
type ComplianceLinkedNumber struct {
	Number     string `json:"number,omitempty"`
	NumberType string `json:"number_type,omitempty"`
}

// ComplianceApplication — /Account/{id}/PhoneNumber/Compliance/{compliance_id}
type ComplianceApplication struct {
	RawBody
	APIID           string                   `json:"api_id,omitempty"`
	ComplianceID    string                   `json:"compliance_id,omitempty"`
	Alias           string                   `json:"alias,omitempty"`
	Status          string                   `json:"status,omitempty"`
	CountryISO      string                   `json:"country_iso,omitempty"`
	NumberType      string                   `json:"number_type,omitempty"`
	UserType        string                   `json:"user_type,omitempty"`
	CallbackURL     string                   `json:"callback_url,omitempty"`
	CallbackMethod  string                   `json:"callback_method,omitempty"`
	RejectionReason string                   `json:"rejection_reason,omitempty"`
	CreatedAt       string                   `json:"created_at,omitempty"`
	UpdatedAt       string                   `json:"updated_at,omitempty"`
	EndUser         *ComplianceEndUser       `json:"end_user,omitempty"`
	Documents       []ComplianceDoc          `json:"documents,omitempty"`
	LinkedNumbers   []ComplianceLinkedNumber `json:"linked_numbers,omitempty"`
}

// ComplianceApplicationList carries its rows under "compliances", not the
// "objects" the other lists use.
type ComplianceApplicationList struct {
	RawBody
	APIID   string                  `json:"api_id"`
	Meta    ListMeta                `json:"meta"`
	Objects []ComplianceApplication `json:"compliances"`
}

// ComplianceCreateResp — POST /PhoneNumber/Compliance/ (auto-submits).
type ComplianceCreateResp struct {
	RawBody
	APIID        string `json:"api_id,omitempty"`
	ComplianceID string `json:"compliance_id,omitempty"`
	Message      string `json:"message,omitempty"`
}

// ComplianceLinkReport — per-number result of a bulk link.
type ComplianceLinkReport struct {
	Number  string `json:"number,omitempty"`
	Status  string `json:"status,omitempty"`
	Remarks string `json:"remarks,omitempty"`
}

// ComplianceLinkResp — POST /PhoneNumber/Compliance/Link/
type ComplianceLinkResp struct {
	RawBody
	APIID        string                 `json:"api_id,omitempty"`
	TotalCount   int                    `json:"total_count,omitempty"`
	UpdatedCount int                    `json:"updated_count,omitempty"`
	Report       []ComplianceLinkReport `json:"report,omitempty"`
}

// Conference — /Account/{id}/Conference/
type ConferenceMember struct {
	MemberID   string `json:"member_id"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	CallerName string `json:"caller_name,omitempty"`
	CallUUID   string `json:"call_uuid"`
	Muted      bool   `json:"muted"`
	Deaf       bool   `json:"deaf"`
	JoinTime   string `json:"join_time,omitempty"`
	Direction  string `json:"direction,omitempty"`
}

type Conference struct {
	RawBody
	APIID                 string             `json:"api_id,omitempty"`
	ConferenceName        string             `json:"conference_name"`
	ConferenceRunTime     string             `json:"conference_run_time,omitempty"`
	ConferenceMemberCount string             `json:"conference_member_count,omitempty"`
	Members               []ConferenceMember `json:"members,omitempty"`
	ResourceURI           string             `json:"resource_uri,omitempty"`
}

// ConferenceList — list returns a flat array of names, not full Conference objects.
type ConferenceList struct {
	RawBody
	APIID       string   `json:"api_id"`
	Conferences []string `json:"conferences,omitempty"`
}

// MPC — /Account/{id}/MultiPartyCall/
type MPC struct {
	RawBody
	APIID        string `json:"api_id,omitempty"`
	MPCUUID      string `json:"mpc_uuid,omitempty"`
	FriendlyName string `json:"friendly_name,omitempty"`
	Status       string `json:"status,omitempty"`
	BillingType  string `json:"billing_type,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	EndAt        string `json:"end_at,omitempty"`
	ResourceURI  string `json:"resource_uri,omitempty"`
}

type MPCList struct {
	RawBody
	APIID   string   `json:"api_id"`
	Meta    ListMeta `json:"meta"`
	Objects []MPC    `json:"objects"`
}

type MPCParticipant struct {
	ParticipantID string `json:"member_id,omitempty"`
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
	CallerName    string `json:"caller_name,omitempty"`
	CallUUID      string `json:"call_uuid,omitempty"`
	Muted         bool   `json:"muted,omitempty"`
	OnHold        bool   `json:"hold,omitempty"`
	JoinTime      string `json:"join_time,omitempty"`
	Direction     string `json:"direction,omitempty"`
	Role          string `json:"role,omitempty"`
}

type MPCParticipantList struct {
	RawBody
	APIID   string           `json:"api_id"`
	Meta    ListMeta         `json:"meta"`
	Objects []MPCParticipant `json:"objects"`
}

// AudioStream — /Account/{id}/Call/{uuid}/Stream/
type AudioStream struct {
	RawBody
	StreamID      string `json:"stream_id,omitempty"`
	CallUUID      string `json:"call_uuid,omitempty"`
	StreamURL     string `json:"stream_url,omitempty"`
	Status        string `json:"status,omitempty"`
	AudioTrack    string `json:"audio_track,omitempty"`
	BiDirectional bool   `json:"bidirectional,omitempty"`
	StartTime     string `json:"start_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
	ResourceURI   string `json:"resource_uri,omitempty"`
}

type AudioStreamList struct {
	RawBody
	APIID   string        `json:"api_id,omitempty"`
	Meta    ListMeta      `json:"meta"`
	Objects []AudioStream `json:"objects,omitempty"`
}

// Brand10DLC is a 10DLC brand, as the Go SDK's Brand reads it: a list carries
// them under "brands" and a get nests one under "brand". The company details
// come from the profile the brand was registered with.
type Brand10DLC struct {
	BrandID            string           `json:"brand_id"`
	BrandAlias         string           `json:"brand_alias,omitempty"`
	BrandType          string           `json:"brand_type,omitempty"`
	ProfileUUID        string           `json:"profile_uuid,omitempty"`
	CompanyName        string           `json:"company_name,omitempty"`
	EntityType         string           `json:"entity_type,omitempty"`
	Vertical           string           `json:"vertical,omitempty"`
	Website            string           `json:"website,omitempty"`
	EIN                string           `json:"ein,omitempty"`
	EINIssuingCountry  string           `json:"ein_issuing_country,omitempty"`
	StockSymbol        string           `json:"stock_symbol,omitempty"`
	StockExchange      string           `json:"stock_exchange,omitempty"`
	RegistrationStatus string           `json:"registration_status,omitempty"`
	VettingStatus      string           `json:"vetting_status,omitempty"`
	VettingScore       FlexString       `json:"vetting_score,omitempty"`
	Address            Address10DLC     `json:"address"`
	AuthorizedContact  AuthContact10DLC `json:"authorized_contact"`
	CreatedAt          string           `json:"created_at,omitempty"`
}

// Address10DLC is a brand's or a profile's postal address.
type Address10DLC struct {
	Street     string `json:"street,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	Country    string `json:"country,omitempty"`
}

// AuthContact10DLC is the person a brand names as its authorized contact.
type AuthContact10DLC struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Title     string `json:"title,omitempty"`
	Seniority string `json:"seniority,omitempty"`
}

// Brand10DLCResponse is GET /10dlc/Brand/{brand_id}/.
type Brand10DLCResponse struct {
	RawBody
	APIID string     `json:"api_id,omitempty"`
	Brand Brand10DLC `json:"brand"`
}

type Brand10DLCList struct {
	RawBody
	APIID  string       `json:"api_id"`
	Meta   ListMeta     `json:"meta"`
	Brands []Brand10DLC `json:"brands"`
}

// Campaign10DLC is a 10DLC campaign, as the Go SDK's Campaign reads it: a
// list carries them under "campaigns", and a get or an update nests one under
// "campaign".
type Campaign10DLC struct {
	CampaignID             string                  `json:"campaign_id"`
	CampaignAlias          string                  `json:"campaign_alias,omitempty"`
	BrandID                string                  `json:"brand_id,omitempty"`
	Usecase                string                  `json:"usecase,omitempty"`
	SubUsecase             string                  `json:"sub_usecase,omitempty"` // comma-separated
	Vertical               string                  `json:"vertical,omitempty"`
	Description            string                  `json:"description,omitempty"`
	RegistrationStatus     string                  `json:"registration_status,omitempty"`
	CampaignSource         string                  `json:"campaign_source,omitempty"`
	ResellerID             string                  `json:"reseller_id,omitempty"`
	MessageFlow            string                  `json:"message_flow,omitempty"`
	Sample1                string                  `json:"sample1,omitempty"`
	Sample2                string                  `json:"sample2,omitempty"`
	HelpMessage            string                  `json:"help_message,omitempty"`
	HelpKeywords           string                  `json:"help_keywords,omitempty"`
	OptinMessage           string                  `json:"optin_message,omitempty"`
	OptinKeywords          string                  `json:"optin_keywords,omitempty"`
	OptoutMessage          string                  `json:"optout_message,omitempty"`
	OptoutKeywords         string                  `json:"optout_keywords,omitempty"`
	TermsAndConditionsLink string                  `json:"terms_and_conditions_link,omitempty"`
	PrivacyPolicyLink      string                  `json:"privacy_policy_link,omitempty"`
	CampaignAttributes     Campaign10DLCAttributes `json:"campaign_attributes"`
	// Throughput per carrier, kept as sent: T-Mobile's entry has a brand_tier
	// where the others have a tpm.
	MnoMetadata json.RawMessage `json:"mno_metadata,omitempty"`
	ErrorReason string          `json:"error_reason,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
}

// Campaign10DLCAttributes are a campaign's yes/no declarations.
type Campaign10DLCAttributes struct {
	EmbeddedLink       bool `json:"embedded_link"`
	EmbeddedPhone      bool `json:"embedded_phone"`
	AgeGated           bool `json:"age_gated"`
	DirectLending      bool `json:"direct_lending"`
	AffiliateMarketing bool `json:"affiliate_marketing"`
	SubscriberOptin    bool `json:"subscriber_optin"`
	SubscriberOptout   bool `json:"subscriber_optout"`
	SubscriberHelp     bool `json:"subscriber_help"`
}

// Campaign10DLCResponse is GET or POST /10dlc/Campaign/{campaign_id}/.
type Campaign10DLCResponse struct {
	RawBody
	APIID    string        `json:"api_id,omitempty"`
	Campaign Campaign10DLC `json:"campaign"`
}

type Campaign10DLCList struct {
	RawBody
	APIID     string          `json:"api_id"`
	Meta      ListMeta        `json:"meta"`
	Campaigns []Campaign10DLC `json:"campaigns"`
}

// CampaignNumbers10DLC is GET /10dlc/Campaign/{campaign_id}/Number/[{number}/]:
// the numbers linked to a campaign, each with its link's status.
type CampaignNumbers10DLC struct {
	RawBody
	APIID         string                `json:"api_id,omitempty"`
	CampaignID    string                `json:"campaign_id"`
	CampaignAlias string                `json:"campaign_alias,omitempty"`
	Usecase       string                `json:"usecase,omitempty"`
	PhoneNumbers  []CampaignNumber10DLC `json:"phone_numbers"`
}

type CampaignNumber10DLC struct {
	Number string `json:"number"`
	Status string `json:"status"`
}

// TollFreeVerification is a toll-free verification request, as the Go SDK's
// TollfreeVerification reads it: GET /TollfreeVerification/{uuid}/, or a row
// of the list.
type TollFreeVerification struct {
	RawBody
	APIID                  string `json:"api_id,omitempty"`
	UUID                   string `json:"uuid"`
	ProfileUUID            string `json:"profile_uuid,omitempty"`
	Number                 string `json:"number,omitempty"`
	Usecase                string `json:"usecase,omitempty"`
	UsecaseSummary         string `json:"usecase_summary,omitempty"`
	MessageSample          string `json:"message_sample,omitempty"`
	OptinType              string `json:"optin_type,omitempty"`
	OptinImageURL          string `json:"optin_image_url,omitempty"`
	Volume                 string `json:"volume,omitempty"`
	AdditionalInformation  string `json:"additional_information,omitempty"`
	TermsAndConditionsLink string `json:"terms_and_conditions_link,omitempty"`
	PrivacyPolicyLink      string `json:"privacy_policy_link,omitempty"`
	OptinMessage           string `json:"optin_message,omitempty"`
	HelpMessage            string `json:"help_message,omitempty"`
	ExtraData              string `json:"extra_data,omitempty"`
	CallbackURL            string `json:"callback_url,omitempty"`
	CallbackMethod         string `json:"callback_method,omitempty"`
	Status                 string `json:"status,omitempty"`
	ErrorMessage           string `json:"error_message,omitempty"`
	Created                string `json:"created,omitempty"`
	LastModified           string `json:"last_modified,omitempty"`
}

type TollFreeVerificationList struct {
	RawBody
	APIID   string                 `json:"api_id"`
	Meta    ListMeta               `json:"meta"`
	Objects []TollFreeVerification `json:"objects"`
}

// Powerpack — /Account/{id}/Powerpack/
type Powerpack struct {
	RawBody
	APIID           string `json:"api_id,omitempty"`
	UUID            string `json:"uuid"`
	Name            string `json:"name,omitempty"`
	StickySender    bool   `json:"sticky_sender,omitempty"`
	LocalConnect    bool   `json:"local_connect,omitempty"`
	ApplicationType string `json:"application_type,omitempty"`
	ApplicationID   string `json:"application_id,omitempty"`
	// An array of per-country priorities, as the API reference has it. Raw, so
	// a string or null decodes too.
	NumberPriority json.RawMessage `json:"number_priority,omitempty"`
	NumberPoolUUID string          `json:"number_pool,omitempty"`
	CreatedOn      string          `json:"created_on,omitempty"`
	ResourceURI    string          `json:"resource_uri,omitempty"`
}

type PowerpackList struct {
	RawBody
	APIID   string      `json:"api_id"`
	Meta    ListMeta    `json:"meta"`
	Objects []Powerpack `json:"objects"`
}

// PowerpackNumber — /Powerpack/{uuid}/Number/
type PowerpackNumber struct {
	Number      string `json:"number,omitempty"`
	Country     string `json:"country,omitempty"`
	Type        string `json:"type,omitempty"`
	AddedOn     string `json:"added_on,omitempty"`
	ResourceURI string `json:"resource_uri,omitempty"`
}

type PowerpackNumberList struct {
	RawBody
	APIID   string            `json:"api_id"`
	Meta    ListMeta          `json:"meta"`
	Objects []PowerpackNumber `json:"objects"`
}

// Buddy — /v1/aiassist/buddy-ext (Plivo's customer-facing AI assistant).
// Auth: HTTP Basic with the user's auth_id:auth_token; region is resolved
// server-side from the creds. Chat uses Server-Sent Events.

// BuddyAttachment represents a file uploaded with a buddy chat (base64 data: URL).
type BuddyAttachment struct {
	MediaType string `json:"mediaType"`
	Filename  string `json:"filename"`
	URL       string `json:"url"`
}

// BuddyTurn is one prior message in the conversation history.
type BuddyTurn struct {
	Role        string            `json:"role"` // "user" | "assistant"
	Text        string            `json:"text"`
	Attachments []BuddyAttachment `json:"attachments,omitempty"`
}

// BuddyUserContext personalises Buddy's responses with account context.
//
// Field shape mirrors the server's strict Pydantic model — notably, `plan` is
// an enum (free_trial / professional / enterprise) and a bare account_type
// (e.g. "standard") 400s the request, so the CLI omits it rather than
// guessing. Any unknown field on the server is silently dropped (extra:
// ignore), so a `callUUID` here would be a no-op; voice-debug context is
// carried inline in the chat message instead.
type BuddyUserContext struct {
	Email     string `json:"email,omitempty"`
	Plan      string `json:"plan,omitempty"`
	Region    string `json:"region,omitempty"`
	CountryID string `json:"countryId,omitempty"`
	Balance   string `json:"balance,omitempty"`
	Currency  string `json:"currency,omitempty"`
}

// BuddyChatRequest is the POST body for /v1/aiassist/buddy-ext/chat.
type BuddyChatRequest struct {
	Message     string            `json:"message"`
	History     []BuddyTurn       `json:"history,omitempty"`
	Attachments []BuddyAttachment `json:"attachments,omitempty"`
	UserContext BuddyUserContext  `json:"userContext"`
	PageURL     string            `json:"pageUrl,omitempty"`
	// SessionID groups the turns of one conversation for analytics. Minted by
	// the client (see newBuddySessionID in cmd/buddy.go) because the server
	// emits no `session` event to take it from, and replayed on follow-ups.
	SessionID string `json:"session_id,omitempty"`
}

// BuddyEscalation is one row from GET /v1/aiassist/buddy-ext/escalations.
type BuddyEscalation struct {
	UUID              string `json:"uuid,omitempty"`
	Status            string `json:"status,omitempty"`
	EscalationSummary string `json:"escalation_summary,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	PylonTicketID     string `json:"pylon_ticket_id,omitempty"`
	PylonContactID    string `json:"pylon_contact_id,omitempty"`
}

// BuddyEscalationsResponse is the standard Plivo envelope returned by the
// escalations endpoint: { api_id, status, data: { escalations: [...] } }.
type BuddyEscalationsResponse struct {
	APIID string `json:"api_id"`
	Data  struct {
		Escalations []BuddyEscalation `json:"escalations"`
	} `json:"data"`
}

// SIPTrunkCall is one SIP Trunking CDR — /Account/{id}/Zentrunk/Call/
//
// Field names follow the public API, which differs from the console's internal
// view: the cause is `hangup_cause_name` (plus a numeric `hangup_cause_code`),
// and there is no SIP-response or codec field here.
type SIPTrunkCall struct {
	RawBody
	CallUUID             string `json:"call_uuid"`
	CallID               string `json:"call_id,omitempty"` // SIP Call-ID
	FromNumber           string `json:"from_number,omitempty"`
	ToNumber             string `json:"to_number,omitempty"`
	CallDirection        string `json:"call_direction,omitempty"`
	CallDuration         int    `json:"call_duration"`
	BillDuration         int    `json:"bill_duration"`
	BilledDuration       int    `json:"billed_duration"`
	InitiationTime       string `json:"initiation_time,omitempty"`
	AnswerTime           string `json:"answer_time,omitempty"`
	EndTime              string `json:"end_time,omitempty"`
	HangupCauseName      string `json:"hangup_cause_name,omitempty"`
	HangupCauseCode      int    `json:"hangup_cause_code"`
	HangupSource         string `json:"hangup_source,omitempty"`
	TrunkDomain          string `json:"trunk_domain,omitempty"`
	FromCountry          string `json:"from_country,omitempty"`
	ToCountry            string `json:"to_country,omitempty"`
	TransportProtocol    string `json:"transport_protocol,omitempty"`
	SRTP                 bool   `json:"srtp"`
	SecureTrunking       bool   `json:"secure_trunking"`
	SecureTrunkingRate   string `json:"secure_trunking_rate,omitempty"`
	STIRVerification     string `json:"stir_verification,omitempty"`
	AttestationIndicator string `json:"attestation_indicator,omitempty"`
	TotalRate            string `json:"total_rate,omitempty"`
	TotalAmount          string `json:"total_amount,omitempty"`
	CnamLookup           bool   `json:"cnam_lookup"`
	CnamLookupRate       string `json:"cnam_lookup_rate,omitempty"`
}

type SIPTrunkCallList struct {
	RawBody
	APIID   string         `json:"api_id"`
	Meta    ListMeta       `json:"meta"`
	Objects []SIPTrunkCall `json:"objects"`
}

// FlexString takes a scalar whether the API sends it as a string, a number or
// null, and keeps it as sent: 24, "24" and 1000.0 read back as 24, 24 and
// 1000.0. A plain string field would fail the whole decode on a number.
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(b)
	return nil
}

// SIPCallInsights is the quality record of one SIP Trunking call —
// /Account/{id}/Zentrunk/Call/{call_uuid}/Insights/
//
// The docs type the metrics as strings except post_dial_delay (a float), so
// every scalar is a FlexString: a wrong guess must not lose the whole record.
type SIPCallInsights struct {
	RawBody
	APIID             FlexString         `json:"api_id"`
	CallUUID          FlexString         `json:"call_uuid"`
	From              SIPCallInsightsLeg `json:"from"`
	To                SIPCallInsightsLeg `json:"to"`
	HangupCause       FlexString         `json:"hangup_cause"`
	HangupSource      FlexString         `json:"hangup_source"`
	RTT               FlexString         `json:"rtt"`
	Jitter            FlexString         `json:"jitter"`
	PacketLoss        FlexString         `json:"packet_loss"`
	PostDialDelay     FlexString         `json:"post_dial_delay"`
	PlivoQualityScore FlexString         `json:"plivo_quality_score"`
}

// SIPCallInsightsLeg is one side of the call as Insights reports it.
type SIPCallInsightsLeg struct {
	Carrier FlexString `json:"carrier"`
	Number  FlexString `json:"number"`
	Region  FlexString `json:"region"`
}

// SIPTrunk is one trunk — /Account/{id}/Zentrunk/Trunk/
type SIPTrunk struct {
	RawBody
	TrunkID         string `json:"trunk_id"`
	Name            string `json:"name,omitempty"`
	TrunkDomain     string `json:"trunk_domain,omitempty"`
	TrunkStatus     string `json:"trunk_status,omitempty"`
	TrunkDirection  string `json:"trunk_direction,omitempty"`
	Secure          bool   `json:"secure"`
	IPACLUUID       string `json:"ipacl_uuid,omitempty"`
	CredentialUUID  string `json:"credential_uuid,omitempty"`
	PrimaryURIUUID  string `json:"primary_uri_uuid,omitempty"`
	FallbackURIUUID string `json:"fallback_uri_uuid,omitempty"`
}

type SIPTrunkList struct {
	RawBody
	APIID   string     `json:"api_id"`
	Meta    ListMeta   `json:"meta"`
	Objects []SIPTrunk `json:"objects"`
}

// SIPTrunkACL is one IP access control list —
// /Account/{id}/Zentrunk/IPAccessControlList/
type SIPTrunkACL struct {
	RawBody
	IPACLUUID   string   `json:"ipacl_uuid"`
	Name        string   `json:"name,omitempty"`
	IPAddresses []string `json:"ip_addresses,omitempty"`
}

type SIPTrunkACLList struct {
	RawBody
	APIID   string        `json:"api_id"`
	Meta    ListMeta      `json:"meta"`
	Objects []SIPTrunkACL `json:"objects"`
}

// SIPTrunkURI is one origination URI — /Account/{id}/Zentrunk/URI/
// sip_user is returned by the API today even though it is not in the public docs.
type SIPTrunkURI struct {
	RawBody
	URIUUID              string `json:"uri_uuid"`
	Name                 string `json:"name,omitempty"`
	URI                  string `json:"uri,omitempty"`
	AuthenticationNeeded bool   `json:"authentication_needed"`
	Username             string `json:"username,omitempty"`
	SipUser              string `json:"sip_user,omitempty"`
}

type SIPTrunkURIList struct {
	RawBody
	APIID   string        `json:"api_id"`
	Meta    ListMeta      `json:"meta"`
	Objects []SIPTrunkURI `json:"objects"`
}

// SIPTrunkCredential is one credential — /Account/{id}/Zentrunk/Credential/
// The API never returns the password, so neither does this.
type SIPTrunkCredential struct {
	RawBody
	CredentialUUID string `json:"credential_uuid"`
	Name           string `json:"name,omitempty"`
	Username       string `json:"username,omitempty"`
}

type SIPTrunkCredentialList struct {
	RawBody
	APIID   string               `json:"api_id"`
	Meta    ListMeta             `json:"meta"`
	Objects []SIPTrunkCredential `json:"objects"`
}

// UsageSummary — /Account/{id}/UsageSummary/: usage and spend over a date
// window, one usage row per period and dimension. Paged by next_page_token;
// the totals and other_charges come on the first page only. Amounts are
// numbers, and a field the API leaves out stays nil.
type UsageSummary struct {
	RawBody
	APIID        string             `json:"api_id"`
	Meta         UsageSummaryMeta   `json:"meta"`
	Usage        []UsageRow         `json:"usage"`
	OtherCharges []UsageOtherCharge `json:"other_charges"`
}

type UsageSummaryMeta struct {
	AuthID               string             `json:"auth_id"`
	SubaccountAuth       string             `json:"subaccount_auth,omitempty"`
	Granularity          string             `json:"granularity"`
	FromDate             string             `json:"from_date"`
	ToDate               string             `json:"to_date"`
	Currency             string             `json:"currency"`
	PageSize             int                `json:"page_size"`
	NextPageToken        string             `json:"next_page_token,omitempty"`
	TotalSpend           *float64           `json:"total_spend,omitempty"`
	TotalSubaccountSpend *float64           `json:"total_subaccount_spend,omitempty"`
	SubaccountSpend      map[string]float64 `json:"subaccount_spend,omitempty"`
}

// UsageRow is one period of one product. The API splits rows by subaccount
// and by error code or hangup cause, so two rows can share every other field.
type UsageRow struct {
	FromDate        string     `json:"from_date"`
	ToDate          string     `json:"to_date"`
	Product         string     `json:"product"`
	Type            string     `json:"type"`
	SubaccountAuth  string     `json:"subaccount_auth,omitempty"`
	Country         string     `json:"country,omitempty"`
	Direction       string     `json:"direction,omitempty"`
	TotalUnits      *float64   `json:"total_units"`
	DurationSeconds *float64   `json:"duration_seconds,omitempty"`
	TotalAmount     *float64   `json:"total_amount"`
	Surcharge       *float64   `json:"surcharge,omitempty"`
	ErrorCode       FlexString `json:"error_code,omitempty"`
	HangupCause     FlexString `json:"hangup_cause,omitempty"`
	ErrorReason     string     `json:"error_reason,omitempty"`
}

// UsageOtherCharge is a charge outside telecom traffic: number rentals,
// lookups, fees, taxes and adjustments.
type UsageOtherCharge struct {
	FromDate    string   `json:"from_date"`
	ToDate      string   `json:"to_date"`
	Description string   `json:"description"`
	TotalUnits  *float64 `json:"total_units"`
	TotalAmount *float64 `json:"total_amount"`
}
