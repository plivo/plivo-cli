package voicexmlskill

import (
	"strings"
	"testing"
)

// A broken //go:embed yields an empty string rather than a build failure, so
// assert the content actually made it in and looks like a skill file (YAML
// frontmatter up top).
func TestSkillMDEmbedded(t *testing.T) {
	if SkillMD == "" {
		t.Fatal("SkillMD is empty; //go:embed likely failed silently")
	}
	if !strings.HasPrefix(SkillMD, "---") {
		t.Errorf("SkillMD should start with YAML frontmatter (---), got: %.40q", SkillMD)
	}
}
