// Package audiostreamingskill embeds the Plivo Audio Streaming skill file
// (SKILL.md) so `plivo skill install` can write it out without a network
// round-trip. Mirrors the cli-skill package: the directory is
// audio-streaming-skill (matching skills.sh's GitHub raw path); the package
// is audiostreamingskill.
package audiostreamingskill

import _ "embed"

// SkillMD is the contents of SKILL.md.
//
//go:embed SKILL.md
var SkillMD string
