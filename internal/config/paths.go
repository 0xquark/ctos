package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvConfigDir overrides the config directory when set.
const EnvConfigDir = "CTOS_CONFIG_DIR"

// Options are the command-line inputs that influence where config is found.
type Options struct {
	// Dir is an explicit --config-dir. It wins over everything else.
	Dir string

	// HomeConfig forces the legacy ~/.ctos location (--home-config).
	HomeConfig bool
}

// ResolveDir picks the config directory, in this order:
//
//  1. --config-dir
//  2. $CTOS_CONFIG_DIR
//  3. ~/.ctos            (only with --home-config)
//  4. $XDG_CONFIG_HOME/ctos
//  5. ~/.config/ctos
//  6. ~/.ctos            (only if it already exists)
//
// The directory is not required to exist; `ctos init` creates it.
func ResolveDir(opts Options) (string, error) {
	if opts.Dir != "" {
		return ExpandPath(opts.Dir), nil
	}
	if env := os.Getenv(EnvConfigDir); env != "" {
		return ExpandPath(env), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	legacy := filepath.Join(home, ".ctos")

	if opts.HomeConfig {
		return legacy, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctos"), nil
	}
	// Prefer an existing legacy directory over creating a new XDG one, so
	// upgrading users keep their dashboards.
	if fi, err := os.Stat(legacy); err == nil && fi.IsDir() {
		return legacy, nil
	}
	return filepath.Join(home, ".config", "ctos"), nil
}

// ExpandPath expands a leading ~ to the user's home directory. Any other ~ is
// left alone, since it is meaningful in some filenames.
func ExpandPath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// DashboardsDir is where dashboard YAML files live inside a config directory.
func DashboardsDir(dir string) string { return filepath.Join(dir, "dashboards") }

// SettingsFile is the global settings file inside a config directory.
func SettingsFile(dir string) string { return filepath.Join(dir, "config.yaml") }
