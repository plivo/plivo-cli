package api

import "encoding/json"

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

// SetRaw stores a copy, so the caller stays free to reuse its buffer.
func (r *RawBody) SetRaw(b []byte) {
	if len(b) == 0 {
		r.raw = nil
		return
	}
	r.raw = append(json.RawMessage(nil), b...)
}

func (r *RawBody) Raw() json.RawMessage { return r.raw }
