package cmd

import (
	"strings"
	"testing"

	"github.com/plivo/plivo-cli/internal/config"
)

func TestConfigTelemetry_onOffStatus(t *testing.T) {
	setFakeCreds(t)
	t.Setenv(config.TelemetryEnvVar, "")

	// Fresh config → default on.
	err, stdout, _ := execCmd(t, "-o", "table", "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "telemetry: on") {
		t.Errorf("expected default-on status, got: %q", stdout)
	}

	err, _, stderr := execCmd(t, "config", "telemetry", "off")
	if err != nil {
		t.Fatalf("telemetry off: %v", err)
	}
	if !strings.Contains(stderr, "Telemetry: off") {
		t.Errorf("expected off confirmation, got: %q", stderr)
	}

	err, stdout, _ = execCmd(t, "-o", "table", "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "telemetry: off") {
		t.Errorf("expected off status after toggling, got: %q", stdout)
	}

	err, _, stderr = execCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("telemetry on: %v", err)
	}
	if !strings.Contains(stderr, "Telemetry: on") {
		t.Errorf("expected on confirmation, got: %q", stderr)
	}
}

func TestConfigTelemetry_statusJSON(t *testing.T) {
	setFakeCreds(t)
	t.Setenv(config.TelemetryEnvVar, "")

	err, stdout, _ := execCmd(t, "-o", "json", "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, `"telemetry"`) || !strings.Contains(stdout, `"on"`) {
		t.Errorf("expected JSON envelope with telemetry=on, got: %q", stdout)
	}
}

func TestConfigTelemetry_invalidArg(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "config", "telemetry", "maybe")
	if err == nil || !strings.Contains(err.Error(), `"on", "off", or "status"`) {
		t.Errorf("expected usage error for bad arg, got: %v", err)
	}
}

func TestConfigTelemetry_statusNotesEnvOverride(t *testing.T) {
	setFakeCreds(t)
	t.Setenv(config.TelemetryEnvVar, "0")

	err, stdout, _ := execCmd(t, "-o", "table", "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "telemetry: off") || !strings.Contains(stdout, "forced off by "+config.TelemetryEnvVar) {
		t.Errorf("expected forced-off status with env note, got: %q", stdout)
	}
}

func TestConfigGetSet_telemetry(t *testing.T) {
	setFakeCreds(t)
	t.Setenv(config.TelemetryEnvVar, "")

	err, stdout, _ := execCmd(t, "-o", "table", "config", "get", "telemetry")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.TrimSpace(stdout) != "on" {
		t.Errorf("get telemetry (fresh config) = %q, want on", strings.TrimSpace(stdout))
	}

	err, _, stderr := execCmd(t, "config", "set", "telemetry", "off")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(stderr, "telemetry = off") {
		t.Errorf("expected set confirmation, got: %q", stderr)
	}

	err, stdout, _ = execCmd(t, "-o", "table", "config", "get", "telemetry")
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if strings.TrimSpace(stdout) != "off" {
		t.Errorf("get telemetry after set off = %q, want off", strings.TrimSpace(stdout))
	}
}

func TestConfigSet_acceptsBooleanSynonyms(t *testing.T) {
	setFakeCreds(t)
	for _, v := range []string{"false", "0", "no", "off"} {
		err, _, _ := execCmd(t, "config", "set", "telemetry", v)
		if err != nil {
			t.Fatalf("set telemetry %q: %v", v, err)
		}
		err, stdout, _ := execCmd(t, "-o", "table", "config", "get", "telemetry")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if strings.TrimSpace(stdout) != "off" {
			t.Errorf("set telemetry %q then get = %q, want off", v, strings.TrimSpace(stdout))
		}
	}
}

func TestConfigSet_invalidValue(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "config", "set", "telemetry", "maybe")
	if err == nil || !strings.Contains(err.Error(), "invalid value") {
		t.Errorf("expected invalid-value error, got: %v", err)
	}
}

func TestConfigGetSet_unknownKey(t *testing.T) {
	setFakeCreds(t)
	err, _, _ := execCmd(t, "config", "get", "does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("get: expected unknown-key error, got: %v", err)
	}
	err, _, _ = execCmd(t, "config", "set", "does-not-exist", "x")
	if err == nil || !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("set: expected unknown-key error, got: %v", err)
	}
}
