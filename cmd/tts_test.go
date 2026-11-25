package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const ttsAudio = "RIFF\x00\x01audio"

// ttsRun resets the tts flags, which persist between Execute calls, then runs.
func ttsRun(t *testing.T, srv *qaServer, stdin string, args ...string) qaResult {
	t.Helper()
	setFakeCreds(t)
	for _, c := range []*cobra.Command{ttsVoicesCmd, ttsSpeakCmd, ttsDictionaryCreateCmd} {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Value.Type() != "stringArray" {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	ttsDictRules = nil
	return srv.run(t, stdin, append([]string{"tts"}, args...)...)
}

// ttsAudioServer answers the batch call with audio and records its Accept header.
func ttsAudioServer(t *testing.T, accept *string) *qaServer {
	srv := newQAServer(t, nil)
	srv.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/TTS/") {
			return false
		}
		*accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte(ttsAudio))
		return true
	}
	return srv
}

func ttsCode(err error) clierr.Code {
	var ce *clierr.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestTTSSpeak_writesTheAudioItGot(t *testing.T) {
	var accept string
	out := filepath.Join(t.TempDir(), "hello.wav")
	r := ttsRun(t, ttsAudioServer(t, &accept), "", "speak", "--voice", "r4_ashley", "--text", "Hello from Plivo", "--out", out, "--yes")
	got, _ := os.ReadFile(out)
	if r.err != nil || string(got) != ttsAudio || accept != "*/*" || len(r.reqs) != 1 || r.reqs[0].String() != "POST TTS/" {
		t.Fatalf("err %v, file %q, accept %q, requests %v", r.err, got, accept, r.reqs)
	}
	b := r.reqs[0].Body
	if b["voice_id"] != "r4_ashley" || b["text"] != "Hello from Plivo" ||
		fmt.Sprint(b["output_format"]) != "map[container:wav encoding:pcm_s16le sample_rate:24000]" {
		t.Errorf("body %v", b)
	}
	if _, ok := b["pace"]; ok || b["seed"] != nil || b["language"] != nil {
		t.Errorf("flags left at their defaults were sent: %v", b)
	}
}

func TestTTSSpeak_formatFromExtensionOrFlag(t *testing.T) {
	for _, c := range []struct {
		out   string
		extra []string
		want  string
	}{
		{"a.mp3", nil, "map[bitrate:128000 container:mp3 encoding:mp3 sample_rate:44100]"},
		{"a.ulaw", nil, "map[container:raw encoding:pcm_mulaw sample_rate:8000]"},
		{"a.alaw", nil, "map[container:raw encoding:pcm_alaw sample_rate:8000]"},
		{"a.pcm", []string{"--sample-rate", "16000"}, "map[container:raw encoding:pcm_s16le sample_rate:16000]"},
		{"a.bin", nil, "map[container:wav encoding:pcm_s16le sample_rate:24000]"},
		{"a.wav", []string{"--format", "mulaw"}, "map[container:raw encoding:pcm_mulaw sample_rate:8000]"},
		{"a.wav", []string{"--format", "mp3", "--bitrate", "64000"}, "map[bitrate:64000 container:mp3 encoding:mp3 sample_rate:44100]"},
		// 128000 is only valid at 44100 and 48000 Hz: the default follows the rate.
		{"a.mp3", []string{"--sample-rate", "8000"}, "map[bitrate:32000 container:mp3 encoding:mp3 sample_rate:8000]"},
		{"a.mp3", []string{"--sample-rate", "16000"}, "map[bitrate:64000 container:mp3 encoding:mp3 sample_rate:16000]"},
		{"a.mp3", []string{"--sample-rate", "22050"}, "map[bitrate:32000 container:mp3 encoding:mp3 sample_rate:22050]"},
		{"a.mp3", []string{"--sample-rate", "24000"}, "map[bitrate:48000 container:mp3 encoding:mp3 sample_rate:24000]"},
		{"a.mp3", []string{"--sample-rate", "48000"}, "map[bitrate:128000 container:mp3 encoding:mp3 sample_rate:48000]"},
	} {
		var accept string
		args := append([]string{"speak", "--voice", "v", "--text", "hi", "--out", filepath.Join(t.TempDir(), c.out), "--yes"}, c.extra...)
		r := ttsRun(t, ttsAudioServer(t, &accept), "", args...)
		if r.err != nil || len(r.reqs) != 1 || fmt.Sprint(r.reqs[0].Body["output_format"]) != c.want {
			t.Errorf("%s %v: err %v, output_format %v, want %s", c.out, c.extra, r.err, r.reqs, c.want)
		}
	}
}

func TestTTSSpeak_sendsOnlyTheOptionsGiven(t *testing.T) {
	var accept string
	srv := ttsAudioServer(t, &accept)
	out := filepath.Join(t.TempDir(), "a.wav")
	r := ttsRun(t, srv, "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes",
		"--language", "hi", "--pace", "1.1", "--volume", "0.8", "--pronunciation-dictionary", "pd_1", "--seed", "7")
	b := r.reqs[0].Body
	if r.err != nil || b["language"] != "hi" || b["pace"] != 1.1 || b["volume"] != 0.8 || b["pronunciation_dictionary_id"] != "pd_1" ||
		b["seed"] != float64(7) || b["continuation"] != false {
		t.Fatalf("err %v, body %v", r.err, b)
	}
	// The flags are package state: a second run must not inherit the first's.
	r = ttsRun(t, srv, "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes")
	if b := r.reqs[0].Body; b["pace"] != nil || b["seed"] != nil || b["continuation"] != nil || b["pronunciation_dictionary_id"] != nil {
		t.Errorf("second run inherited flags: %v", b)
	}
}

func TestTTSSpeak_refusedBeforeAnyRequest(t *testing.T) {
	long := strings.Repeat("a", ttsMaxChars+1)
	for _, c := range []struct {
		line string
		want clierr.Code
	}{
		{"--voice v --text hi", clierr.CodeDestructiveRefused},
		{"--voice v --yes", clierr.CodeBadInput},
		{"--voice v --text a --file f --yes", clierr.CodeBadInput},
		{"--voice v --text " + long + " --yes", clierr.CodeBadInput},
		{"--voice v --text hi --yes --pace 2", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --volume 0.1", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --language xx", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --language en", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --format ogg", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --bitrate 64000", clierr.CodeBadFlag},
		{"--voice v --text caf\xe9 --yes", clierr.CodeBadInput},
		{"--voice v --text hi --yes --out $DIR/nodir/a.wav", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --out $DIR", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --out $DIR/" + strings.Repeat("a", 300) + ".wav", clierr.CodeBadFlag},
		{"--voice v --file $DIR/m.txt --yes --out $DIR/m.txt", clierr.CodeBadFlag},
	} {
		dir := t.TempDir()
		out := filepath.Join(dir, "a.wav")
		_ = os.WriteFile(filepath.Join(dir, "m.txt"), []byte("hello"), 0o600)
		line := strings.ReplaceAll(c.line, "$DIR", dir)
		r := ttsRun(t, newQAServer(t, nil), "", append([]string{"speak", "--out", out}, strings.Fields(line)...)...)
		if _, err := os.Stat(out); ttsCode(r.err) != c.want || len(r.reqs) != 0 || err == nil {
			t.Errorf("%.60s: code %q, requests %v, file written %v; want %s, none, no", c.line, ttsCode(r.err), r.reqs, err == nil, c.want)
		}
	}
}

// The API takes 20,000 characters of text. The numbers are written out, so the
// test fails if the CLI's own limit moves away from the API's.
func TestTTSSpeak_textLimitIsTheAPIs(t *testing.T) {
	var accept string
	out := filepath.Join(t.TempDir(), "a.wav")
	r := ttsRun(t, ttsAudioServer(t, &accept), "", "speak", "--voice", "v", "--text", strings.Repeat("a", 20000), "--out", out, "--yes")
	if r.err != nil || len(r.reqs) != 1 || len(fmt.Sprint(r.reqs[0].Body["text"])) != 20000 {
		t.Errorf("20,000 characters: err %v, %d requests; want the text sent whole", r.err, len(r.reqs))
	}
	r = ttsRun(t, ttsAudioServer(t, &accept), "", "speak", "--voice", "v", "--text", strings.Repeat("a", 20001), "--out", out, "--yes")
	if ttsCode(r.err) != clierr.CodeBadInput || len(r.reqs) != 0 {
		t.Errorf("20,001 characters: code %q, %d requests; want %s and none", ttsCode(r.err), len(r.reqs), clierr.CodeBadInput)
	}
}

// Without --timeout the request waits for the text's length: the default 30 s
// cut a long text after it was billed. With --timeout it is the user's limit.
func TestTTSSpeak_waitsForTheTextsLength(t *testing.T) {
	var accept string
	var wait time.Duration
	srv := ttsAudioServer(t, &accept)
	audio := srv.handler
	srv.handler = func(w http.ResponseWriter, r *http.Request) bool {
		wait = clientForTest.HTTP.Timeout // the client the command is sending with
		return audio(w, r)
	}
	out := filepath.Join(t.TempDir(), "a.wav")
	for _, c := range []struct {
		chars int
		flags []string
		want  time.Duration
	}{
		{20, nil, 30 * time.Second},
		{3000, nil, 90 * time.Second},
		{20000, nil, 430 * time.Second},
		{20000, []string{"--timeout", "45"}, 45 * time.Second},
	} {
		wait = 0
		r := ttsRun(t, srv, "", append([]string{"speak", "--voice", "v", "--text", strings.Repeat("a", c.chars), "--out", out, "--yes"}, c.flags...)...)
		if r.err != nil || wait != c.want {
			t.Errorf("%d characters %v: err %v, the request waits %s; want %s", c.chars, c.flags, r.err, wait, c.want)
		}
	}
}

func TestTTSSpeak_dryRunSendsAndWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.wav")
	r := ttsRun(t, newQAServer(t, nil), "", "--dry-run", "speak", "--voice", "r4_ashley", "--text", "hi", "--out", out)
	if _, err := os.Stat(out); r.err != nil || len(r.reqs) != 0 || err == nil ||
		!strings.Contains(r.stderr, "[dry-run] POST") || !strings.Contains(r.stderr, "r4_ashley") || !strings.Contains(r.stderr, "2 characters") {
		t.Errorf("err %v, requests %v, stderr %q", r.err, r.reqs, r.stderr)
	}
}

func TestTTSSpeak_stdinToStdout(t *testing.T) {
	var accept string
	r := ttsRun(t, ttsAudioServer(t, &accept), "Hello stdin\n", "speak", "--voice", "v", "--file", "-", "--out", "-", "--yes")
	if r.err != nil || r.stdout != ttsAudio || len(r.reqs) != 1 || r.reqs[0].Body["text"] != "Hello stdin" {
		t.Errorf("err %v, stdout %q, requests %v", r.err, r.stdout, r.reqs)
	}
}

func TestTTSSpeak_apiErrorWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.wav")
	srv := newQAServer(t, map[string]string{"POST TTS/": `400 {"api_id": "x", "code": "language_not_supported", "error": "voice does not speak hi"}`})
	r := ttsRun(t, srv, "", "speak", "--voice", "r4_ashley", "--language", "hi", "--text", "hi", "--out", out, "--yes")
	if _, err := os.Stat(out); r.err == nil || err == nil || !strings.Contains(r.stderr, "language_not_supported") {
		t.Errorf("err %v, file written %v, stderr %q", r.err, err == nil, r.stderr)
	}
	// A 2xx that is not audio is no success either.
	r = ttsRun(t, newQAServer(t, map[string]string{"POST TTS/": `200 {"error": "voice overloaded"}`}), "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes")
	if _, err := os.Stat(out); ttsCode(r.err) != clierr.CodeUpstreamError || err == nil || !strings.Contains(r.stderr, "voice overloaded") {
		t.Errorf("JSON 200: err %v, file written %v, stderr %q", r.err, err == nil, r.stderr)
	}
	// A 5xx may have been billed: never retryable, and the hint sends the caller to the usage report.
	r = ttsRun(t, newQAServer(t, map[string]string{"POST TTS/": `502 {"error": "no node"}`}), "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes")
	var ce *clierr.Error
	if _, err := os.Stat(out); !errors.As(r.err, &ce) || ce.Retryable || !strings.Contains(ce.Hint, "tts usage") || err == nil || len(r.reqs) != 1 {
		t.Errorf("502: err %v, file written %v, requests %v; want not retryable, a usage hint, no file, 1 request", r.err, err == nil, r.reqs)
	}
}

