package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime configuration for the bridge.
// Fields are populated from env.lsp, an optional env.custom override file,
// and CLI flag overrides (last writer wins).
type Config struct {
	Workspace string
	Port      string
	LogPath   string
	PyrightBin string
	TSSBin     string
	RustBin    string
	GoplsBin   string
	KotlinBin  string
	MetalsBin  string
	BashBin    string
	YamlBin    string
	TaploBin   string
	JSONBin    string
	HTMLBin    string
	CSSBin     string
}

var envKeyToField = map[string]func(*Config, string){
	"LSP_WORKSPACE":   func(c *Config, v string) { c.Workspace = v },
	"LSP_PORT":        func(c *Config, v string) { c.Port = v },
	"LSP_LOG":         func(c *Config, v string) { c.LogPath = v },
	"LSP_PYRIGHT_BIN": func(c *Config, v string) { c.PyrightBin = v },
	"LSP_TSS_BIN":     func(c *Config, v string) { c.TSSBin = v },
	"LSP_RUST_BIN":    func(c *Config, v string) { c.RustBin = v },
	"LSP_GOPLS_BIN":   func(c *Config, v string) { c.GoplsBin = v },
	"LSP_KOTLIN_BIN":  func(c *Config, v string) { c.KotlinBin = v },
	"LSP_METALS_BIN":  func(c *Config, v string) { c.MetalsBin = v },
	"LSP_BASH_BIN":    func(c *Config, v string) { c.BashBin = v },
	"LSP_YAML_BIN":    func(c *Config, v string) { c.YamlBin = v },
	"LSP_TAPLO_BIN":   func(c *Config, v string) { c.TaploBin = v },
	"LSP_JSON_BIN":    func(c *Config, v string) { c.JSONBin = v },
	"LSP_HTML_BIN":    func(c *Config, v string) { c.HTMLBin = v },
	"LSP_CSS_BIN":     func(c *Config, v string) { c.CSSBin = v },
}

// Load builds a Config from two env files and optional flag overrides.
//
// Load order (last wins): defaults → envLsp → envCustom → flags.
// envCustom missing is not an error. envLsp missing is an error.
// flags maps env.lsp key names (e.g. "LSP_PORT") to override values.
func Load(envLsp, envCustom string, flags map[string]string) (*Config, error) {
	c := &Config{
		Port: "7890",
	}

	if err := applyFile(c, envLsp); err != nil {
		return nil, fmt.Errorf("load %s: %w", envLsp, err)
	}

	if envCustom != "" {
		if err := applyFile(c, envCustom); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("load %s: %w", envCustom, err)
		}
	}

	for k, v := range flags {
		if setter, ok := envKeyToField[k]; ok {
			setter(c, v)
		}
	}

	return c, nil
}

// applyFile reads a KEY=VALUE env file and merges it into c.
func applyFile(c *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := line[idx+1:] // preserve everything after first '=', including spaces
		if setter, ok := envKeyToField[key]; ok {
			setter(c, val)
		}
	}
	return scanner.Err()
}
