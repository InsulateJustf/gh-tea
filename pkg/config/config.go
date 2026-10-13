// Package config stores the multi-host Tea configuration
// (see GLOSSARY.md: Tea, Host, Default, Token, ClientID).
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultPath is where the configuration lives unless overridden by --config.
	DefaultPath = "~/.config/gh-tea/config.yaml"

	// OAuthCallbackURL must exactly match the redirect URI registered in the
	// Gitea OAuth2 application (see ADR 0002).
	OAuthCallbackURL = "http://localhost:1455/callback"

	// OAuth endpoint path templates; Gitea-spec paths, not credentials.
	OAuthAuthURLTmpl  = "%s/login/oauth/authorize"
	OAuthTokenURLTmpl = "%s/login/oauth/access_token"
)

type (
	// TeaName is the user-chosen name of a Tea entry.
	TeaName string

	// TeaItem is one configured Gitea host with its credentials.
	TeaItem struct {
		Name     string `yaml:"name"`
		Host     string `yaml:"host"`
		ClientID string `yaml:"client-id,omitempty"`
		// Token is the raw access token; never printed by any command.
		Token string `yaml:"token,omitempty"`
	}

	// Tea maps TeaName to its entry.
	Tea map[TeaName]TeaItem

	// Config is the whole config file.
	Config struct {
		Tea     Tea     `yaml:"tea"`
		Default TeaName `yaml:"default,omitempty"`
	}
)

func (t TeaName) String() string { return string(t) }

// SortedNames returns all Tea names in stable order (map iteration is random).
func (c Config) SortedNames() []TeaName {
	names := make([]TeaName, 0, len(c.Tea))
	for k := range c.Tea {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// NormalizeHost canonicalizes a host URL so that remote-inference can compare
// it against git remote URLs: lower-cased scheme and host, no trailing slash.
func NormalizeHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid host %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("host %q must start with http:// or https://", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("host %q has no host part", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// Load reads the config file. A missing file is not an error: the empty
// Config is returned so commands can report their own actionable messages.
// A malformed file is fatal, so a typo can never silently wipe the config.
func Load(path string) (Config, error) {
	cfg := Config{Tea: Tea{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Tea == nil {
		cfg.Tea = Tea{}
	}
	return cfg, nil
}

// Save writes the config atomically (temp file + rename) so an interrupted
// write can never truncate the existing file.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gh-tea-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
