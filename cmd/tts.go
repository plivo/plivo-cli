package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/plivo/plivo-cli/internal/api"
	"github.com/plivo/plivo-cli/internal/clierr"
	"github.com/plivo/plivo-cli/internal/output"
	"github.com/spf13/cobra"
)

// Limits from the Plivo TTS API reference.
const (
	ttsMaxChars       = 5000
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

var ttsLanguages = []string{"auto", "en", "hi", "bn", "ta", "te", "mr", "gu", "kn"}

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

// ttsExtFormats picks a --format from the --out extension.
var ttsExtFormats = map[string]string{
	".wav": "wav", ".mp3": "mp3", ".ulaw": "mulaw", ".mulaw": "mulaw",
	".alaw": "alaw", ".pcm": "pcm", ".raw": "pcm",
}

var ttsCmd = &cobra.Command{
	Use:   "tts",
	Short: "Text-to-speech: list voices, speak text to an audio file, manage lexicons",
	Args:  cobra.NoArgs,
	RunE:  groupRunE,
}

var ttsVoicesCmd = &cobra.Command{
	Use:   "voices",
	Short: "List the voices you can speak with",
	Long: `List the voices you can speak with: Plivo's public voices and any your
organization owns. Each voice speaks one language. --language and --gender
filter the list here; the API has no filters.`,
	Example: `  plivo tts voices --language hi
  plivo tts voices --gender female -o json`,
	Args: cobra.NoArgs,
	RunE: runTTSVoices,
}

var ttsSpeakCmd = &cobra.Command{
	Use:   "speak",
	Short: "Turn text into an audio file (billed per character)",
	Long: `Turn text into an audio file with a Plivo voice.

Batch text-to-speech is billed per character, so it needs --yes; --dry-run
prints the request and sends nothing. Text is up to 5000 characters, from
--text or --file (- reads stdin).

--format picks the audio: wav (24 kHz), mp3 (44.1 kHz, 128 kbps), mulaw or alaw
(8 kHz telephony, raw), pcm (24 kHz, raw). Without it the --out extension
decides (.wav .mp3 .ulaw .alaw .pcm), and anything else is wav. --out - writes
the audio to stdout, which is refused on a terminal.`,
	Example: `  plivo tts speak --voice r4_ashley --text "Hello from Plivo" --out hello.wav --dry-run
  plivo tts speak --voice r4_ashley --text "Hello from Plivo" --out hello.wav --yes
  plivo tts speak --voice r4_riya --language hi --file msg.txt --out msg.ulaw --yes`,
	Args: cobra.NoArgs,
	RunE: runTTSSpeak,
}

var ttsLexiconCmd = &cobra.Command{
	Use:     "lexicons",
	Aliases: []string{"lexicon"},
	Short:   "Manage pronunciation lexicons",
	Args:    cobra.NoArgs,
	RunE:    groupRunE,
}

var ttsLexiconListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pronunciation lexicons",
	Args:  cobra.NoArgs,
	RunE:  runTTSLexiconList,
}

var ttsLexiconGetCmd = &cobra.Command{
	Use:   "get <lexicon_id>",
	Short: "Get a lexicon and its rules",
	Args:  cobra.ExactArgs(1),
	RunE:  runTTSLexiconGet,
}

var ttsLexiconCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a lexicon of word pronunciations",
	Long: `Create a lexicon: up to 100 rules, each a word and how to say it. --kind sets
how every value is written: ipa, arpa or respell. Give the rules with --rule
word=value (repeatable) or --file, a JSON array of {"word": ..., "value": ...}.
Use the lexicon with plivo tts speak --lexicon <lexicon_id>.`,
	Example: `  plivo tts lexicon create --name brands --kind respell --rule "Plivo=PLEE-voh"`,
	Args:    cobra.NoArgs,
	RunE:    runTTSLexiconCreate,
}

var ttsLexiconDeleteCmd = &cobra.Command{
	Use:   "delete <lexicon_id>",
	Short: "Delete a lexicon (requires --yes)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTTSLexiconDelete,
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
	ttsLexicon    string
	ttsSeed       int64

	ttsLexName  string
	ttsLexKind  string
	ttsLexFile  string
	ttsLexRules []string
)

