package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoadRPCFromFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ConfigFile), []byte("SAW_RPC_8453=https://example.base\nSAW_SOLANA_RPC=https://sol.example\n"), 0o600))
	t.Setenv("SAW_RPC_84532", "https://sepolia.example")
	cfg, err := config.Load()
	require.NoError(t, err)
	u, err := cfg.EVMRPCURL(8453)
	require.NoError(t, err)
	require.Equal(t, "https://example.base", u)
	require.Equal(t, "https://sol.example", cfg.SolanaRPC)
	u2, err := cfg.EVMRPCURL(84532)
	require.NoError(t, err)
	require.Equal(t, "https://sepolia.example", u2)
}
