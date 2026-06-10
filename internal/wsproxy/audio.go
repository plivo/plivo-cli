package wsproxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
)

// Plivo's <Stream> WebSocket uses JSON messages per RFC 8228-ish frame
// envelopes. We model only the subset `plivo voice streams test` needs:
// `start` once, N `media` frames, `stop` once.
//
// Reference: api.plivo.com/v1/Account/{}/Stream + the <Stream> PlivoXML
// element. Mediaformat defaults to audio/x-mulaw 8000Hz mono.

// StartFrame is the first message we send to identify the stream.
type StartFrame struct {
	Event string     `json:"event"` // always "start"
	Start StartInner `json:"start"`
}

type StartInner struct {
	StreamID    string      `json:"streamId"`
	CallID      string      `json:"callId"`
	AccountID   string      `json:"accountId"`
	MediaFormat MediaFormat `json:"mediaFormat"`
}

type MediaFormat struct {
	Encoding   string `json:"encoding"`   // "audio/x-mulaw" | "audio/l16"
	SampleRate int    `json:"sampleRate"` // 8000 | 16000
	Channels   int    `json:"channels"`   // 1
}

// MediaFrame carries one audio chunk. Payload is base64-encoded mulaw
// (or l16) for the chunk's duration.
type MediaFrame struct {
	Event string     `json:"event"` // always "media"
	Media MediaInner `json:"media"`
}

type MediaInner struct {
	Track     string `json:"track"`     // "inbound" | "outbound"
	Chunk     string `json:"chunk"`     // sequence number, stringified
	Timestamp string `json:"timestamp"` // milliseconds since stream start
	Payload   string `json:"payload"`   // base64 audio bytes
}

// StopFrame signals end-of-stream.
type StopFrame struct {
	Event string `json:"event"` // always "stop"
}

// EncodeStart marshals a start frame for the given identifiers + format.
func EncodeStart(streamID, callID, accountID string, mf MediaFormat) ([]byte, error) {
	return json.Marshal(StartFrame{
		Event: "start",
		Start: StartInner{
			StreamID:    streamID,
			CallID:      callID,
			AccountID:   accountID,
			MediaFormat: mf,
		},
	})
}

// EncodeMedia marshals a media frame whose payload is base64-encoded audio.
func EncodeMedia(track string, chunk, timestampMs int, audio []byte) ([]byte, error) {
	return json.Marshal(MediaFrame{
		Event: "media",
		Media: MediaInner{
			Track:     track,
			Chunk:     intStr(chunk),
			Timestamp: intStr(timestampMs),
			Payload:   base64.StdEncoding.EncodeToString(audio),
		},
	})
}

// EncodeStop marshals the stop frame.
func EncodeStop() ([]byte, error) {
	return json.Marshal(StopFrame{Event: "stop"})
}

// SyntheticMulaw generates `frames` chunks of mulaw audio, each `frameMs`
// long at `sampleRateHz`. The waveform is a 1kHz sine; payload is just
// for shape, not audio quality. Returns one []byte per frame.
//
// frameMs=20 at 8000Hz → 160 samples per frame (Plivo's typical chunk size).
func SyntheticMulaw(frames, frameMs, sampleRateHz int) [][]byte {
	samplesPerFrame := frameMs * sampleRateHz / 1000
	out := make([][]byte, frames)
	for f := 0; f < frames; f++ {
		buf := make([]byte, samplesPerFrame)
		for i := 0; i < samplesPerFrame; i++ {
			t := float64(f*samplesPerFrame+i) / float64(sampleRateHz)
			pcm := int16(math.Sin(2*math.Pi*1000*t) * 16384) // 1kHz tone at half-scale
			buf[i] = pcmToMulaw(pcm)
		}
		out[f] = buf
	}
	return out
}

// pcmToMulaw converts a 16-bit PCM sample to 8-bit G.711 mu-law.
// Standard ITU-T mu-law encoder, no dependencies.
func pcmToMulaw(pcm int16) byte {
	const bias = 0x84
	const clip = 32635
	sign := byte(0)
	if pcm < 0 {
		pcm = -pcm
		sign = 0x80
	}
	if pcm > clip {
		pcm = clip
	}
	pcm += bias
	exp := byte(7)
	for mask := int16(0x4000); pcm&mask == 0 && exp > 0; mask >>= 1 {
		exp--
	}
	mant := byte((pcm >> (exp + 3)) & 0x0F)
	return ^(sign | (exp << 4) | mant)
}

// intStr is faster than fmt.Sprintf("%d", ...) for the hot path; not
// load-bearing here but keeps the dependency surface clean.
func intStr(n int) string { return fmt.Sprintf("%d", n) }
