package wsproxy

import (
	"strings"
	"testing"
)

func TestEncoding_isTheXPrefixedForm(t *testing.T) {
	// "audio/l16" is not a value the platform recognises; it must be x-l16.
	cases := map[string]string{
		"mulaw": "audio/x-mulaw", "MULAW": "audio/x-mulaw",
		"l16": "audio/x-l16", "L16": "audio/x-l16", " l16 ": "audio/x-l16",
		"": "audio/x-mulaw", "opus": "audio/x-mulaw",
	}
	for in, want := range cases {
		if got := Encoding(in); got != want {
			t.Errorf("Encoding(%q) = %q, want %q", in, got, want)
		}
	}
}

// The XML attribute carries the rate inside contentType; a separate
// sampleRate attribute is not part of the contract.
func TestContentType_joinsCodecAndRate(t *testing.T) {
	if got := ContentType("mulaw", 8000); got != "audio/x-mulaw;rate=8000" {
		t.Errorf("got %q", got)
	}
	if got := ContentType("l16", 16000); got != "audio/x-l16;rate=16000" {
		t.Errorf("got %q", got)
	}
}

// Every value we can emit must be one the platform actually accepts.
func TestContentType_everyValidComboIsSupported(t *testing.T) {
	for _, c := range Codecs {
		for _, r := range []int{8000, 16000} {
			err := ValidateCodecRate(c, r)
			ct := ContentType(c, r)
			listed := false
			for _, s := range SupportedContentTypes {
				if s == ct {
					listed = true
				}
			}
			if (err == nil) != listed {
				t.Errorf("%s: validate err=%v but listed=%v", ct, err, listed)
			}
		}
	}
}

func TestValidateCodecRate(t *testing.T) {
	if err := ValidateCodecRate("mulaw", 8000); err != nil {
		t.Errorf("mulaw 8k should pass: %v", err)
	}
	// There is no mu-law 16kHz stream.
	if err := ValidateCodecRate("mulaw", 16000); err == nil {
		t.Error("mulaw 16k should be rejected")
	}
	if err := ValidateCodecRate("l16", 8000); err != nil {
		t.Errorf("l16 8k should pass: %v", err)
	}
	if err := ValidateCodecRate("l16", 16000); err != nil {
		t.Errorf("l16 16k should pass: %v", err)
	}
	err := ValidateCodecRate("opus", 8000)
	if err == nil {
		t.Fatal("unknown codec should be rejected")
	}
	if !strings.Contains(err.Error(), "mulaw") {
		t.Errorf("error should name the valid codecs, got %v", err)
	}
	// The message should show what was actually asked for.
	if err := ValidateCodecRate("mulaw", 16000); err == nil || !strings.Contains(err.Error(), "audio/x-mulaw;rate=16000") {
		t.Errorf("error should quote the rejected value, got %v", err)
	}
}

// The bug this guards: SyntheticAudio used to return mu-law for every
// codec, so an l16 run announced 16-bit PCM and sent 8-bit bytes. Frame
// size is the tell — l16 is two bytes per sample, mu-law is one.
func TestSyntheticAudio_frameSizeFollowsCodec(t *testing.T) {
	const frameMs = 20

	mu := SyntheticAudio("mulaw", 3, frameMs, 8000)
	if len(mu) != 3 {
		t.Fatalf("got %d frames", len(mu))
	}
	if len(mu[0]) != 160 { // 20ms * 8000Hz / 1000 * 1 byte
		t.Errorf("mulaw 8k frame = %d bytes, want 160", len(mu[0]))
	}

	l16 := SyntheticAudio("l16", 3, frameMs, 16000)
	if len(l16[0]) != 640 { // 320 samples * 2 bytes
		t.Errorf("l16 16k frame = %d bytes, want 640", len(l16[0]))
	}

	l8 := SyntheticAudio("l16", 2, frameMs, 8000)
	if len(l8[0]) != 320 { // 160 samples * 2 bytes
		t.Errorf("l16 8k frame = %d bytes, want 320", len(l8[0]))
	}

	// Same rate, different codec: l16 must be exactly twice mu-law.
	if len(SyntheticAudio("l16", 1, frameMs, 8000)[0]) != 2*len(SyntheticAudio("mulaw", 1, frameMs, 8000)[0]) {
		t.Error("l16 should be 2 bytes per sample against mulaw's 1")
	}
}

// A silent all-zero buffer would also satisfy the size check, so confirm
// the tone is really there and little-endian.
func TestSyntheticL16_carriesSignalLittleEndian(t *testing.T) {
	frames := SyntheticL16(1, 20, 8000)
	buf := frames[0]

	nonZero := 0
	for _, b := range buf {
		if b != 0 {
			nonZero++
		}
	}
	if nonZero < len(buf)/4 {
		t.Errorf("only %d/%d bytes non-zero — buffer looks like silence", nonZero, len(buf))
	}

	// A 1kHz tone at 8kHz sampling is 8 samples per cycle; sample 2 sits at
	// the positive peak (sin(pi/2)) and must be near +16384.
	got := int16(uint16(buf[4]) | uint16(buf[5])<<8)
	if got < 16000 || got > 16384 {
		t.Errorf("sample 2 = %d, want the positive peak near 16384 (little-endian?)", got)
	}
}
