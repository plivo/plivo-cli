package wsproxy

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeStart_shape(t *testing.T) {
	raw, err := EncodeStart("s1", "c1", "a1", MediaFormat{Encoding: "audio/x-mulaw", SampleRate: 8000})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got StartFrame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "start" || got.SequenceNumber != 1 || len(got.Start.Tracks) != 1 {
		t.Errorf("event %q, sequenceNumber %d, tracks %v; want start, 1, [inbound]", got.Event, got.SequenceNumber, got.Start.Tracks)
	}
	if got.Start.StreamID != "s1" || got.Start.CallID != "c1" || got.Start.AccountID != "a1" {
		t.Errorf("ids lost: %+v", got.Start)
	}
	if got.Start.MediaFormat.Encoding != "audio/x-mulaw" || got.Start.MediaFormat.SampleRate != 8000 {
		t.Errorf("mediaFormat lost: %+v", got.Start.MediaFormat)
	}
}

func TestEncodeMedia_payloadIsBase64(t *testing.T) {
	audio := []byte{0xff, 0x7f, 0x00, 0x80}
	raw, err := EncodeMedia("s1", 5, 100, audio)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got MediaFrame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "media" {
		t.Errorf("event = %q, want media", got.Event)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Media.Payload)
	if err != nil {
		t.Fatalf("payload not valid base64: %v", err)
	}
	if string(decoded) != string(audio) {
		t.Errorf("payload round-trip lost data: got %v want %v", decoded, audio)
	}
	if got.Media.Chunk != 5 || got.Media.Timestamp != "100" || got.Media.Track != "inbound" || got.SequenceNumber != 6 || got.StreamID != "s1" {
		t.Errorf("media frame lost a field: %+v", got)
	}
}

// Plivo's documented start and media frames: `plivo docs show
// voice-agents/audio-streaming/concepts/audio-streaming-reference`.
const (
	docStart = `{"event": "start", "sequenceNumber": 1, "start": {"callId": "12345678-1234-1234-1234-123456789abc",
		"streamId": "87654321-4321-4321-4321-cba987654321", "accountId": "MAXXXXXXXXXXXXXXXXXX", "tracks": ["inbound"],
		"mediaFormat": {"encoding": "audio/x-mulaw", "sampleRate": 8000}}, "extra_headers": "userId=12345;sessionId=abc-xyz"}`
	docMedia = `{"event": "media", "sequenceNumber": 42, "streamId": "87654321-4321-4321-4321-cba987654321",
		"media": {"track": "inbound", "timestamp": "1705312200000", "chunk": 41, "payload": "//uQxAAAAAANIAAAAAExBTUUzLjEwMFVV"},
		"extra_headers": "userId=12345;sessionId=abc-xyz"}`
)

// Each documented frame decodes into ours with no field unknown or mistyped,
// and encodes back unchanged, so ours carry nothing Plivo's lack.
func TestFramesMatchPlivosDocumentedShape(t *testing.T) {
	for doc, frame := range map[string]any{docStart: &StartFrame{}, docMedia: &MediaFrame{}} {
		dec := json.NewDecoder(strings.NewReader(doc))
		dec.DisallowUnknownFields()
		err := dec.Decode(frame)
		back, _ := json.Marshal(frame)
		var want, got any
		_ = json.Unmarshal([]byte(doc), &want)
		_ = json.Unmarshal(back, &got)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%T does not fit Plivo's frame (%v):\n got %s\nwant %s", frame, err, back, doc)
		}
	}
}

func TestEncodeStop_shape(t *testing.T) {
	raw, err := EncodeStop()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got StopFrame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "stop" {
		t.Errorf("event = %q, want stop", got.Event)
	}
}

func TestSyntheticMulaw_correctSize(t *testing.T) {
	// 20ms frames at 8000 Hz → 160 samples = 160 bytes per frame
	frames := SyntheticMulaw(3, 20, 8000)
	if len(frames) != 3 {
		t.Errorf("frame count = %d, want 3", len(frames))
	}
	for i, f := range frames {
		if len(f) != 160 {
			t.Errorf("frame %d len = %d, want 160", i, len(f))
		}
	}
}

func TestSyntheticMulaw_16khz_rateMath(t *testing.T) {
	// 20ms at 16kHz → 320 samples
	frames := SyntheticMulaw(2, 20, 16000)
	for i, f := range frames {
		if len(f) != 320 {
			t.Errorf("frame %d at 16kHz = %d bytes, want 320", i, len(f))
		}
	}
}

func TestPCMToMulaw_silenceMapsToHighBit(t *testing.T) {
	// PCM 0 → mulaw 0x7F or 0xFF depending on sign; just check it doesn't
	// blow up + isn't 0 (which would be a degenerate value).
	got := pcmToMulaw(0)
	if got == 0 {
		t.Errorf("pcm 0 produced 0x00 (likely a bug)")
	}
}

func TestPCMToMulaw_clipsLoudInput(t *testing.T) {
	// 32767 should clip to the mulaw equivalent of 32635 max, not overflow.
	got := pcmToMulaw(32767)
	if got == 0 {
		t.Error("loud input produced 0 (likely overflow)")
	}
}
