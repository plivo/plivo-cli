//go:build internal

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// VibeSession persists the state of an in-progress `plivo agent create` flow
// so that subsequent `plivo agent create -c "..."` invocations can resume the
// same conversation without losing context.
//
// The vibe-agent SSE endpoint is stateful on the server side (keyed on
// session_id), but the LLM also benefits from receiving the current workflow
// JSON on every turn — without it the model re-plans from scratch.
//
// On disk: ~/.plivo/vibe-session.json, mode 0600.
type VibeSession struct {
	SessionID       string `json:"session_id"`
	CurrentWorkflow string `json:"current_workflow,omitempty"`
	AgentName       string `json:"agent_name,omitempty"`
	InitialPrompt   string `json:"initial_prompt,omitempty"`
	StartedAt       string `json:"started_at"`
	LastTurnAt      string `json:"last_turn_at,omitempty"`
	TurnCount       int    `json:"turn_count"`
}

var ErrNoVibeSession = errors.New("no vibe session: start one with `plivo agent create --prompt \"...\"`")

func VibeSessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".plivo", "vibe-session.json"), nil
}

func LoadVibeSession() (*VibeSession, error) {
	p, err := VibeSessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoVibeSession
		}
		return nil, err
	}
	var s VibeSession
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	if s.SessionID == "" {
		return nil, ErrNoVibeSession
	}
	return &s, nil
}

func SaveVibeSession(s *VibeSession) error {
	p, err := VibeSessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	if s.StartedAt == "" {
		s.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.LastTurnAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

func ClearVibeSession() error {
	p, err := VibeSessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
