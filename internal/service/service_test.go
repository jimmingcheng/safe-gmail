package service

import (
	"strings"
	"testing"

	"github.com/jimmingcheng/safe-gmail/internal/config"
)

func TestBuildSpecUsesStatePathLogsAndAbsolutePaths(t *testing.T) {
	t.Parallel()

	spec, err := BuildSpec(config.Config{
		Instance:   "Work Mail",
		SocketPath: "/tmp/safe-gmail.sock",
		StatePath:  "/var/lib/safe-gmail/work/state.json",
	}, "./broker.json", "./bin/safe-gmaild")
	if err != nil {
		t.Fatalf("BuildSpec() error = %v", err)
	}

	if !strings.HasSuffix(spec.ConfigPath, "/broker.json") {
		t.Fatalf("ConfigPath = %q, want absolute broker.json", spec.ConfigPath)
	}
	if !strings.HasSuffix(spec.BinaryPath, "/bin/safe-gmaild") {
		t.Fatalf("BinaryPath = %q, want absolute bin/safe-gmaild", spec.BinaryPath)
	}
	if spec.StdoutLogPath != "/var/lib/safe-gmail/work/logs/safe-gmaild.stdout.log" {
		t.Fatalf("StdoutLogPath = %q", spec.StdoutLogPath)
	}
	if SystemdUnitName("Work Mail") != "safe-gmaild@work-mail.service" {
		t.Fatalf("SystemdUnitName() = %q", SystemdUnitName("Work Mail"))
	}
	if LaunchdLabel("Work Mail") != "com.safe-gmail.work-mail" {
		t.Fatalf("LaunchdLabel() = %q", LaunchdLabel("Work Mail"))
	}
}

func TestSystemdUnitRendersExecStart(t *testing.T) {
	t.Parallel()

	unit, err := SystemdUnit(Spec{
		Instance:      "work",
		ConfigPath:    "/Users/me/.config/safe-gmail/work/broker.json",
		BinaryPath:    "/Users/me/bin/safe-gmaild",
		StdoutLogPath: "/tmp/stdout.log",
		StderrLogPath: "/tmp/stderr.log",
	})
	if err != nil {
		t.Fatalf("SystemdUnit() error = %v", err)
	}

	want := `ExecStart="/Users/me/bin/safe-gmaild" run --config "/Users/me/.config/safe-gmail/work/broker.json"`
	if !strings.Contains(unit, want) {
		t.Fatalf("unit missing ExecStart:\n%s", unit)
	}
	if !strings.Contains(unit, "NoNewPrivileges=yes") {
		t.Fatalf("unit missing NoNewPrivileges:\n%s", unit)
	}
}

func TestLaunchdPlistRendersArgumentsAndLogs(t *testing.T) {
	t.Parallel()

	plist, err := LaunchdPlist(Spec{
		Instance:      "work",
		ConfigPath:    "/Users/me/.config/safe-gmail/work/broker.json",
		BinaryPath:    "/Users/me/bin/safe-gmaild",
		StdoutLogPath: "/Users/me/Library/Logs/safe-gmaild.stdout.log",
		StderrLogPath: "/Users/me/Library/Logs/safe-gmaild.stderr.log",
	})
	if err != nil {
		t.Fatalf("LaunchdPlist() error = %v", err)
	}

	if !strings.Contains(plist, "<string>com.safe-gmail.work</string>") {
		t.Fatalf("plist missing label:\n%s", plist)
	}
	if !strings.Contains(plist, "<string>/Users/me/bin/safe-gmaild</string>") {
		t.Fatalf("plist missing binary path:\n%s", plist)
	}
	if !strings.Contains(plist, "<string>/Users/me/.config/safe-gmail/work/broker.json</string>") {
		t.Fatalf("plist missing config path:\n%s", plist)
	}
	if !strings.Contains(plist, "<string>/Users/me/Library/Logs/safe-gmaild.stderr.log</string>") {
		t.Fatalf("plist missing stderr log path:\n%s", plist)
	}
}
