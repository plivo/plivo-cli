package cmd

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// Limits from the Plivo TTS API reference.
const (
	ttsMaxChars       = 20000
	ttsMaxInputBytes  = 1 << 20 // the API's request body limit
	ttsMaxRules       = 100
	ttsMaxNameChars   = 100
	ttsMaxWordChars   = 100
	ttsMaxValueChars  = 120
	ttsDefaultBitrate = 128000
	ttsMinPace        = 0.7
	ttsMaxPace        = 1.2
	ttsMinVolume      = 0.5
	ttsMaxVolume      = 2.0
)

// ttsLanguages are the codes the API takes. English comes in four regional codes;
// there is no plain "en".
var ttsLanguages = []string{"auto", "en-US", "en-GB", "en-IN", "en-AU", "hi", "bn", "ta", "te", "mr", "gu", "kn"}

// ttsKinds is how a pronunciation rule's value can be written.
var ttsKinds = []string{"ipa", "arpa", "respell"}

// ttsLanguageMatches reports whether a voice that speaks langs matches a --language
// filter. A code with no region ("en") matches every region of that language.
func ttsLanguageMatches(langs []string, want string) bool {
	return slices.ContainsFunc(langs, func(l string) bool {
		if strings.EqualFold(l, want) {
			return true
		}
		base, _, _ := strings.Cut(l, "-")
		return !strings.Contains(want, "-") && strings.EqualFold(base, want)
	})
}

// ttsFormat is a --format preset: the output_format a batch request sends.
type ttsFormat struct {
	container, encoding string
	sampleRate          int
}

var ttsFormats = map[string]ttsFormat{
	"wav":   {"wav", "pcm_s16le", 24000},
	"mp3":   {"mp3", "mp3", 44100},
	"mulaw": {"raw", "pcm_mulaw", 8000},
	"alaw":  {"raw", "pcm_alaw", 8000},
	"pcm":   {"raw", "pcm_s16le", 24000},
}

// ttsMP3Bitrates is the bitrate the API takes at each mp3 sample rate (the highest
// where it takes more than one): 128000 is only valid at 44100 and 48000 Hz.
var ttsMP3Bitrates = map[int]int{8000: 32000, 16000: 64000, 22050: 32000, 24000: 48000, 44100: 128000, 48000: 128000}

// ttsExtFormats picks a --format from the --out extension.
var ttsExtFormats = map[string]string{
	".wav": "wav", ".mp3": "mp3", ".ulaw": "mulaw", ".mulaw": "mulaw",
	".alaw": "alaw", ".pcm": "pcm", ".raw": "pcm",
}

