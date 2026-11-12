package cmd

// r4TtsCases are the tts speak options added in v1.3.4, run on the wire to a
// local server: nothing is billed, and --dry-run shows what would be sent.
func r4TtsCases() []qaCase {
	return []qaCase{
		{
			id: "r4/tts-dry-run-shows-new-fields", title: "tts speak --dry-run shows text_normalization, inline rules and a wav-wrapped G.711 format",
			args: qaArgs("tts speak --voice r4_ashley --text Hello --out x.wav --dry-run --format wav-mulaw --text-normalization=false --pronounce Plivo=PLEE-voh --pronounce-kind respell"),
			stderr: []string{`"text_normalization": false`, `"word": "Plivo"`, `"kind": "respell"`,
				`"container": "wav"`, `"encoding": "pcm_mulaw"`},
		},
		{
			id: "r4/tts-rate-refused-locally", title: "tts speak refuses a rate the format does not offer before it sends, and names the rates",
			args:   qaArgs("tts speak --voice r4_ashley --text Hello --out x.wav --yes --format wav-alaw --sample-rate 16000"),
			exit:   ExitUserError,
			stderr: []string{"16000 Hz is not offered for --format wav-alaw; use 8000"},
		},
	}
}
