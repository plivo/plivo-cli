// Package cliskill embeds the agent skill file (SKILL.md) into the binary so
// `plivo skill install` can write it out without a network round-trip.
//
// The directory is named cli-skill (matching the repo layout and the GitHub
// raw path used by skills.sh); Go permits a package name that differs from its
// directory, so the import path is github.com/plivo/plivo-cli/cli-skill while
// the package is cliskill.
package cliskill

import _ "embed"

// SkillMD is the contents of SKILL.md, the single-file reference written for
// LLM-agent consumption.
//
//go:embed SKILL.md
var SkillMD string