var ttsCmd = &cobra.Command{
	Use:   "tts",
	Short: "Text-to-speech: list voices, speak text to an audio file, manage pronunciation dictionaries",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var ttsVoicesCmd = &cobra.Command{
	Use:   "voices",
	Short: "List the voices you can speak with",
	Long: `List the voices you can speak with: Plivo's public voices and any your
organization owns. Each voice speaks one language. --language and --gender
narrow the list. --language en lists every English voice; en-US, en-GB, en-IN
and en-AU list one accent.`,
	Example: `  plivo tts voices --language hi
  plivo tts voices --language en-US --gender female
  plivo tts voice 6uWzc4xFj31t4jNJUaED`,
	Args: cobra.NoArgs,
	RunE: runTTSVoices,
}

var ttsVoiceGetCmd = &cobra.Command{
	Use:     "voice <voice_id>",
	Short:   "Show one voice",
	Example: `  plivo tts voice 6uWzc4xFj31t4jNJUaED`,
	Args:    cobra.ExactArgs(1),
	RunE:    runTTSVoiceGet,
}

var ttsSpeakCmd = &cobra.Command{
	Use:   "speak",
	Short: "Turn text into an audio file (billed per character)",
	Long: `Turn text into an audio file with a Plivo voice.

Batch text-to-speech is billed per character, so it needs --yes; --dry-run
prints the request and sends nothing. Text is up to 20000 characters, from
--text or --file (- reads stdin). Without --timeout, speak waits long enough
for the text's length (30 seconds plus one second for every 50 characters);
--timeout sets your own limit.

--format picks the audio: wav (24 kHz), mp3 (44.1 kHz, 128 kbps), mulaw or alaw
(8 kHz telephony, raw), pcm (24 kHz, raw). Without it the --out extension
decides (.wav .mp3 .ulaw .alaw .pcm), and anything else is wav. --out - writes
the audio to stdout, which is refused on a terminal.`,
	Example: `  plivo tts speak --voice lC7qzMoRE7SSjaNCVeff --text "Hello from Plivo" --out hello.wav --dry-run
  plivo tts speak --voice lC7qzMoRE7SSjaNCVeff --text "Hello from Plivo" --out hello.wav --yes
  plivo tts speak --voice 6uWzc4xFj31t4jNJUaED --language hi --file msg.txt --out msg.ulaw --yes`,
	Args: cobra.NoArgs,
	RunE: runTTSSpeak,
}

var ttsDictionaryCmd = &cobra.Command{
	Use:     "pronunciation-dictionaries",
	Aliases: []string{"pronunciation-dictionary"},
	Short:   "Manage pronunciation dictionaries",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var ttsDictionaryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pronunciation dictionaries",
	Args:  cobra.NoArgs,
	RunE:  runTTSDictionaryList,
}

var ttsDictionaryGetCmd = &cobra.Command{
	Use:   "get <pronunciation_dictionary_id>",
	Short: "Get a pronunciation dictionary and its rules",
	Args:  cobra.ExactArgs(1),
	RunE:  runTTSDictionaryGet,
}

var ttsDictionaryCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a pronunciation dictionary",
	Long: `Create a pronunciation dictionary: up to 100 rules, each a word, how to say it
and the kind that value is written in: ipa, arpa or respell. Give the rules
with --rule word=value (repeatable) or --file, a JSON array of
{"word": ..., "kind": ..., "value": ...}; a rule in the file may also set
"case_sensitive": true. --kind is the kind of every --rule, and of a rule in
the file that names none. Each create makes a new dictionary, also with a name
you already use. Use the dictionary with
plivo tts speak --pronunciation-dictionary <pronunciation_dictionary_id>.`,
	Example: `  plivo tts pronunciation-dictionaries create --name brands --kind respell --rule "Plivo=PLEE-voh"`,
	Args:    cobra.NoArgs,
	RunE:    runTTSDictionaryCreate,
}

var ttsDictionaryDeleteCmd = &cobra.Command{
	Use:   "delete <pronunciation_dictionary_id>",
	Short: "Delete a pronunciation dictionary (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTTSDictionaryDelete,
}

var ttsUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show characters and audio generated, batch and realtime",
	Args:  cobra.NoArgs,
	RunE:  runTTSUsage,
}

var (
	ttsVoicesLanguage string
	ttsVoicesGender   string

	ttsVoice      string
	ttsText       string
	ttsFile       string
	ttsOut        string
	ttsLanguage   string
	ttsFormatName string
	ttsSampleRate int
	ttsBitrate    int
	ttsPace       float64
	ttsVolume     float64
	ttsDictionary string
	ttsSeed       int64

	ttsDictName  string
	ttsDictKind  string
	ttsDictFile  string
	ttsDictRules []string
)