func init() {
	ttsVoicesCmd.Flags().StringVar(&ttsVoicesLanguage, "language", "", "only voices for this language code (en, hi, bn, ta, te, mr, gu, kn)")
	ttsVoicesCmd.Flags().StringVar(&ttsVoicesGender, "gender", "", "only voices of this gender (male or female)")

	f := ttsSpeakCmd.Flags()
	f.StringVar(&ttsVoice, "voice", "", "voice_id from plivo tts voices (required)")
	f.StringVar(&ttsText, "text", "", "text to speak, up to 5000 characters")
	f.StringVar(&ttsFile, "file", "", "read the text from this file (- for stdin)")
	f.StringVar(&ttsOut, "out", "", "file to write the audio to (- for stdout) (required)")
	f.StringVar(&ttsLanguage, "language", "", "language code: "+strings.Join(ttsLanguages, ", ")+" (default auto)")
	f.StringVar(&ttsFormatName, "format", "", "audio format: wav, mp3, mulaw, alaw or pcm (default from the --out extension)")
	f.IntVar(&ttsSampleRate, "sample-rate", 0, "sample rate in Hz (default from --format)")
	f.IntVar(&ttsBitrate, "bitrate", 0, "mp3 bitrate in bits per second (default 128000)")
	f.Float64Var(&ttsPace, "pace", 1.0, "speaking pace, 0.7 to 1.2")
	f.Float64Var(&ttsVolume, "volume", 1.0, "volume, 0.5 to 2.0")
	f.StringVar(&ttsLexicon, "lexicon", "", "lexicon_id of a saved lexicon to apply")
	f.Int64Var(&ttsSeed, "seed", 0, "repeatable output: the same text, voice and seed give identical audio")
	_ = ttsSpeakCmd.MarkFlagRequired("voice")
	_ = ttsSpeakCmd.MarkFlagRequired("out")

	g := ttsLexiconCreateCmd.Flags()
	g.StringVar(&ttsLexName, "name", "", "lexicon name, up to 100 characters (required)")
	g.StringVar(&ttsLexKind, "kind", "", "how the values are written: ipa, arpa or respell (required)")
	g.StringVar(&ttsLexFile, "file", "", "JSON file: an array of {\"word\": ..., \"value\": ...}")
	g.StringArrayVar(&ttsLexRules, "rule", nil, "a rule as word=value (repeatable)")
	_ = ttsLexiconCreateCmd.MarkFlagRequired("name")
	_ = ttsLexiconCreateCmd.MarkFlagRequired("kind")

	ttsLexiconCmd.AddCommand(ttsLexiconListCmd, ttsLexiconGetCmd, ttsLexiconCreateCmd, ttsLexiconDeleteCmd)
	ttsCmd.AddCommand(ttsVoicesCmd, ttsSpeakCmd, ttsLexiconCmd, ttsUsageCmd)
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
		if ttsVoicesLanguage != "" && !slices.ContainsFunc(v.Languages, func(l string) bool { return strings.EqualFold(l, ttsVoicesLanguage) }) {
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
	if ttsOut == "-" && isTerminal(int(os.Stdout.Fd())) {
		e := clierr.BadFlag("out", "refusing to write audio to a terminal")
		e.Hint = "Give a file name, or pipe the output: plivo tts speak ... --out - --yes | afplay -"
		return e
	}

	client, _, err := getClient()
	if err != nil {
		return err
	}
	proceed, dryRun, gerr := guardSpend(fmt.Sprintf("speak %d characters with voice %s", chars, ttsVoice))
	if !proceed {
		return gerr
	}
	applyDryRun(client, dryRun)

	var audio api.BinaryBody
	apiErr, err := client.Do("POST", client.AccountURL("TTS"), body, nil, &audio)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if client.DryRun {
		return nil
	}
	if len(audio.Data) == 0 {
		return clierr.Wrap(fmt.Errorf("Plivo returned no audio"))
	}
	if ttsOut == "-" {
		_, err = os.Stdout.Write(audio.Data)
		fmt.Fprintf(os.Stderr, "Wrote %d bytes of %s to stdout\n", len(audio.Data), audio.ContentType)
		return err
	}
	if err := os.WriteFile(ttsOut, audio.Data, 0o644); err != nil {
		return clierr.Wrap(err)
	}
	return emitWrite(fmt.Sprintf("Wrote %d bytes of %s to %s (%d characters)", len(audio.Data), audio.ContentType, ttsOut, chars),
		map[string]any{"out": ttsOut, "bytes": len(audio.Data), "content_type": audio.ContentType, "characters": chars, "voice_id": ttsVoice})
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
			b, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		} else {
			b, err = os.ReadFile(ttsFile)
		}
		if err != nil {
			return "", clierr.Wrap(err)
		}
		text = string(b)
	}
	// Padding is billed as characters and says nothing: a file ends in a newline.
	if text = strings.TrimSpace(text); text == "" {
		return "", clierr.BadInput("the text is empty")
	}
	if n := utf8.RuneCountInString(text); n > ttsMaxChars {
		e := clierr.BadInput(fmt.Sprintf("the text is %d characters; one request takes up to %d", n, ttsMaxChars))
		e.Hint = "Split the text and run one request per part."
		return "", e
	}
	return text, nil
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
		format["bitrate"] = cmp.Or(ttsBitrate, ttsDefaultBitrate)
	case ttsBitrate > 0:
		return nil, clierr.BadFlag("bitrate", "applies to --format mp3 only")
	}

	body := map[string]any{"voice_id": ttsVoice, "text": text, "output_format": format}
	if ttsLanguage != "" {
		if !slices.Contains(ttsLanguages, ttsLanguage) {
			return nil, clierr.BadFlag("language", fmt.Sprintf("%q is not one of %s", ttsLanguage, strings.Join(ttsLanguages, ", ")))
		}
		body["language"] = ttsLanguage
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
	if ttsLexicon != "" {
		body["lexicon_id"] = ttsLexicon
	}
	if cmd.Flags().Changed("seed") {
		// A seed only repeats the audio with continuation off.
		body["seed"], body["continuation"] = ttsSeed, false
	}
	return body, nil
}

