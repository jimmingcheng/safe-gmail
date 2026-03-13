package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

var launchdHeader = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
`)

type plist struct {
	XMLName xml.Name  `xml:"plist"`
	Version string    `xml:"version,attr"`
	Dict    plistDict `xml:"dict"`
}

type plistDict struct {
	Entries []plistEntry `xml:",any"`
}

type plistEntry struct {
	XMLName xml.Name
	Value   any `xml:",innerxml"`
}

// LaunchdPlist renders a per-user launchd plist for the broker instance.
func LaunchdPlist(spec Spec) (string, error) {
	if err := validateSpec(spec); err != nil {
		return "", err
	}

	data := []plistEntry{
		stringEntry("Label", LaunchdLabel(spec.Instance)),
		arrayEntry("ProgramArguments", []string{
			spec.BinaryPath,
			"run",
			"--config",
			spec.ConfigPath,
		}),
		boolEntry("RunAtLoad", true),
		boolEntry("KeepAlive", true),
		stringEntry("StandardOutPath", spec.StdoutLogPath),
		stringEntry("StandardErrorPath", spec.StderrLogPath),
	}

	doc := plist{
		Version: "1.0",
		Dict: plistDict{
			Entries: data,
		},
	}

	var out bytes.Buffer
	out.Write(launchdHeader)
	enc := xml.NewEncoder(&out)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("render launchd plist: %w", err)
	}
	out.WriteByte('\n')
	return out.String(), nil
}

func stringEntry(key, value string) plistEntry {
	return plistEntry{
		XMLName: xml.Name{Local: "key"},
		Value:   xmlEscape(key) + `</key><string>` + xmlEscape(value) + `</string>`,
	}
}

func boolEntry(key string, value bool) plistEntry {
	boolTag := "false"
	if value {
		boolTag = "true"
	}
	return plistEntry{
		XMLName: xml.Name{Local: "key"},
		Value:   xmlEscape(key) + `</key><` + boolTag + `/>`,
	}
}

func arrayEntry(key string, values []string) plistEntry {
	var buf bytes.Buffer
	buf.WriteString(xmlEscape(key))
	buf.WriteString(`</key><array>`)
	for _, value := range values {
		buf.WriteString(`<string>`)
		buf.WriteString(xmlEscape(value))
		buf.WriteString(`</string>`)
	}
	buf.WriteString(`</array>`)
	return plistEntry{
		XMLName: xml.Name{Local: "key"},
		Value:   buf.String(),
	}
}

func xmlEscape(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