func init() {
	ttsVoicesCmd.Flags().StringVar(&ttsVoicesLanguage, "language", "", "only voices for this language code (en, en-US, en-GB, en-IN, en-AU, hi, bn, ta, te, mr, gu, kn)")
	ttsVoicesCmd.Flags().StringVar(&ttsVoicesGender, "gender", "", "only voices of this gender (male or female)")

	f := ttsSpeakCmd.Flags()
	f.StringVar(&ttsVoice, "voice", "", "voice_id from plivo tts voices (required)")
	f.StringVar(&ttsText, "text", "", "text to speak, up to 20000 characters")
	f.StringVar(&ttsFile, "file", "", "read the text from this file (- for stdin)")
	f.StringVar(&ttsOut, "out", "", "file to write the audio to (- for stdout) (required)")
	f.StringVar(&ttsLanguage, "language", "", "language code: "+strings.Join(ttsLanguages, ", ")+" (default auto)")
	f.StringVar(&ttsFormatName, "format", "", "audio format: wav, mp3, mulaw, alaw or pcm (default from the --out extension)")
	f.IntVar(&ttsSampleRate, "sample-rate", 0, "sample rate in Hz (default from --format)")
	f.IntVar(&ttsBitrate, "bitrate", 0, "mp3 bitrate in bits per second (default depends on the sample rate)")
	f.Float64Var(&ttsPace, "pace", 1.0, "speaking pace, 0.7 to 1.2")
	f.Float64Var(&ttsVolume, "volume", 1.0, "volume, 0.5 to 2.0")
	f.StringVar(&ttsDictionary, "pronunciation-dictionary", "", "pronunciation_dictionary_id of a saved pronunciation dictionary to apply")
	f.Int64Var(&ttsSeed, "seed", 0, "repeatable output: the same text, voice and seed give identical audio")
	_ = ttsSpeakCmd.MarkFlagRequired("voice")
	_ = ttsSpeakCmd.MarkFlagRequired("out")

	g := ttsDictionaryCreateCmd.Flags()
	g.StringVar(&ttsDictName, "name", "", "dictionary name, up to 100 characters (required)")
	g.StringVar(&ttsDictKind, "kind", "", "how a value is written: ipa, arpa or respell (required for --rule)")
	g.StringVar(&ttsDictFile, "file", "", "JSON file: an array of {\"word\": ..., \"kind\": ..., \"value\": ...}")
	g.StringArrayVar(&ttsDictRules, "rule", nil, "a rule as word=value, written in --kind (repeatable)")
	_ = ttsDictionaryCreateCmd.MarkFlagRequired("name")

	ttsDictionaryCmd.AddCommand(ttsDictionaryListCmd, ttsDictionaryGetCmd, ttsDictionaryCreateCmd, ttsDictionaryDeleteCmd)
	ttsCmd.AddCommand(ttsVoicesCmd, ttsVoiceGetCmd, ttsSpeakCmd, ttsDictionaryCmd, ttsUsageCmd)
	rootCmd.AddCommand(ttsCmd)
}

func runTTSVoices(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.TTSVoiceList
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "Voices"), nil, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	var rows []api.TTSVoice
	table := [][]string{{"VOICE_ID", "NAME", "GENDER", "LANGUAGES", "ACCENT"}}
	for _, v := range resp.Objects {
		if ttsVoicesLanguage != "" && !ttsLanguageMatches(v.Languages, ttsVoicesLanguage) {
			continue
		}
		if ttsVoicesGender != "" && !strings.EqualFold(v.Gender, ttsVoicesGender) {
			continue
		}
		rows = append(rows, v)
		table = append(table, []string{v.VoiceID, v.Name, v.Gender, strings.Join(v.Languages, ","), v.AccentType})
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, mustJSON(map[string]any{"objects": rows}), "objects")
	}
	return output.Table(os.Stdout, table)
}

func runTTSVoiceGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var v api.TTSVoiceDetail
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "Voices", args[0]), nil, nil, &v)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, v.Raw())
	}
	return output.KV(os.Stdout, [][2]string{
		{"voice_id", v.VoiceID}, {"name", v.Name}, {"gender", v.Gender},
		{"languages", strings.Join(v.Languages, ",")}, {"accent", v.AccentType},
		{"description", v.Description}, {"is_owner", fmt.Sprint(v.IsOwner)}, {"sample_url", v.SampleURL},
		{"created_at", v.CreatedAt},
	})
}

