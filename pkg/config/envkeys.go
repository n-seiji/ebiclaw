// EbiClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 EbiClaw contributors

package config

import (
	"os"
	"path/filepath"

	"github.com/n-seiji/ebiclaw/pkg"
)

// Runtime environment variable keys for the ebiclaw process.
// These control the location of files and binaries at runtime and are read
// directly via os.Getenv / os.LookupEnv. All ebiclaw-specific keys use the
// EBICLAW_ prefix. Reference these constants instead of inline string
// literals to keep all supported knobs visible in one place and to prevent
// typos.
const (
	// EnvHome overrides the base directory for all ebiclaw data
	// (config, workspace, skills, auth store, …).
	// Default: ~/.ebiclaw
	EnvHome = "EBICLAW_HOME"

	// EnvConfig overrides the full path to the JSON config file.
	// Default: $EBICLAW_HOME/config.json
	EnvConfig = "EBICLAW_CONFIG"

	// EnvBuiltinSkills overrides the directory from which built-in
	// skills are loaded.
	// Default: <cwd>/skills
	EnvBuiltinSkills = "EBICLAW_BUILTIN_SKILLS"

	// EnvBinary overrides the path to the ebiclaw executable.
	// Used by the web launcher when spawning the gateway subprocess.
	// Default: resolved from the same directory as the current executable.
	EnvBinary = "EBICLAW_BINARY"

	// EnvGatewayHost overrides the host address for the gateway server.
	// Default: "127.0.0.1"
	EnvGatewayHost = "EBICLAW_GATEWAY_HOST"
)

func GetHome() string {
	homePath, _ := os.UserHomeDir()
	if ebiclawHome := os.Getenv(EnvHome); ebiclawHome != "" {
		homePath = ebiclawHome
	} else if homePath != "" {
		homePath = filepath.Join(homePath, pkg.DefaultEbiClawHome)
	}
	if homePath == "" {
		homePath = "."
	}
	return homePath
}
