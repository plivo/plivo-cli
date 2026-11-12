// Package ttsskill embeds the Plivo Text-to-Speech skill file (SKILL.md) so
// `plivo skill install` can write it out without a network round-trip. Mirrors
// the cli-skill package: the directory is tts-skill (matching skills.sh's
// GitHub raw path); the package is ttsskill.
package ttsskill

import _ "embed"

// SkillMD is the contents of SKILL.md.
//
//go:embed SKILL.md
var SkillMD string
