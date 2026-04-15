package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"text/template"
)

var launchdTemplate = template.Must(template.New("launchd").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>{{ xml .Label }}</string>
    <key>ProgramArguments</key>
    <array>
{{- range .ProgramArguments }}
      <string>{{ xml . }}</string>
{{- end }}
    </array>
    <key>RunAtLoad</key>
{{- if .RunAtLoad }}
    <true/>
{{- else }}
    <false/>
{{- end }}
    <key>KeepAlive</key>
{{- if .KeepAlive }}
    <true/>
{{- else }}
    <false/>
{{- end }}
    <key>StandardOutPath</key>
    <string>{{ xml .StdoutLogPath }}</string>
    <key>StandardErrorPath</key>
    <string>{{ xml .StderrLogPath }}</string>
  </dict>
</plist>
`))

type launchdTemplateData struct {
	Label            string
	ProgramArguments []string
	RunAtLoad        bool
	KeepAlive        bool
	StdoutLogPath    string
	StderrLogPath    string
}

// LaunchdPlist renders a per-user launchd plist for the broker instance.
func LaunchdPlist(spec Spec) (string, error) {
	if err := validateSpec(spec); err != nil {
		return "", err
	}

	data := launchdTemplateData{
		Label: LaunchdLabel(spec.Instance),
		ProgramArguments: []string{
			spec.BinaryPath,
			"run",
			"--config",
			spec.ConfigPath,
		},
		RunAtLoad:     true,
		KeepAlive:     true,
		StdoutLogPath: spec.StdoutLogPath,
		StderrLogPath: spec.StderrLogPath,
	}

	var out bytes.Buffer
	if err := launchdTemplate.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render launchd plist: %w", err)
	}
	return out.String(), nil
}

func xmlEscape(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
