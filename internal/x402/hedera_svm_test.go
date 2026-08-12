package x402pay_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/wallet"
	x402pay "github.com/AxLabs/simple-agent-wallet/internal/x402"
	"github.com/stretchr/testify/require"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
	hederaclient "github.com/x402-foundation/x402/go/v2/mechanisms/hedera/exact/client"
	"github.com/x402-foundation/x402/go/v2/types"
)

type fakeHederaSigner struct {
	accountID string
	txB64     string
}

func (f *fakeHederaSigner) AccountID() string { return f.accountID }
func (f *fakeHederaSigner) CreatePartiallySignedTransferTransaction(
	_ context.Context, req types.PaymentRequirements,
) (string, error) {
	if req.Extra == nil {
		return "", fmt.Errorf("feePayer required")
	}
	if _, ok := req.Extra["feePayer"].(string); !ok {
		return "", fmt.Errorf("feePayer required")
	}
	return f.txB64, nil
}

func TestHederaPayloadRequiresFeePayerAndBase64(t *testing.T) {
	tx := base64.StdEncoding.EncodeToString([]byte("hedera-partial-tx"))
	scheme := hederaclient.NewExactHederaScheme(&fakeHederaSigner{
		accountID: "0.0.9001",
		txB64:     tx,
	})
	payload, err := scheme.CreatePaymentPayload(context.Background(), types.PaymentRequirements{
		Scheme:            hederamech.SchemeExact,
		Network:           hederamech.HederaTestnetCAIP2,
		Asset:             hederamech.HBARAssetID,
		Amount:            "50",
		PayTo:             "0.0.7001",
		MaxTimeoutSeconds: 180,
		Extra:             map[string]interface{}{"feePayer": "0.0.5001"},
	})
	require.NoError(t, err)
	require.Equal(t, tx, payload.Payload["transaction"])
}

func TestSolanaFeePayerRequiredInSelect(t *testing.T) {
	v := x402pay.ViewFromV2(0, types.PaymentRequirements{
		Scheme:  "exact",
		Network: "solana:mainnet",
		Asset:   "mint",
		Amount:  "1",
		PayTo:   "payTo",
		Extra:   map[string]interface{}{"feePayer": "Fee111111111111111111111111111111111111111"},
	})
	require.Equal(t, "solana", v.Family)
	require.NotEmpty(t, v.FeePayer)
}

func TestBuildClientRegistersFamilies(t *testing.T) {
	w := &store.Wallet{Version: 1}
	evm, err := wallet.ImportEVMPrivateKey("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = evm
	sol, err := wallet.CreateSolana()
	require.NoError(t, err)
	w.Solana = sol
	w.Hedera = &store.HederaSlot{
		AccountID:  "0.0.9001",
		PrivateKey: "302e020100300506032b657004220420a869f4c6191b9c8c99933e7f6b6611711737e4b1a1a5a4cb5370e719a1f6df98",
		Network:    "hedera:testnet",
	}

	client, err := x402pay.BuildClient(w, &config.Config{}, "eip155:84532")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.Client)
	require.NotNil(t, client.Batch)
}
