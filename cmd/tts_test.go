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

	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const ttsAudio = "RIFF\x00\x01audio"

// ttsRun resets the tts flags, which persist between Execute calls, then runs.
func ttsRun(t *testing.T, srv *qaServer, stdin string, args ...string) qaResult {
	t.Helper()
	setFakeCreds(t)
	for _, c := range []*cobra.Command{ttsVoicesCmd, ttsSpeakCmd, ttsLexiconCreateCmd} {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Value.Type() != "stringArray" {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	ttsLexRules = nil
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
		"--language", "hi", "--pace", "1.1", "--volume", "0.8", "--lexicon", "lx_1", "--seed", "7")
	b := r.reqs[0].Body
	if r.err != nil || b["language"] != "hi" || b["pace"] != 1.1 || b["volume"] != 0.8 || b["lexicon_id"] != "lx_1" ||
		b["seed"] != float64(7) || b["continuation"] != false {
		t.Fatalf("err %v, body %v", r.err, b)
	}
	// The flags are package state: a second run must not inherit the first's.
	r = ttsRun(t, srv, "", "speak", "--voice", "v", "--text", "hi", "--out", out, "--yes")
	if b := r.reqs[0].Body; b["pace"] != nil || b["seed"] != nil || b["continuation"] != nil || b["lexicon_id"] != nil {
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
		{"--voice v --text hi --yes --format ogg", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --bitrate 64000", clierr.CodeBadFlag},
		{"--voice v --text caf\xe9 --yes", clierr.CodeBadInput},
		{"--voice v --text hi --yes --out $DIR/nodir/a.wav", clierr.CodeBadFlag},
		{"--voice v --text hi --yes --out $DIR", clierr.CodeBadFlag},
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

func TestTTSSpeak_dryRunSendsAndWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.wav")
	r := ttsRun(t, newQAServer(t, nil), "", "--dry-run", "speak", "--voice", "r4_ashley", "--text", "hi", "--out", out)
	if _, err := os.Stat(out); r.err != nil || len(r.reqs) != 0 || err == nil ||
		!strings.Contains(r.stderr, "[dry-run] POST") || !strings.Contains(r.stderr, "r4_ashley") {
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
}

const ttsVoicesJSON = `{"api_id": "x", "meta": {"total_count": 3}, "objects": [
 {"voice_id": "r4_ashley", "name": "Ashley", "gender": "female", "languages": ["en"], "accent_type": "American"},
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
	if r = ttsRun(t, srv, "", "voices", "--language", "kn", "-o", "json"); r.err != nil || !strings.Contains(r.stdout, `"objects": []`) {
		t.Errorf("no match must be an empty list: err %v\n%s", r.err, r.stdout)
	}
}

const ttsLexJSON = `{"lexicon_id": "lx_1", "name": "brands", "kind": "respell", "version": 1, "rules": [{"word": "Plivo", "value": "PLEE-voh"}]}`

func TestTTSLexicons_createChecksThenPosts(t *testing.T) {
	srv := newQAServer(t, map[string]string{"POST TTS/Lexicons/": ttsLexJSON})
	rules := filepath.Join(t.TempDir(), "rules.json")
	_ = os.WriteFile(rules, []byte(`[{"word": "Plivo", "value": "PLEE-voh"}]`), 0o600)

	r := ttsRun(t, srv, "", "lexicons", "create", "--name", "brands", "--kind", "respell", "--file", rules, "--rule", "SMS=ess em ess")
	want := `[map[value:PLEE-voh word:Plivo] map[value:ess em ess word:SMS]]`
	if r.err != nil || len(r.reqs) != 1 || r.reqs[0].Body["name"] != "brands" || r.reqs[0].Body["kind"] != "respell" ||
		fmt.Sprint(r.reqs[0].Body["rules"]) != want || !strings.Contains(r.stdout, "lx_1") {
		t.Fatalf("err %v, requests %v, stdout %s", r.err, r.reqs, r.stdout)
	}
	for _, line := range []string{
		"--name n --kind phoneme --rule a=b",
		"--name n --kind ipa",
		"--name n --kind ipa --rule noequals",
		"--name n --kind ipa --rule =b",
		"--name n --kind ipa --rule a=" + strings.Repeat("v", ttsMaxValueChars+1),
	} {
		if r := ttsRun(t, srv, "", append([]string{"lexicons", "create"}, strings.Fields(line)...)...); r.err == nil || len(r.reqs) != 0 {
			t.Errorf("%.50s: err %v, requests %v; want a refusal and none", line, r.err, r.reqs)
		}
	}
}

func TestTTSLexicons_listGetDelete(t *testing.T) {
	srv := newQAServer(t, map[string]string{
		"GET TTS/Lexicons/":         `{"objects": [` + ttsLexJSON + `]}`,
		"GET TTS/Lexicons/lx_1/":    ttsLexJSON,
		"DELETE TTS/Lexicons/lx_1/": ``,
	})
	if r := ttsRun(t, srv, "", "lexicons", "list", "-o", "table"); r.err != nil || !strings.Contains(r.stdout, "lx_1") || !strings.Contains(r.stdout, "respell") {
		t.Errorf("list: err %v\n%s", r.err, r.stdout)
	}
	if r := ttsRun(t, srv, "", "lexicon", "get", "lx_1", "-o", "table"); r.err != nil || !strings.Contains(r.stdout, "PLEE-voh") {
		t.Errorf("get: err %v\n%s", r.err, r.stdout)
	}
	if r := ttsRun(t, srv, "", "lexicons", "delete", "lx_1"); ttsCode(r.err) != clierr.CodeDestructiveRefused || len(r.reqs) != 0 {
		t.Errorf("delete without --yes: code %q, requests %v", ttsCode(r.err), r.reqs)
	}
	if r := ttsRun(t, srv, "", "lexicons", "delete", "lx_1", "--yes"); r.err != nil || len(r.reqs) != 1 || r.reqs[0].String() != "DELETE TTS/Lexicons/lx_1/" {
		t.Errorf("delete --yes: err %v, requests %v", r.err, r.reqs)
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
