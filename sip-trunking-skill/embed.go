// Package siptrunkingskill embeds the Plivo SIP trunking skill file
// (SKILL.md) so `plivo skill install` can write it out without a network
// round-trip. Mirrors the cli-skill package: the directory is
// sip-trunking-skill (matching skills.sh's GitHub raw path); the package is
// siptrunkingskill.
package siptrunkingskill

import _ "embed"

// SkillMD is the contents of SKILL.md.
//
//go:embed SKILL.md
var SkillMD string
