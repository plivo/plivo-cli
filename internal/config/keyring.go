package config

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// keyringService is the service name under which per-profile auth tokens are
// stored in the OS secret store: Keychain (macOS), Secret Service / libsecret
// (Linux), Credential Manager (Windows). The account key is the profile name.
const keyringService = "plivo-cli"

// SetToken stores a profile's auth token in the OS keychain.
func SetToken(profile, token string) error {
	return keyring.Set(keyringService, profile, token)
}

// GetToken retrieves a profile's auth token from the OS keychain. A missing
// entry returns ("", nil) so callers can fall back to other credential sources
// without special-casing the not-found error.
func GetToken(profile string) (string, error) {
	tok, err := keyring.Get(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return tok, err
}

// DeleteToken removes a profile's auth token from the OS keychain. A missing
// entry is not treated as an error.
func DeleteToken(profile string) error {
	err := keyring.Delete(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