func runTTSLexiconList(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var resp api.TTSLexiconList
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "Lexicons"), nil, nil, &resp)
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
	table := [][]string{{"LEXICON_ID", "NAME", "KIND", "RULES", "VERSION"}}
	for _, l := range resp.Objects {
		table = append(table, []string{l.LexiconID, l.Name, l.Kind, fmt.Sprint(len(l.Rules)), fmt.Sprint(l.Version)})
	}
	return output.Table(os.Stdout, table)
}

func runTTSLexiconGet(cmd *cobra.Command, args []string) error {
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var l api.TTSLexicon
	apiErr, err := client.Do("GET", client.AccountURL("TTS", "Lexicons", args[0]), nil, nil, &l)
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
		return output.JSONRaw(os.Stdout, l.Raw())
	}
	if err := output.KV(os.Stdout, [][2]string{
		{"lexicon_id", l.LexiconID}, {"name", l.Name}, {"kind", l.Kind}, {"version", fmt.Sprint(l.Version)},
	}); err != nil {
		return err
	}
	table := [][]string{{"WORD", "VALUE"}}
	for _, r := range l.Rules {
		table = append(table, []string{r.Word, r.Value})
	}
	fmt.Println()
	return output.Table(os.Stdout, table)
}

func runTTSLexiconCreate(cmd *cobra.Command, args []string) error {
	if !slices.Contains([]string{"ipa", "arpa", "respell"}, ttsLexKind) {
		return clierr.BadFlag("kind", fmt.Sprintf("%q is not ipa, arpa or respell", ttsLexKind))
	}
	if n := utf8.RuneCountInString(ttsLexName); n == 0 || n > ttsMaxNameChars {
		return clierr.BadFlag("name", fmt.Sprintf("must be 1 to %d characters", ttsMaxNameChars))
	}
	rules, err := ttsLexiconRules()
	if err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	var l api.TTSLexicon
	apiErr, err := client.Do("POST", client.AccountURL("TTS", "Lexicons"),
		map[string]any{"name": ttsLexName, "kind": ttsLexKind, "rules": rules}, nil, &l)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitResponse(fmt.Sprintf("Created lexicon %s (%d rules)", l.LexiconID, len(rules)), l.Raw())
}

// ttsLexiconRules collects the rules from --file and --rule and checks the
// API's limits, so a bad one is named here and not by a 422.
func ttsLexiconRules() ([]api.TTSLexiconRule, error) {
	var rules []api.TTSLexiconRule
	if ttsLexFile != "" {
		b, err := os.ReadFile(ttsLexFile)
		if err != nil {
			return nil, clierr.Wrap(err)
		}
		if err := json.Unmarshal(b, &rules); err != nil {
			return nil, clierr.BadFlag("file", `must be a JSON array of {"word": ..., "value": ...}`)
		}
	}
	for _, s := range ttsLexRules {
		word, value, ok := strings.Cut(s, "=")
		if !ok {
			return nil, clierr.BadFlag("rule", fmt.Sprintf("%q is not word=value", s))
		}
		rules = append(rules, api.TTSLexiconRule{Word: word, Value: value})
	}
	if len(rules) == 0 || len(rules) > ttsMaxRules {
		return nil, clierr.BadInput(fmt.Sprintf("a lexicon takes 1 to %d rules, from --rule or --file; got %d", ttsMaxRules, len(rules)))
	}
	for _, r := range rules {
		if w, v := utf8.RuneCountInString(r.Word), utf8.RuneCountInString(r.Value); w == 0 || w > ttsMaxWordChars || v == 0 || v > ttsMaxValueChars {
			return nil, clierr.BadInput(fmt.Sprintf("rule %q: the word takes 1 to %d characters and the value 1 to %d", r.Word, ttsMaxWordChars, ttsMaxValueChars))
		}
	}
	return rules, nil
}

func runTTSLexiconDelete(cmd *cobra.Command, args []string) error {
	if err := confirmDestructive("delete lexicon " + args[0]); err != nil {
		return err
	}
	client, _, err := getClient()
	if err != nil {
		return err
	}
	apiErr, err := client.Do("DELETE", client.AccountURL("TTS", "Lexicons", args[0]), nil, nil, nil)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if dryRunFlag {
		return nil
	}
	return emitWrite("Deleted lexicon "+args[0], map[string]any{"lexicon_id": args[0], "deleted": true})
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
