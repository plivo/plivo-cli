package wsproxy

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
)

// Plivo's <Stream> WebSocket uses JSON messages per RFC 8228-ish frame
// envelopes. We model only the subset `plivo voice streams test` needs:
// `start` once, N `media` frames, `stop` once. start and media follow the
// documented frames field for field: `plivo docs show
// voice-agents/audio-streaming/concepts/audio-streaming-reference`.

// StartFrame is the first message we send to identify the stream.
type StartFrame struct {
	Event          string     `json:"event"`          // always "start"
	SequenceNumber int        `json:"sequenceNumber"` // always 1
	Start          StartInner `json:"start"`
	ExtraHeaders   string     `json:"extra_headers"` // the <Stream> extraHeaders; none here
}

type StartInner struct {
	CallID      string      `json:"callId"`
	StreamID    string      `json:"streamId"`
	AccountID   string      `json:"accountId"`
	Tracks      []string    `json:"tracks"`
	MediaFormat MediaFormat `json:"mediaFormat"`
}

type MediaFormat struct {
	Encoding   string `json:"encoding"`   // "audio/x-mulaw" | "audio/x-l16"
	SampleRate int    `json:"sampleRate"` // 8000 | 16000
}

// MediaFrame carries one audio chunk. Payload is base64-encoded mulaw
// (or l16) for the chunk's duration.
type MediaFrame struct {
	Event          string     `json:"event"` // always "media"
	SequenceNumber int        `json:"sequenceNumber"`
	StreamID       string     `json:"streamId"`
	Media          MediaInner `json:"media"`
	ExtraHeaders   string     `json:"extra_headers"`
}

type MediaInner struct {
	Track     string `json:"track"`     // "inbound": the caller's audio
	Timestamp string `json:"timestamp"` // Unix time in milliseconds, as a string
	Chunk     int    `json:"chunk"`     // the track's chunk number, from 1
	Payload   string `json:"payload"`   // base64 audio bytes
}

// StopFrame signals end-of-stream.
type StopFrame struct {
	Event string `json:"event"` // always "stop"
}

// EncodeStart marshals a start frame for the given identifiers + format, for
// one inbound track.
func EncodeStart(streamID, callID, accountID string, mf MediaFormat) ([]byte, error) {
	return json.Marshal(StartFrame{
		Event:          "start",
		SequenceNumber: 1,
		Start: StartInner{
			CallID:      callID,
			StreamID:    streamID,
			AccountID:   accountID,
			Tracks:      []string{"inbound"},
			MediaFormat: mf,
		},
	})
}

// EncodeMedia marshals inbound chunk number chunk (from 1), sent at
// unixMs, whose payload is base64-encoded audio. Start is event 1, so with
// one track the chunk is event chunk+1.
func EncodeMedia(streamID string, chunk int, unixMs int64, audio []byte) ([]byte, error) {
	return json.Marshal(MediaFrame{
		Event:          "media",
		SequenceNumber: chunk + 1,
		StreamID:       streamID,
		Media: MediaInner{
			Track:     "inbound",
			Timestamp: strconv.FormatInt(unixMs, 10),
			Chunk:     chunk,
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
