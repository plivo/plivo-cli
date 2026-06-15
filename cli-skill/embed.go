// Package cliskill embeds the agent skill file (SKILL.md) so `plivo skill
// install` can write it out without a network round-trip. The directory is
// cli-skill (matching skills.sh's GitHub raw path); the package is cliskill.
package cliskill

import _ "embed"

// SkillMD is the contents of SKILL.md.
//
//go:embed SKILL.md
var SkillMD string
