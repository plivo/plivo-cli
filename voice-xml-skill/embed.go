// Package voicexmlskill embeds the Plivo Voice XML skill file (SKILL.md) so
// `plivo skill install` can write it out without a network round-trip.
// Mirrors the cli-skill package: the directory is voice-xml-skill (matching
// skills.sh's GitHub raw path); the package is voicexmlskill.
package voicexmlskill

import _ "embed"

// SkillMD is the contents of SKILL.md.
//
//go:embed SKILL.md
var SkillMD string
