//go:build !windows

package agent_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Not parallel: syscall.Umask is process-global.
func TestSessionStore_WriteFileCreatesDirectories0700(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)

	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := agent.OpenSessionStoreAt(&storeStubAgent{dir: storeDir, resolve: joinResolve}, storeDir)
	require.NoError(t, err)

	require.NoError(t, store.WriteFile("nested/sub/session.jsonl", []byte("hi\n"), 0o600))

	for _, dir := range []string{storeDir, filepath.Join(storeDir, "nested"), filepath.Join(storeDir, "nested", "sub")} {
		info, statErr := os.Stat(dir)
		require.NoError(t, statErr, dir)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}

	info, statErr := os.Stat(filepath.Join(storeDir, "nested", "sub", "session.jsonl"))
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
