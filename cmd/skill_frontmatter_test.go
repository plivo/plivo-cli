package cmd

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var (
	skillNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	xmlTagRe    = regexp.MustCompile(`<[A-Za-z/][^>]*>`)
)

// Loaders with a strict YAML parser, such as the `skills` npm CLI, skip a skill
// whose frontmatter does not parse. An unquoted ": " inside the description is
// enough to do that, and Claude Code's tolerant loader hides it.
func TestBundledSkills_frontmatterFollowsAgentSkillsSpec(t *testing.T) {
	for _, s := range bundledSkills {
		t.Run(s.selector, func(t *testing.T) {
			content := strings.ReplaceAll(s.content, "\r\n", "\n")
			rest, ok := strings.CutPrefix(content, "---\n")
			if !ok {
				t.Fatal("SKILL.md does not open with a frontmatter block")
			}
			block, _, ok := strings.Cut(rest, "\n---\n")
			if !ok {
				t.Fatal("frontmatter block is not closed")
			}

			var fm struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
				t.Fatalf("frontmatter is not valid YAML: %v", err)
			}

			if fm.Name != s.dirName {
				t.Errorf("name %q does not match the install directory %q", fm.Name, s.dirName)
			}
			if !skillNameRe.MatchString(fm.Name) {
				t.Errorf("name %q must be 1-64 lowercase letters, digits or hyphens", fm.Name)
			}
			if fm.Description == "" {
				t.Error("description is empty")
			}
			if n := utf8.RuneCountInString(fm.Description); n > 1024 {
				t.Errorf("description is %d characters; the limit is 1024", n)
			}
			if tag := xmlTagRe.FindString(fm.Description); tag != "" {
				t.Errorf("description contains the XML tag %q", tag)
			}
		})
	}
}
