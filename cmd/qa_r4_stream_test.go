package cmd

func r4StreamCases() []qaCase {
	return []qaCase{
		{id: "r4/stream-default", title: "streams start sent 16 kHz with no --content-type; the API's default is 8 kHz", args: qaArgs("voice calls streams start c1 --url wss://example.com/s --dry-run"), stderr: []string{`"content_type": "audio/x-l16;rate=8000"`, "No --content-type: sending audio/x-l16;rate=8000 (v1.3.3 and earlier sent audio/x-l16;rate=16000)"}},
		{id: "r4/stream-explicit", title: "an explicit --content-type goes out as given, with no note", args: qaArgs("voice calls streams start c1 --url wss://example.com/s --dry-run --content-type audio/x-mulaw;rate=8000"), stderr: []string{`"content_type": "audio/x-mulaw;rate=8000"`}, hidden: []string{"No --content-type"}},
	}
}
