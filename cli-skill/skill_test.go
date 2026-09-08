package cliskill

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// listJQLine matches a shell line that pipes a `plivo ... list ...` command
// into a quoted jq filter, capturing the filter body. Every jq example in
// SKILL.md lives on a single line, so a line-scoped regex is enough — no
// need for a full markdown/shell parser here.
var listJQLine = regexp.MustCompile(`(?m)^.*\bplivo\b.*\blist\b.*\|\s*jq(?:\s+-\S+)*\s+'([^']*)'.*$`)

// staleListEnvelopeErrors scans md for `plivo ... list ... | jq '...'`
// examples that still address the retired v0.2.x envelope shape (rows at
// data[...]) instead of the current one (rows at data.objects[...]).
// Returns one message per offending example, each explaining the fix.
func staleListEnvelopeErrors(md string) []string {
	var errs []string
	for _, m := range listJQLine.FindAllStringSubmatch(md, -1) {
		line, jqExpr := m[0], m[1]
		if !strings.Contains(jqExpr, ".data[") {
			continue
		}
		want := strings.Replace(jqExpr, ".data[", ".data.objects[", 1)
		errs = append(errs, "list-command example uses the retired v0.2.x envelope shape:\n"+
			"  line: "+strings.TrimSpace(line)+"\n"+
			"  had:  "+jqExpr+"\n"+
			"  want: "+want+"\n"+
			"List commands nest rows under data.objects (see 'Changed in v0.3.0' in SKILL.md); change `.data[` to `.data.objects[`.")
	}
	return errs
}

// TestSkillMDListExamples_useObjectsEnvelope guards a regression that
// shipped once already: v0.3.0 moved list-command rows from data to
// data.objects, but three jq examples in SKILL.md kept pointing at the old
// shape and nothing caught it. This walks every `plivo ... list ... | jq
// '...'` example in the embedded skill file and fails if any still uses the
// retired form.
//
// Single-resource `get` examples are exempt by construction — they don't
// contain the word "list", and they legitimately address `.data` directly
// since only list responses nest rows under `objects`.
func TestSkillMDListExamples_useObjectsEnvelope(t *testing.T) {
	matches := listJQLine.FindAllStringSubmatch(SkillMD, -1)
	if len(matches) == 0 {
		t.Fatal("found no `plivo ... list ... | jq '...'` example in SKILL.md; " +
			"either the file lost its examples or this test's detection regex is " +
			"out of sync with the file's format — check both before assuming it's fine")
	}
	for _, errMsg := range staleListEnvelopeErrors(SkillMD) {
		t.Error(errMsg)
	}
}

// TestStaleListEnvelopeErrors_detection proves the checker actually
// discriminates good/bad/exempt examples, using synthetic input rather than
// editing the real skill file.
func TestStaleListEnvelopeErrors_detection(t *testing.T) {
	cases := []struct {
		name    string
		md      string
		wantErr bool
	}{
		{
			name:    "current shape passes",
			md:      "plivo numbers list -o json | jq '.data.objects[].number'",
			wantErr: false,
		},
		{
			name:    "retired shape is caught",
			md:      "plivo numbers list -o json | jq '.data[].number'",
			wantErr: true,
		},
		{
			name:    "single-resource get is exempt (no 'list' token)",
			md:      "plivo voice calls get <uuid> -o json | jq '.data.duration'",
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := staleListEnvelopeErrors(tc.md)
			if tc.wantErr && len(errs) == 0 {
				t.Fatalf("expected a stale-envelope error for %q, got none", tc.md)
			}
			if !tc.wantErr && len(errs) != 0 {
				t.Fatalf("expected no error for %q, got: %v", tc.md, errs)
			}
		})
	}
}

// fencedBlocks returns the contents of each ``` fenced code block.
func fencedBlocks(md string) []string {
	var out []string
	var cur []string
	in := false
	for _, ln := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			if in {
				out = append(out, strings.Join(cur, "\n"))
				cur = nil
			}
			in = !in
			continue
		}
		if in {
			cur = append(cur, ln)
		}
	}
	return out
}

var jqDataIndex = regexp.MustCompile(`\bjq\b[^\n']*'[^']*\.data\[`)

// staleListEnvelopeErrorsByBlock catches the multi-line form that the
// line-scoped check cannot see: a list command whose output is captured into a
// variable on one line and filtered by jq several lines later. docs/errors.md
// carried exactly that shape, and the line-scoped checker read it as clean.
func staleListEnvelopeErrorsByBlock(md string) []string {
	var errs []string
	for _, b := range fencedBlocks(md) {
		if !strings.Contains(b, "plivo ") || !strings.Contains(b, "list") {
			continue
		}
		if m := jqDataIndex.FindString(b); m != "" {
			errs = append(errs, "code block runs a list command and filters `.data[` ("+
				strings.TrimSpace(m)+"…); list rows nest under `data.objects`, so use `.data.objects[`")
		}
	}
	return errs
}

// TestRepoDocs_useObjectsEnvelope runs the same check across every markdown
// file in the repo, not just the embedded skill.
//
// The narrow version of this test passed for four releases while README.md and
// docs/errors.md both carried the retired `.data[` form. A checker scoped to
// one file only protects that file; the README is the example most people copy.
func TestRepoDocs_useObjectsEnvelope(t *testing.T) {
	root := ".."
	var checked int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		// COMMANDS.md is generated from the command tree, and CHANGELOG.md
		// documents the old shape on purpose when describing the v0.3.0 break.
		switch filepath.Base(path) {
		case "COMMANDS.md", "CHANGELOG.md":
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for _, errMsg := range staleListEnvelopeErrors(string(b)) {
			t.Errorf("%s: %s", path, errMsg)
		}
		for _, errMsg := range staleListEnvelopeErrorsByBlock(string(b)) {
			t.Errorf("%s: %s", path, errMsg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A path or extension typo would silently check nothing and pass.
	if checked < 8 {
		t.Fatalf("only walked %d markdown files; expected the whole repo — check the walk root", checked)
	}
	t.Logf("checked %d markdown files", checked)
}
