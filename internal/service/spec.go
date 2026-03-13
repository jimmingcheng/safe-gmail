package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jimmingcheng/safe-gmail/internal/config"
)

// Spec is the rendered service manifest input for one broker instance.
type Spec struct {
	Instance      string
	ConfigPath    string
	BinaryPath    string
	StdoutLogPath string
	StderrLogPath string
}

// BuildSpec converts broker config plus CLI paths into a service render spec.
func BuildSpec(cfg config.Config, configPath, binaryPath string) (Spec, error) {
	configPath = strings.TrimSpace(configPath)
	binaryPath = strings.TrimSpace(binaryPath)
	if configPath == "" {
		return Spec{}, fmt.Errorf("missing config path")
	}
	if binaryPath == "" {
		return Spec{}, fmt.Errorf("missing binary path")
	}

	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return Spec{}, fmt.Errorf("resolve config path: %w", err)
	}
	binaryAbs, err := filepath.Abs(binaryPath)
	if err != nil {
		return Spec{}, fmt.Errorf("resolve binary path: %w", err)
	}

	logDir := defaultLogDir(cfg)
	return Spec{
		Instance:      cfg.Instance,
		ConfigPath:    configAbs,
		BinaryPath:    binaryAbs,
		StdoutLogPath: filepath.Join(logDir, "safe-gmaild.stdout.log"),
		StderrLogPath: filepath.Join(logDir, "safe-gmaild.stderr.log"),
	}, nil
}

// SystemdUnitName returns the suggested user-unit filename.
func SystemdUnitName(instance string) string {
	return fmt.Sprintf("safe-gmaild@%s.service", sanitizeInstance(instance))
}

// LaunchdLabel returns the suggested launchd label.
func LaunchdLabel(instance string) string {
	return fmt.Sprintf("com.safe-gmail.%s", sanitizeInstance(instance))
}

// LaunchdFileName returns the suggested plist filename.
func LaunchdFileName(instance string) string {
	return LaunchdLabel(instance) + ".plist"
}

func defaultLogDir(cfg config.Config) string {
	if strings.TrimSpace(cfg.StatePath) != "" {
		return filepath.Join(filepath.Dir(cfg.StatePath), "logs")
	}
	return filepath.Join(filepath.Dir(cfg.SocketPath), "logs")
}

func sanitizeInstance(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "instance"
	}

	var out []rune
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
			lastDash = false
		case r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			out = append(out, r)
			lastDash = false
		default:
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}

	normalized := strings.Trim(string(out), "-")
	if normalized == "" {
		return "instance"
	}
	return normalized
}
