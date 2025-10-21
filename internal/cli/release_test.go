package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseReleaseTargets(t *testing.T) {
	targets, err := parseReleaseTargets([]string{"darwin/arm64", "linux/amd64"})
	require.NoError(t, err)
	require.Len(t, targets, 2)
	require.Equal(t, "darwin", targets[0].GOOS)
	require.Equal(t, "arm64", targets[0].GOARCH)
	require.Equal(t, "linux", targets[1].GOOS)
	require.Equal(t, "amd64", targets[1].GOARCH)
}

func TestParseReleaseTargetsInvalid(t *testing.T) {
	_, err := parseReleaseTargets([]string{"darwin", "linux/"})
	require.Error(t, err)
}

func TestFindModuleRoot(t *testing.T) {
	orig, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})

	projectRoot := filepath.Dir(filepath.Dir(orig))
	require.NoError(t, os.Chdir(filepath.Join(projectRoot, "internal", "cli")))

	root, err := findModuleRoot()
	require.NoError(t, err)
	require.Equal(t, projectRoot, root)
}
