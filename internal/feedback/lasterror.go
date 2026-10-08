package feedback

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LastError is the most recent failed command, kept for `plivo feedback
// --bug`. It holds the command path and the error's category: never argument
// values or the error message, either of which can echo what the user typed.
type LastError struct {
	Command    string    `json:"command"` // dotted path, e.g. "voice.calls.get"
	ExitCode   int       `json:"exit_code"`
	ErrorCode  string    `json:"error_code"`
	RequestID  string    `json:"request_id,omitempty"`
	CLIVersion string    `json:"cli_version"`
	OS         string    `json:"os"`
	Arch       string    `json:"arch"`
	Timestamp  time.Time `json:"timestamp"`
}

// LastErrorFile returns ~/.plivo/last-error.json.
func LastErrorFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".plivo", "last-error.json"), nil
}

// SaveLastError replaces the saved failure. It writes a temp file in the same
// directory (CreateTemp makes it 0600) and renames it into place, so a reader
// never sees half a file.
func SaveLastError(e LastError) error {
	p, err := LastErrorFile()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".last-error-*.json")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) // no-op once the rename has succeeded
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// LoadLastError returns the saved failure, or nil when none was recorded or
// the file cannot be parsed.
func LoadLastError() (*LastError, error) {
	p, err := LastErrorFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e LastError
	if json.Unmarshal(data, &e) != nil {
		return nil, nil
	}
	return &e, nil
}