const ttsVoicesJSON = `{"api_id": "x", "meta": {"total_count": 3}, "objects": [
 {"voice_id": "r4_ashley", "is_owner": true, "name": "Ashley", "gender": "female", "languages": ["en"], "accent_type": "American", "description": "Warm", "sample_url": "https://example.test/ashley.mp3", "created_at": "2026-11-17T09:30:00.000Z"},
 {"voice_id": "r4_riya", "name": "Riya", "gender": "female", "languages": ["hi"], "accent_type": "Indian"},
 {"voice_id": "r4_aditya", "name": "Aditya", "gender": "male", "languages": ["hi"], "accent_type": "Indian"}]}`

func TestTTSVoices_filterHere(t *testing.T) {
	srv := newQAServer(t, map[string]string{"GET TTS/Voices/": ttsVoicesJSON})
	r := ttsRun(t, srv, "", "voices", "--language", "hi", "-o", "table")
	if r.err != nil || !strings.Contains(r.stdout, "r4_riya") || !strings.Contains(r.stdout, "r4_aditya") || strings.Contains(r.stdout, "r4_ashley") {
		t.Errorf("--language hi: err %v\n%s", r.err, r.stdout)
	}
	r = ttsRun(t, srv, "", "voices", "--language", "HI", "--gender", "Female", "-o", "json")
	var out struct {
		Data struct {
			Objects []struct {
				VoiceID string `json:"voice_id"`
			} `json:"objects"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || len(out.Data.Objects) != 1 || out.Data.Objects[0].VoiceID != "r4_riya" {
		t.Errorf("--language HI --gender Female -o json: %v\n%s", err, r.stdout)
	}
	if r = ttsRun(t, srv, "", "voices", "--language", "en", "-o", "json"); r.err != nil || !strings.Contains(r.stdout, `"sample_url": "https://example.test/ashley.mp3"`) || !strings.Contains(r.stdout, `"description": "Warm"`) ||
		!strings.Contains(r.stdout, `"is_owner": true`) || !strings.Contains(r.stdout, `"created_at": "2026-11-17T09:30:00.000Z"`) {
		t.Errorf("a voice must keep its sample_url, description, is_owner and created_at: err %v\n%s", r.err, r.stdout)
	}
	if r = ttsRun(t, srv, "", "voices", "--language", "kn", "-o", "json"); r.err != nil || !strings.Contains(r.stdout, `"objects": []`) {
		t.Errorf("no match must be an empty list: err %v\n%s", r.err, r.stdout)
	}
}

const ttsDictJSON = `{"pronunciation_dictionary_id": "pd_1", "name": "brands", "description": "Brand names", "version": 1, "created_at": "2026-11-17T09:30:00.000Z",
 "rules": [{"word": "Plivo", "kind": "respell", "value": "PLEE-voh", "case_sensitive": false}]}`

func TestTTSDictionaries_createChecksThenPosts(t *testing.T) {
	srv := newQAServer(t, map[string]string{"POST TTS/PronunciationDictionaries/": ttsDictJSON})
	dir := t.TempDir()
	rules, noKind, misspelt := filepath.Join(dir, "rules.json"), filepath.Join(dir, "nokind.json"), filepath.Join(dir, "misspelt.json")
	_ = os.WriteFile(rules, []byte(`[{"word": "Plivo", "kind": "ipa", "value": "ˈpliːvoʊ", "case_sensitive": true}]`), 0o600)
	_ = os.WriteFile(noKind, []byte(`[{"word": "Plivo", "value": "PLEE-voh"}, {"word": "SMS", "value": "ess em ess"}]`), 0o600)
	_ = os.WriteFile(misspelt, []byte(`[{"word": "Plivo", "kind": "ipa", "value": "ˈpliːvoʊ", "caseSensitive": true}]`), 0o600)

	// The kind is each rule's own: the file's, else --kind. The dictionary has none.
	r := ttsRun(t, srv, "", "pronunciation-dictionaries", "create", "--name", "brands", "--kind", "respell", "--file", rules, "--rule", "SMS=ess em ess")
	want := `[map[case_sensitive:true kind:ipa value:ˈpliːvoʊ word:Plivo] map[kind:respell value:ess em ess word:SMS]]`
	if r.err != nil || len(r.reqs) != 1 || r.reqs[0].Body["name"] != "brands" || r.reqs[0].Body["kind"] != nil ||
		fmt.Sprint(r.reqs[0].Body["rules"]) != want || !strings.Contains(r.stdout, "pd_1") {
		t.Fatalf("err %v, requests %v, stdout %s", r.err, r.reqs, r.stdout)
	}
	// A file whose rules name no kind takes --kind for each of them.
	r = ttsRun(t, srv, "", "pronunciation-dictionaries", "create", "--name", "brands", "--kind", "respell", "--file", noKind)
	want = `[map[kind:respell value:PLEE-voh word:Plivo] map[kind:respell value:ess em ess word:SMS]]`
	if r.err != nil || len(r.reqs) != 1 || fmt.Sprint(r.reqs[0].Body["rules"]) != want {
		t.Fatalf("--kind for a file with no kinds: err %v, requests %v; want rules %s", r.err, r.reqs, want)
	}
	// A file whose rules carry their own kind needs no --kind.
	r = ttsRun(t, srv, "", "pronunciation-dictionaries", "create", "--name", "brands", "--file", rules)
	want = `[map[case_sensitive:true kind:ipa value:ˈpliːvoʊ word:Plivo]]`
	if r.err != nil || len(r.reqs) != 1 || fmt.Sprint(r.reqs[0].Body["rules"]) != want {
		t.Fatalf("a file with its own kinds and no --kind: err %v, requests %v; want rules %s", r.err, r.reqs, want)
	}
	for _, line := range []string{
		"--name n --kind phoneme --rule a=b",
		"--name n --kind phoneme --file " + rules,
		"--name n --file " + misspelt,
		"--name n --kind ipa",
		"--name n --kind ipa --rule noequals",
		"--name n --kind ipa --rule =b",
		"--name n --kind ipa --rule a=" + strings.Repeat("v", ttsMaxValueChars+1),
		"--name n --rule a=b",
		"--name n --file " + noKind,
	} {
		if r := ttsRun(t, srv, "", append([]string{"pronunciation-dictionaries", "create"}, strings.Fields(line)...)...); r.err == nil || len(r.reqs) != 0 {
			t.Errorf("%.50s: err %v, requests %v; want a refusal and none", line, r.err, r.reqs)
		}
	}
}

func TestTTSDictionaries_listGetDelete(t *testing.T) {
	srv := newQAServer(t, map[string]string{
		"GET TTS/PronunciationDictionaries/":         `{"objects": [` + ttsDictJSON + `]}`,
		"GET TTS/PronunciationDictionaries/pd_1/":    ttsDictJSON,
		"DELETE TTS/PronunciationDictionaries/pd_1/": `{"api_id": "x", "pronunciation_dictionary_id": "pd_1", "message": "deleted"}`,
	})
	if r := ttsRun(t, srv, "", "pronunciation-dictionaries", "list", "-o", "table"); r.err != nil || !strings.Contains(r.stdout, "pd_1") || !strings.Contains(r.stdout, "brands") {
		t.Errorf("list: err %v\n%s", r.err, r.stdout)
	}
	if r := ttsRun(t, srv, "", "pronunciation-dictionary", "get", "pd_1", "-o", "table"); r.err != nil || !strings.Contains(r.stdout, "PLEE-voh") ||
		!strings.Contains(r.stdout, "respell") || !strings.Contains(r.stdout, "Brand names") {
		t.Errorf("get: err %v\n%s", r.err, r.stdout)
	}
	if r := ttsRun(t, srv, "", "pronunciation-dictionaries", "delete", "pd_1"); ttsCode(r.err) != clierr.CodeDestructiveRefused || len(r.reqs) != 0 {
		t.Errorf("delete without --yes: code %q, requests %v", ttsCode(r.err), r.reqs)
	}
	if r := ttsRun(t, srv, "", "pronunciation-dictionaries", "delete", "pd_1", "--yes"); r.err != nil || len(r.reqs) != 1 || r.reqs[0].String() != "DELETE TTS/PronunciationDictionaries/pd_1/" {
		t.Errorf("delete --yes: err %v, requests %v", r.err, r.reqs)
	}
}

// The API has one name for a pronunciation dictionary: the old commands and
// flag are gone, not aliases, and send nothing.
func TestTTS_noLexiconNames(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.wav")
	for _, args := range [][]string{
		{"lexicons", "list"},
		{"lexicon", "get", "lx_1"},
		{"speak", "--voice", "v", "--text", "hi", "--out", out, "--yes", "--lexicon", "lx_1"},
	} {
		if r := ttsRun(t, newQAServer(t, nil), "", args...); r.err == nil || len(r.reqs) != 0 {
			t.Errorf("%v: err %v, requests %v; want an error and no request", args, r.err, r.reqs)
		}
	}
}

func TestTTSUsage_namesBatchAndRealtime(t *testing.T) {
	srv := newQAServer(t, map[string]string{"GET TTS/Usage/": `{"auth_id": "x", "routes": {
 "live": {"requests": 552, "characters": 39948, "tokens": 1, "audio_seconds": 2760.88, "spoken_chars": 38282},
 "http": {"requests": 198, "characters": 12011, "tokens": 1, "audio_seconds": 820.5, "spoken_chars": 11800}}}`})
	r := ttsRun(t, srv, "", "usage", "-o", "table")
	batch, realtime := strings.Index(r.stdout, "batch"), strings.Index(r.stdout, "realtime")
	if r.err != nil || batch < 0 || realtime < batch || !strings.Contains(r.stdout, "12011") || !strings.Contains(r.stdout, "39948") || !strings.Contains(r.stdout, "2760.9") {
		t.Errorf("err %v\n%s", r.err, r.stdout)
	}
}

func TestTTSSpeak_regionalEnglishCodes(t *testing.T) {
	var accept string
	srv := ttsAudioServer(t, &accept)
	out := filepath.Join(t.TempDir(), "a.wav")
	for in, want := range map[string]string{"en-US": "en-US", "en-gb": "en-GB", "EN-IN": "en-IN", "en-AU": "en-AU"} {
		r := ttsRun(t, srv, "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes", "--language", in)
		if r.err != nil || len(r.reqs) == 0 || r.reqs[len(r.reqs)-1].Body["language"] != want {
			t.Errorf("--language %s: err %v, requests %v; want language %s sent", in, r.err, r.reqs, want)
		}
	}
	r := ttsRun(t, srv, "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes", "--language", "en")
	var ce *clierr.Error
	if !errors.As(r.err, &ce) || !strings.Contains(ce.Hint, "en-US") {
		t.Errorf("bare en: err %v; want a hint naming the regional codes", r.err)
	}
}

func TestTTSVoices_englishFamily(t *testing.T) {
	srv := newQAServer(t, map[string]string{"GET TTS/Voices/": `{"objects": [
 {"voice_id": "a", "languages": ["en-US"]}, {"voice_id": "b", "languages": ["en-GB"]}, {"voice_id": "c", "languages": ["hi"]}]}`})
	r := ttsRun(t, srv, "", "voices", "--language", "en", "-o", "table")
	if r.err != nil || !strings.Contains(r.stdout, "a ") || !strings.Contains(r.stdout, "b ") || strings.Contains(r.stdout, "c ") {
		t.Errorf("--language en: err %v\n%s", r.err, r.stdout)
	}
	r = ttsRun(t, srv, "", "voices", "--language", "en-GB", "-o", "json")
	if r.err != nil || !strings.Contains(r.stdout, `"voice_id": "b"`) || strings.Contains(r.stdout, `"voice_id": "a"`) || strings.Contains(r.stdout, "lexicon") {
		t.Errorf("--language en-GB: err %v\n%s", r.err, r.stdout)
	}
}

func TestTTSVoice_showsOne(t *testing.T) {
	srv := newQAServer(t, map[string]string{"GET TTS/Voices/v1/": `{"api_id": "x1", "voice_id": "v1", "is_owner": true, "name": "Hailey", "gender": "female",
 "languages": ["en-US"], "accent_type": "American", "description": "Warm", "sample_url": "https://example.test/h.mp3", "created_at": "2026-11-17T09:30:00.000Z"}`})
	r := ttsRun(t, srv, "", "voice", "v1", "-o", "table")
	if r.err != nil || len(r.reqs) != 1 || r.reqs[0].String() != "GET TTS/Voices/v1/" || !strings.Contains(r.stdout, "Hailey") ||
		!strings.Contains(r.stdout, "en-US") || !strings.Contains(r.stdout, "true") || !strings.Contains(r.stdout, "2026-11-17") {
		t.Errorf("table: err %v, requests %v\n%s", r.err, r.reqs, r.stdout)
	}
	if r = ttsRun(t, srv, "", "voice", "v1", "-o", "json"); r.err != nil || !strings.Contains(r.stdout, `"api_id": "x1"`) || !strings.Contains(r.stdout, `"is_owner": true`) {
		t.Errorf("json: err %v\n%s", r.err, r.stdout)
	}
	if r = ttsRun(t, srv, "", "voice"); r.err == nil || len(r.reqs) != 0 {
		t.Errorf("no voice_id: err %v, requests %v; want an error and no request", r.err, r.reqs)
	}
}
