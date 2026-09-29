// Package firstagentskill embeds the Plivo first-agent skill file (SKILL.md) so
// `plivo skill install` can write it out without a network round-trip.
// Mirrors the cli-skill package: the directory is first-agent-skill (matching
// skills.sh's GitHub raw path); the package is firstagentskill.
package firstagentskill

import _ "embed"

// SkillMD is the contents of SKILL.md.
//
//go:embed SKILL.md
var SkillMD string
