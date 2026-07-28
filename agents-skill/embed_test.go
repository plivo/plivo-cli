package agentsskill

import (
	"strings"
	"testing"
)

// A broken //go:embed can yield an empty string rather than a build failure, so
// assert the content is actually there and still carries the parts an agent
// depends on: the frontmatter name (how the skill gets matched), the discovery
// endpoint the whole skill leans on, and the preflight check.
func TestSkillMDEmbedded(t *testing.T) {
	if len(SkillMD) < 2000 {
		t.Fatalf("SkillMD is empty or truncated: %d bytes", len(SkillMD))
	}
	if !strings.HasPrefix(SkillMD, "---\nname: plivo-cx-agents\n") {
		t.Error("skill must open with the YAML frontmatter that names it")
	}
	for _, must := range []string{
		"/AgentNode/",     // the discovery endpoint the skill is built around
		"output_states",   // where source handles come from
		"Preflight check", // the validator section
		"agent_id",        // the resource key
	} {
		if !strings.Contains(SkillMD, must) {
			t.Errorf("embedded skill is missing %q", must)
		}
	}
}
