package wsproxy

import (
	"fmt"
	"math"
	"strings"
)

// The XML attribute and the WebSocket JSON frames spell the audio format
// differently, which is what made this easy to get wrong:
//
//	<Stream contentType="audio/x-mulaw;rate=8000">   — one combined value
//	{"mediaFormat":{"encoding":"audio/x-mulaw","sampleRate":8000}}
//
// There is no sampleRate attribute on <Stream>. Both spellings are derived
// here so the two commands cannot drift apart again.

// Codec values accepted by --codec.
const (
	CodecMulaw = "mulaw"
	CodecL16   = "l16"
)

// Codecs lists the selectable values, for help text and validation.
var Codecs = []string{CodecMulaw, CodecL16}

// Encoding returns the bare MIME type for the JSON mediaFormat.encoding
// field, where the rate travels separately in sampleRate.
func Encoding(codec string) string {
	if strings.EqualFold(strings.TrimSpace(codec), CodecL16) {
		return "audio/x-l16"
	}
	return "audio/x-mulaw"
}

// ContentType returns the value the <Stream> contentType attribute wants:
// codec and rate joined, e.g. "audio/x-mulaw;rate=8000".
func ContentType(codec string, rateHz int) string {
	return fmt.Sprintf("%s;rate=%d", Encoding(codec), rateHz)
}

// SupportedContentTypes is the exact set the platform accepts. mu-law is
// 8kHz only; there is no mu-law 16kHz stream.
var SupportedContentTypes = []string{
	"audio/x-mulaw;rate=8000",
	"audio/x-l16;rate=8000",
	"audio/x-l16;rate=16000",
}

// ValidateCodecRate rejects combinations the platform would refuse, so the
// error surfaces locally instead of as a dropped stream mid-call.
func ValidateCodecRate(codec string, rateHz int) error {
	c := strings.ToLower(strings.TrimSpace(codec))
	if c != CodecMulaw && c != CodecL16 {
		return fmt.Errorf("must be %s", strings.Join(Codecs, " | "))
	}
	want := ContentType(c, rateHz)
	for _, ok := range SupportedContentTypes {
		if want == ok {
			return nil
		}
	}
	return fmt.Errorf("%s is not a supported combination (want one of: %s)",
		want, strings.Join(SupportedContentTypes, ", "))
}

// SyntheticAudio generates `frames` chunks of `frameMs` audio in the wire
// format `codec` actually names. Sending mu-law bytes under an l16 header
// makes a test pass while the endpoint receives noise, so the generator has
// to follow the codec.
func SyntheticAudio(codec string, frames, frameMs, sampleRateHz int) [][]byte {
	if strings.EqualFold(strings.TrimSpace(codec), CodecL16) {
		return SyntheticL16(frames, frameMs, sampleRateHz)
	}
	return SyntheticMulaw(frames, frameMs, sampleRateHz)
}

// SyntheticL16 generates linear 16-bit PCM, little-endian, mono: the same
// 1kHz tone as SyntheticMulaw but two bytes per sample rather than one.
func SyntheticL16(frames, frameMs, sampleRateHz int) [][]byte {
	samplesPerFrame := frameMs * sampleRateHz / 1000
	out := make([][]byte, frames)
	for f := 0; f < frames; f++ {
		buf := make([]byte, samplesPerFrame*2)
		for i := 0; i < samplesPerFrame; i++ {
			t := float64(f*samplesPerFrame+i) / float64(sampleRateHz)
			pcm := int16(math.Sin(2*math.Pi*1000*t) * 16384)
			// Little-endian two's complement. Masking keeps each half
			// provably in range, as pcmToMulaw does.
			buf[i*2] = byte(pcm & 0xFF)
			buf[i*2+1] = byte((pcm >> 8) & 0xFF)
		}
		out[f] = buf
	}
	return out
}
