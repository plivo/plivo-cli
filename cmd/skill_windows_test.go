package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cliskill "github.com/plivo/plivo-cli/cli-skill"
	"github.com/plivo/plivo-cli/internal/clierr"
)

// junctionedRepo links the repository's .claude/skills/plivo-cli, with mklink
// /J (no admin rights, and what npx skills makes on Windows), to a folder in
// the repository or outside it. Go leaves a junction unresolved, so --project
// once judged it by its own name.
func junctionedRepo(t *testing.T, inside bool) (copyDir string) {
	t.Helper()
	root, _ := fakeRepo(t, false)
	base := t.TempDir()
	if inside {
		base = filepath.Join(root, ".agents", "skills")
	}
	copyDir = filepath.Join(base, "plivo-cli")
	link := filepath.Join(root, ".claude", "skills", "plivo-cli")
	for _, d := range []string{copyDir, filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, copyDir).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v\n%s", err, out)
	}
	t.Cleanup(func() { os.Remove(link) })
	return copyDir
}

func TestSkillInstall_projectJunction(t *testing.T) {
	for _, inside := range []bool{false, true} {
		copyDir := junctionedRepo(t, inside)
		err, _, _ := execCmd(t, "skill", "install", "--project")
		var ce *clierr.Error
		wrote := fileIs(t, filepath.Join(copyDir, skillFileName), cliskill.SkillMD)
		if inside && (err != nil || !wrote) {
			t.Errorf("a junction into the repository: err = %v, wrote = %v; want it installed", err, wrote)
		}
		if !inside && (!errors.As(err, &ce) || ce.Code != clierr.CodeBadInput || wrote) {
			t.Errorf("a junction out of the repository: err = %v, wrote = %v; want BAD_INPUT and nothing written", err, wrote)
		}
	}
}

func TestSkillUpdate_projectJunction(t *testing.T) {
	for _, inside := range []bool{false, true} {
		copyDir := junctionedRepo(t, inside)
		shipVersion(t, "plivo-cli", olderCLISkill)
		p := filepath.Join(copyDir, skillFileName)
		if err := os.WriteFile(p, []byte(olderCLISkill), 0o644); err != nil {
			t.Fatal(err)
		}
		want := map[bool]string{false: "refused", true: "updated"}[inside]
		rows := skillUpdateRows(t)
		if len(rows) != 1 || rows[0].Result != want || fileIs(t, p, cliskill.SkillMD) != inside {
			t.Errorf("junction inside the repository = %v: rows = %+v, want one %s row", inside, rows, want)
		}
	}
}