func runTTSSpeak(cmd *cobra.Command, args []string) error {
	text, err := ttsReadText()
	if err != nil {
		return err
	}
	chars := utf8.RuneCountInString(text)
	body, err := ttsSpeakBody(cmd, text)
	if err != nil {
		return err
	}
	if err := ttsCheckOut(); err != nil {
		return err
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("timeout") {
		// The timeout covers the whole response, and the audio arrives at about 64
		// to 100 characters of text a second: the default would cut a long text
		// after it was billed.
		client.HTTP.Timeout = max(timeoutFlag, time.Duration(chars/50+30)*time.Second)
	}
	proceed, dryRun, gerr := guardSpend(fmt.Sprintf("speak %d characters with voice %s", chars, ttsVoice))
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	// A terminal for stdout and a folder that takes a file are checked before
	// the billed send. The save can still fail after it: ttsNotSaved says so.
	var staged *os.File
	var dest string
	var keep bool // only the final rename failed: the staged file holds all the audio
	switch {
	case dryRun:
		fmt.Fprintf(os.Stderr, "[dry-run] %d characters, billed per character when sent with --yes\n", chars)
	case ttsOut == "-":
		if isTerminal(int(os.Stdout.Fd())) {
			e := clierr.BadFlag("out", "refusing to write audio to a terminal")
			e.Hint = "Give a file name with --out, or pipe --out - into a program that reads audio from stdin."
			return e
		}
	default:
		if staged, dest, err = ttsStage(); err != nil {
			return err
		}
		defer func() {
			_ = staged.Close()
			if !keep {
				_ = os.Remove(staged.Name())
			}
		}()
	}

	var audio api.BinaryBody
	apiErr, err := client.Do("POST", client.AccountURL("TTS"), body, nil, &audio)
	if err != nil {
		return err
	}
	if apiErr != nil {
		// A 5xx or 408 can come back after Plivo did the work and billed it.
		if apiErr.StatusCode >= 500 || apiErr.StatusCode == http.StatusRequestTimeout {
			apiErr.Retryable = false
			apiErr.Hint = "The request may have been billed. Compare `plivo tts usage` with the reading from before the send before you send it again."
		}
		return apiErr
	}
	if client.DryRun {
		return nil
	}
	if e := ttsCheckAudio(audio); e != nil {
		return e
	}
	if ttsOut == "-" {
		// A closed pipe must come back as an error that says the send was billed,
		// not kill the process with SIGPIPE and no message.
		signal.Ignore(syscall.SIGPIPE)
		if _, err := os.Stdout.Write(audio.Data); err != nil {
			return ttsNotSaved(err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %d bytes of %s to stdout\n", len(audio.Data), audio.ContentType)
		return nil
	}
	if err := ttsSave(staged, dest, audio.Data); err != nil {
		e := ttsNotSaved(err)
		var le *os.LinkError // from the rename; a failed write or close is a PathError
		if st, serr := os.Stat(staged.Name()); errors.As(err, &le) && serr == nil && st.Size() == int64(len(audio.Data)) {
			keep = true
			e.Hint = "The request was sent and billed; the audio could not be moved to " + ttsOut + " and is kept in " + staged.Name() + "."
		}
		return e
	}
	return emitWrite(fmt.Sprintf("Wrote %d bytes of %s to %s (%d characters)", len(audio.Data), audio.ContentType, ttsOut, chars),
		map[string]any{"out": ttsOut, "bytes": len(audio.Data), "content_type": audio.ContentType, "characters": chars, "voice_id": ttsVoice})
}

// ttsCheckOut refuses an --out that is plainly unusable, before anything is
// billed: a path the system cannot look up, a directory, a missing folder, or
// the file --file reads. It cannot promise the save works. It runs on --dry-run
// too, so a preview does not pass a command that would then fail. - is stdout
// and has no path to check.
func ttsCheckOut() error {
	if ttsOut == "-" {
		return nil
	}
	if ttsOut == "" {
		return clierr.BadFlag("out", "give a file name")
	}
	out, outErr := os.Stat(ttsOut)
	if outErr != nil && !errors.Is(outErr, fs.ErrNotExist) {
		return clierr.BadFlag("out", fmt.Sprintf("cannot use this path: %v", errors.Unwrap(outErr)))
	}
	if outErr == nil && out.IsDir() {
		return clierr.BadFlag("out", ttsOut+" is a directory; give a file name")
	}
	if dir, err := os.Stat(filepath.Dir(ttsOut)); err != nil || !dir.IsDir() {
		return clierr.BadFlag("out", "the folder "+filepath.Dir(ttsOut)+" does not exist")
	}
	if ttsFile != "" && ttsFile != "-" && outErr == nil {
		if in, err := os.Stat(ttsFile); err == nil && os.SameFile(in, out) {
			return clierr.BadFlag("out", "is the file --file reads; the audio would overwrite your text")
		}
	}
	return nil
}

// ttsStage opens the temp file the audio is saved through, beside --out so
// the final rename stays on one filesystem and a failed save never touches an
// existing --out. dest is what it is renamed to: --out, or the file a link
// there points at.
func ttsStage() (f *os.File, dest string, err error) {
	dest = ttsOut
	if p, e := filepath.EvalSymlinks(ttsOut); e == nil {
		dest = p
	}
	f, err = os.CreateTemp(filepath.Dir(dest), ".plivo-tts-*")
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, "", clierr.BadFlag("out", fmt.Sprintf("cannot write in %s: %v", filepath.Dir(dest), err))
	}
	return f, dest, nil
}

// ttsSave puts the audio at dest whole or not at all.
func ttsSave(f *os.File, dest string, data []byte) error {
	_, err := f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// The file keeps the permissions of the one it replaces; a new one gets 0644.
	// A chmod that fails is cosmetic and must not cost the billed audio.
	mode := os.FileMode(0o644)
	if st, serr := os.Stat(dest); serr == nil {
		mode = st.Mode().Perm()
	}
	_ = os.Chmod(f.Name(), mode)
	return os.Rename(f.Name(), dest)
}

const ttsBilledHint = "The request was sent and billed; the audio was not saved."

// ttsCheckAudio refuses a 2xx answer that is not audio: a JSON or HTML body
// saved as an audio file would be a billed success that plays nothing.
func ttsCheckAudio(a api.BinaryBody) *clierr.Error {
	mt, _, _ := mime.ParseMediaType(a.ContentType)
	var msg string
	switch {
	case len(a.Data) == 0:
		msg = "Plivo returned no audio"
	case mt == "application/json" || strings.HasSuffix(mt, "+json") || strings.HasPrefix(mt, "text/"):
		body := strings.TrimSpace(strings.ToValidUTF8(string(a.Data[:min(len(a.Data), 200)]), ""))
		msg = fmt.Sprintf("Plivo returned %s, not audio: %s", mt, body)
	default:
		return nil
	}
	e := clierr.Upstream(msg)
	e.Retryable = false
	e.Hint = ttsBilledHint + " Check `plivo tts usage` before you send it again."
	return e
}

// ttsNotSaved marks an error from after the send: Plivo already billed it.
func ttsNotSaved(err error) *clierr.Error {
	e := clierr.Wrap(err)
	e.Hint = ttsBilledHint
	return e
}

// ttsReadText returns the text from --text or --file, exactly one of them.
func ttsReadText() (string, error) {
	if (ttsText == "") == (ttsFile == "") {
		return "", clierr.BadInput("give the text with exactly one of --text or --file")
	}
	text := ttsText
	if ttsFile != "" {
		var b []byte
		var err error
		if ttsFile == "-" {
			b, err = ttsReadAll(os.Stdin, "the text")
		} else {
			b, err = ttsReadPath(ttsFile, "the text")
		}
		if err != nil {
			return "", err
		}
		text = string(b)
	}
	if !utf8.ValidString(text) {
		return "", clierr.BadInput("the text must be UTF-8")
	}
	// Padding is billed as characters and says nothing: a file ends in a newline,
	// and a byte order mark leads one that an editor saved.
	if text = strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF")); text == "" {
		return "", clierr.BadInput("the text is empty")
	}
	if n := utf8.RuneCountInString(text); n > ttsMaxChars {
		e := clierr.BadInput(fmt.Sprintf("the text is %d characters; one request takes up to %d", n, ttsMaxChars))
		e.Hint = "Split the text and run one request per part."
		return "", e
	}
	return text, nil
}

// ttsReadAll reads r whole and refuses more than the API's 1 MiB body: a cut-off
// read would be billed as if it were everything the user gave. what names the
// input in the refusal.
func ttsReadAll(r io.Reader, what string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, ttsMaxInputBytes+1))
	if err != nil {
		return nil, clierr.Wrap(err)
	}
	if len(b) > ttsMaxInputBytes {
		return nil, clierr.BadInput(what + " is more than 1 MiB")
	}
	return b, nil
}

