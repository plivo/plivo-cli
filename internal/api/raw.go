package api

import (
	"encoding/json"

	"github.com/plivo/plivo-cli/internal/redact"
)

// ShowSecrets keeps credential values in what -o json prints. Off, the
// default, Visible hides them: subaccount reads return auth tokens, endpoint
// reads a password hash, and -o json output lands in tickets, logs and agent
// transcripts. Process-global like CLICommand; cmd sets it from --show-secrets.
var ShowSecrets bool

// Visible returns a response body as -o json may print it: with each
// credential value replaced (see redact.Secrets) unless ShowSecrets.
func Visible(body []byte) []byte {
	if ShowSecrets {
		return body
	}
	return redact.Secrets(body)
}

// RawCapturer is implemented by response types that retain the upstream bytes.
// Lets `-o json` echo the API response instead of re-marshalling a struct that
// only maps the fields we happen to have tagged.
type RawCapturer interface {
	SetRaw([]byte)
	Raw() json.RawMessage
}

// RawBody is embedded in response types to satisfy RawCapturer. The field is
// unexported, so encoding/json neither reads nor writes it and the embedding is
// invisible to callers that marshal the outer struct.
type RawBody struct {
	raw json.RawMessage
}

// SetRaw stores a copy, so the caller stays free to reuse its buffer, with
// credential values hidden (see Visible).
func (r *RawBody) SetRaw(b []byte) {
	if len(b) == 0 {
		r.raw = nil
		return
	}
	r.raw = append(json.RawMessage(nil), Visible(b)...)
}

func (r *RawBody) Raw() json.RawMessage { return r.raw }

// RawBodyWithSecrets is RawBody keeping credential values, for the response
// whose credential is the point of the command: subaccounts create hands over
// the new subaccount's auth token.
type RawBodyWithSecrets struct {
	raw json.RawMessage
}

func (r *RawBodyWithSecrets) SetRaw(b []byte)      { r.raw = append(json.RawMessage(nil), b...) }
func (r *RawBodyWithSecrets) Raw() json.RawMessage { return r.raw }
