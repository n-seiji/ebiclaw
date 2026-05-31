package internal

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-seiji/ebiclaw/pkg/config"
)

func TestGetConfigPath(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := filepath.Join("/tmp/home", ".ebiclaw", "config.json")

	assert.Equal(t, want, got)
}

func TestGetConfigPath_WithEBICLAW_HOME(t *testing.T) {
	t.Setenv(config.EnvHome, "/custom/ebiclaw")
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := filepath.Join("/custom/ebiclaw", "config.json")

	assert.Equal(t, want, got)
}

func TestGetConfigPath_WithEBICLAW_CONFIG(t *testing.T) {
	t.Setenv("EBICLAW_CONFIG", "/custom/config.json")
	t.Setenv(config.EnvHome, "/custom/ebiclaw")
	t.Setenv("HOME", "/tmp/home")

	got := GetConfigPath()
	want := "/custom/config.json"

	assert.Equal(t, want, got)
}

func TestGetConfigPath_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific HOME behavior varies; run on windows")
	}

	testUserProfilePath := `C:\Users\Test`
	t.Setenv("USERPROFILE", testUserProfilePath)

	got := GetConfigPath()
	want := filepath.Join(testUserProfilePath, ".ebiclaw", "config.json")

	require.True(t, strings.EqualFold(got, want), "GetConfigPath() = %q, want %q", got, want)
}
