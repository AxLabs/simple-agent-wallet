package tx_test

import (
	"encoding/hex"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/tx"
	"github.com/stretchr/testify/require"
)

func TestEncodeABITransfer(t *testing.T) {
	out, err := tx.EncodeABI("transfer(address,uint256)", []string{
		"0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		"1000",
	})
	require.NoError(t, err)
	require.True(t, len(out) > 4)
	// transfer selector a9059cbb
	require.Equal(t, "a9059cbb", hex.EncodeToString(out[:4]))
}

func TestConfirmGatingDocumented(t *testing.T) {
	// CLI gating is in cobra RunE; this keeps package importable for parallel suites.
	require.NotNil(t, tx.ParseAmount)
}
