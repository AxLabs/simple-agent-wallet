package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/wallet"
	"github.com/stretchr/testify/require"
)

func withTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
}

func TestMultiFamilyStore(t *testing.T) {
	withTempConfig(t)
	w := &store.Wallet{Version: 1}
	evm, err := wallet.ImportEVMPrivateKey("0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = evm
	sol, err := wallet.CreateSolana()
	require.NoError(t, err)
	w.Solana = sol
	hed, err := wallet.ImportHedera("0.0.1234", "302e020100300506032b657004220420"+ // will fail parse on pay but store ok
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "testnet")
	require.NoError(t, err)
	w.Hedera = hed
	require.NoError(t, w.Save(""))

	path, _ := config.WalletPath()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	loaded, err := store.Load("")
	require.NoError(t, err)
	require.True(t, loaded.HasFamily(store.FamilyEVM))
	require.True(t, loaded.HasFamily(store.FamilySolana))
	require.True(t, loaded.HasFamily(store.FamilyHedera))
	st, err := loaded.Status()
	require.NoError(t, err)
	require.NotNil(t, st.EVM)
	require.NotContains(t, *st.EVM, "ac0974") // address only
	require.Equal(t, filepath.Join(os.Getenv(config.EnvConfigDir), "wallet.json"), st.Path)
}

func TestEncryptedStore(t *testing.T) {
	withTempConfig(t)
	w := &store.Wallet{Version: 1}
	slot, err := wallet.ImportEVMPrivateKey("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = slot
	require.NoError(t, w.Save("s3cret"))

	_, err = store.Load("")
	require.Error(t, err)

	loaded, err := store.Load("s3cret")
	require.NoError(t, err)
	require.Equal(t, slot.Address, loaded.EVM.Address)
	require.Equal(t, slot.PrivateKey, loaded.EVM.PrivateKey)
}

func TestEVMMnemonicRoundTrip(t *testing.T) {
	slot, mnemonic, err := wallet.CreateEVM(0)
	require.NoError(t, err)
	require.NotEmpty(t, mnemonic)
	again, err := wallet.ImportEVMMnemonic(mnemonic, 0)
	require.NoError(t, err)
	require.Equal(t, slot.Address, again.Address)
	require.Equal(t, slot.PrivateKey, again.PrivateKey)
}

func TestSolanaMnemonic(t *testing.T) {
	const m = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	slot, err := wallet.ImportSolanaMnemonic(m, 0)
	require.NoError(t, err)
	require.NotEmpty(t, slot.Address)
	require.NotEmpty(t, slot.PrivateKey)
}
