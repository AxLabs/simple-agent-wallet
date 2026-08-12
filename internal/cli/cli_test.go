package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCLIConfirmGating(t *testing.T) {
	bin := buildSaw(t)
	cmd := exec.Command(bin, "pay", "http://127.0.0.1:9/")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.Error(t, err)
	require.Contains(t, stderr.String(), "--confirm")
}

func TestCLINoSecretsInStatus(t *testing.T) {
	bin := buildSaw(t)
	dir := t.TempDir()
	key := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	cmd := exec.Command(bin, "init", "import", "--family", "evm")
	cmd.Env = append(os.Environ(), "SAW_CONFIG_DIR="+dir)
	cmd.Stdin = strings.NewReader(key)
	require.NoError(t, cmd.Run())

	status := exec.Command(bin, "init", "status")
	status.Env = append(os.Environ(), "SAW_CONFIG_DIR="+dir)
	out, err := status.CombinedOutput()
	require.NoError(t, err)
	require.NotContains(t, string(out), key)
	require.Contains(t, string(out), "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
}

func buildSaw(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "saw")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/saw")
	cmd.Dir = findModuleRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