func ttsReadPath(path, what string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, clierr.Wrap(err)
	}
	defer f.Close()
	return ttsReadAll(f, what)
}

// ttsSpeakBody checks the speak flags and builds the batch request body.
func ttsSpeakBody(cmd *cobra.Command, text string) (map[string]any, error) {
	name := ttsFormatName
	if name == "" {
		if name = ttsExtFormats[strings.ToLower(filepath.Ext(ttsOut))]; name == "" {
			name = "wav"
		}
	}
	preset, ok := ttsFormats[name]
	if !ok {
		return nil, clierr.BadFlag("format", fmt.Sprintf("%q is not wav, mp3, mulaw, alaw or pcm", name))
	}
	if ttsSampleRate < 0 || ttsBitrate < 0 {
		return nil, clierr.BadInput("--sample-rate and --bitrate take a positive number")
	}
	if ttsSampleRate > 0 {
		preset.sampleRate = ttsSampleRate
	}
	format := map[string]any{"container": preset.container, "encoding": preset.encoding, "sample_rate": preset.sampleRate}
	switch {
	case name == "mp3":
		format["bitrate"] = cmp.Or(ttsBitrate, ttsMP3Bitrates[preset.sampleRate], ttsDefaultBitrate)
	case ttsBitrate > 0:
		return nil, clierr.BadFlag("bitrate", "applies to --format mp3 only")
	}

	body := map[string]any{"voice_id": ttsVoice, "text": text, "output_format": format}
	if ttsLanguage != "" {
		i := slices.IndexFunc(ttsLanguages, func(l string) bool { return strings.EqualFold(l, ttsLanguage) })
		if i < 0 {
			e := clierr.BadFlag("language", fmt.Sprintf("%q is not one of %s", ttsLanguage, strings.Join(ttsLanguages, ", ")))
			if strings.EqualFold(ttsLanguage, "en") {
				e.Hint = "There is no plain en. Pick en-US, en-GB, en-IN or en-AU: on an English voice the code sets how numbers and dates are read, and the voice keeps its accent."
			}
			return nil, e
		}
		body["language"] = ttsLanguages[i]
	}
	if cmd.Flags().Changed("pace") {
		if ttsPace < ttsMinPace || ttsPace > ttsMaxPace {
			return nil, clierr.BadFlag("pace", fmt.Sprintf("must be %.1f to %.1f", ttsMinPace, ttsMaxPace))
		}
		body["pace"] = ttsPace
	}
	if cmd.Flags().Changed("volume") {
		if ttsVolume < ttsMinVolume || ttsVolume > ttsMaxVolume {
			return nil, clierr.BadFlag("volume", fmt.Sprintf("must be %.1f to %.1f", ttsMinVolume, ttsMaxVolume))
		}
		body["volume"] = ttsVolume
	}
	if ttsDictionary != "" {
		body["pronunciation_dictionary_id"] = ttsDictionary
	}
	if cmd.Flags().Changed("seed") {
		// A seed only repeats the audio with continuation off.
		body["seed"], body["continuation"] = ttsSeed, false
	}
	return body, nil
}

