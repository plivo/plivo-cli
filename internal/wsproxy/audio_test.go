package wsproxy

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestEncodeStart_shape(t *testing.T) {
	raw, err := EncodeStart("s1", "c1", "a1", MediaFormat{
		Encoding: "audio/x-mulaw", SampleRate: 8000, Channels: 1,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got StartFrame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Event != "start" {
		t.Errorf("event = %q, want start", got.Event)
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
	raw, err := EncodeMedia("inbound", 5, 100, audio)
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
	if got.Media.Chunk != "5" || got.Media.Timestamp != "100" || got.Media.Track != "inbound" {
		t.Errorf("media inner lost: %+v", got.Media)
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
