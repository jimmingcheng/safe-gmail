package service

import (
	"fmt"
	"strings"
)

func validateSpec(spec Spec) error {
	if strings.TrimSpace(spec.Instance) == "" {
		return fmt.Errorf("missing instance")
	}
	if strings.TrimSpace(spec.ConfigPath) == "" {
		return fmt.Errorf("missing config path")
	}
	if strings.TrimSpace(spec.BinaryPath) == "" {
		return fmt.Errorf("missing binary path")
	}
	if strings.TrimSpace(spec.StdoutLogPath) == "" {
		return fmt.Errorf("missing stdout log path")
	}
	if strings.TrimSpace(spec.StderrLogPath) == "" {
		return fmt.Errorf("missing stderr log path")
	}
	return nil
}