func runTTSDictionaryList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.TTSPronunciationDictionaryList
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "PronunciationDictionaries"), nil, nil, &resp)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return listJSON(os.Stdout, resp.Raw(), "objects")
	}
	table := [][]string{{"PRONUNCIATION_DICTIONARY_ID", "NAME", "RULES", "VERSION"}}
	for _, d := range resp.Objects {
		table = append(table, []string{d.PronunciationDictionaryID, d.Name, fmt.Sprint(len(d.Rules)), fmt.Sprint(d.Version)})
	}
	return output.Table(os.Stdout, table)
}

func runTTSDictionaryGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var d api.TTSPronunciationDictionary
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "PronunciationDictionaries", args[0]), nil, nil, &d)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, d.Raw())
	}
	if err := output.KV(os.Stdout, [][2]string{
		{"pronunciation_dictionary_id", d.PronunciationDictionaryID}, {"name", d.Name}, {"description", d.Description},
		{"version", fmt.Sprint(d.Version)}, {"created_at", d.CreatedAt},
	}); err != nil {
		return err
	}
	table := [][]string{{"WORD", "KIND", "VALUE", "CASE_SENSITIVE"}}
	for _, r := range d.Rules {
		table = append(table, []string{r.Word, r.Kind, r.Value, fmt.Sprint(r.CaseSensitive)})
	}
	fmt.Println()
	return output.Table(os.Stdout, table)
}

