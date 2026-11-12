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
// /J (no admin rights, and what npx skills makes on Windows), to a folder "in"
// the repository, "out" of it, or "gone" (made, then removed). Go leaves a
// junction unresolved, so --project once judged it by its own name.
func junctionedRepo(t *testing.T, where string) (copyDir string) {
	t.Helper()
	root, _ := fakeRepo(t, false)
	copyDir = filepath.Join(t.TempDir(), "plivo-cli")
	if where == "in" {
		copyDir = filepath.Join(root, ".agents", "skills", "plivo-cli")
	}
	link := filepath.Join(root, ".claude", "skills", "plivo-cli")
	_ = os.MkdirAll(copyDir, 0o755) // a failure here fails the mklink below
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, copyDir).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v\n%s", err, out)
	}
	t.Cleanup(func() { os.Remove(link) })
	if where == "gone" {
		_ = os.RemoveAll(copyDir)
	}
	return copyDir
}

func TestSkillInstall_projectJunction(t *testing.T) {
	for _, where := range []string{"in", "out", "gone"} {
		copyDir := junctionedRepo(t, where)
		err, _, _ := execCmd(t, "skill", "install", "--project")
		var ce *clierr.Error
		refused := errors.As(err, &ce) && ce.Code == clierr.CodeBadInput
		wrote := fileIs(t, filepath.Join(copyDir, skillFileName), cliskill.SkillMD)
		if refused != (where != "in") || wrote != (where == "in") || (err != nil) != refused {
			t.Errorf("a junction %q: err = %v, wrote = %v; want it installed only when it is \"in\" the repository, else BAD_INPUT", where, err, wrote)
		}
	}
}

func TestSkillUpdate_projectJunction(t *testing.T) {
	for _, where := range []string{"in", "out"} {
		copyDir := junctionedRepo(t, where)
		shipVersion(t, "plivo-cli", olderCLISkill)
		p := filepath.Join(copyDir, skillFileName)
		if err := os.WriteFile(p, []byte(olderCLISkill), 0o644); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"in": "updated", "out": "refused"}[where]
		rows := skillUpdateRows(t)
		if len(rows) != 1 || rows[0].Result != want || fileIs(t, p, cliskill.SkillMD) != (where == "in") {
			t.Errorf("a junction %q: rows = %+v, want one %s row", where, rows, want)
		}
	}
}