func runTTSDictionaryCreate(cmd *cobra.Command, args []string) error {
	if ttsDictKind != "" && !slices.Contains(ttsKinds, ttsDictKind) {
		return clierr.BadFlag("kind", fmt.Sprintf("%q is not ipa, arpa or respell", ttsDictKind))
	}
	if n := utf8.RuneCountInString(ttsDictName); n == 0 || n > ttsMaxNameChars {
		return clierr.BadFlag("name", fmt.Sprintf("must be 1 to %d characters", ttsMaxNameChars))
	}
	rules, err := ttsDictionaryRules()
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var d api.TTSPronunciationDictionary
	apiErr, err := client.Do("POST", client.AccountURL("TTS", "PronunciationDictionaries"),
		map[string]any{"name": ttsDictName, "rules": rules}, nil, &d)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitResponse(fmt.Sprintf("Created pronunciation dictionary %s (%d rules)", d.PronunciationDictionaryID, len(rules)), d.Raw())
}

// ttsDictionaryRules collects the rules from --file and --rule and checks the
// API's limits, so a bad one is named here and not by a 400. A rule's kind is
// its own, from the file, else --kind.
func ttsDictionaryRules() ([]api.TTSPronunciationRule, error) {
	var rules []api.TTSPronunciationRule
	if ttsDictFile != "" {
		b, err := ttsReadPath(ttsDictFile, "the rules file")
		if err != nil {
			return nil, err
		}
		// The API refuses a key it does not know. Dropping one here would send a
		// rule that means something else, such as a misspelt case_sensitive.
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&rules); err == nil && dec.More() {
			err = errors.New("more than one JSON value")
		}
		if err != nil {
			return nil, clierr.BadFlag("file", fmt.Sprintf(`must be a JSON array of {"word": ..., "kind": ..., "value": ...}: %v`, err))
		}
	}
	for _, s := range ttsDictRules {
		word, value, ok := strings.Cut(s, "=")
		if !ok {
			return nil, clierr.BadFlag("rule", fmt.Sprintf("%q is not word=value", s))
		}
		rules = append(rules, api.TTSPronunciationRule{Word: word, Value: value})
	}
	if len(rules) == 0 || len(rules) > ttsMaxRules {
		return nil, clierr.BadInput(fmt.Sprintf("a pronunciation dictionary takes 1 to %d rules, from --rule or --file; got %d", ttsMaxRules, len(rules)))
	}
	for i, r := range rules {
		if w, v := utf8.RuneCountInString(r.Word), utf8.RuneCountInString(r.Value); w == 0 || w > ttsMaxWordChars || v == 0 || v > ttsMaxValueChars {
			return nil, clierr.BadInput(fmt.Sprintf("rule %q: the word takes 1 to %d characters and the value 1 to %d", r.Word, ttsMaxWordChars, ttsMaxValueChars))
		}
		if rules[i].Kind = cmp.Or(r.Kind, ttsDictKind); !slices.Contains(ttsKinds, rules[i].Kind) {
			return nil, clierr.BadFlag("kind", fmt.Sprintf("rule %q needs a kind of ipa, arpa or respell, from --kind or its own in --file; got %q", r.Word, rules[i].Kind))
		}
	}
	return rules, nil
}

func runTTSDictionaryDelete(cmd *cobra.Command, args []string) error {
	if err := confirmDestructive("delete pronunciation dictionary " + args[0]); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("TTS", "PronunciationDictionaries", args[0]), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite("Deleted pronunciation dictionary "+args[0], map[string]any{"pronunciation_dictionary_id": args[0], "deleted": true})
}

func runTTSUsage(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var u api.TTSUsage
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "Usage"), nil, nil, &u)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	if effectiveFormat() == output.FormatJSON {
		return output.JSONRaw(os.Stdout, u.Raw())
	}
	table := [][]string{{"ROUTE", "REQUESTS", "CHARACTERS", "SPOKEN_CHARS", "AUDIO_SECONDS"}}
	for _, r := range [][2]string{{"batch", "http"}, {"realtime", "live"}} {
		v := u.Routes[r[1]]
		table = append(table, []string{r[0], fmt.Sprint(v.Requests), fmt.Sprint(v.Characters), fmt.Sprint(v.SpokenChars), fmt.Sprintf("%.1f", v.AudioSeconds)})
	}
	return output.Table(os.Stdout, table)
}
