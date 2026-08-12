package x402pay_test

import (
	"context"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/wallet"
	x402pay "github.com/AxLabs/simple-agent-wallet/internal/x402"
	"github.com/stretchr/testify/require"
	batchsettlement "github.com/x402-foundation/x402/go/v2/mechanisms/evm/batch-settlement"
	"github.com/x402-foundation/x402/go/v2/types"
)

func TestSelectBatchSettlementScheme(t *testing.T) {
	accepts := []x402pay.AcceptView{
		{Index: 0, Scheme: "exact", Network: "eip155:8453", Asset: "0xA", Amount: "1", Family: "evm", AssetTransferMethod: "eip3009"},
		{Index: 1, Scheme: "batch-settlement", Network: "eip155:8453", Asset: "0xA", Amount: "1", Family: "evm"},
	}
	a, err := x402pay.SelectAccept(accepts, x402pay.SelectOpts{Scheme: "batch-settlement"})
	require.NoError(t, err)
	require.Equal(t, 1, a.Index)
	require.Equal(t, "batch-settlement", a.Scheme)
}

func TestBatchSettlementPayloadShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	w := &store.Wallet{Version: 1}
	slot, err := wallet.ImportEVMPrivateKey("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = slot

	built, err := x402pay.BuildClient(w, &config.Config{}, "eip155:8453")
	require.NoError(t, err)
	require.NotNil(t, built.Batch)

	payload, err := built.Client.CreatePaymentPayload(context.Background(), types.PaymentRequirements{
		Scheme:            batchsettlement.SchemeBatched,
		Network:           "eip155:8453",
		Asset:             "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		Amount:            "100",
		PayTo:             "0x3333333333333333333333333333333333333333",
		MaxTimeoutSeconds: 60,
		Extra: map[string]interface{}{
			"name":               "USD Coin",
			"version":            "2",
			"receiverAuthorizer": "0x4444444444444444444444444444444444444444",
		},
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, batchsettlement.SchemeBatched, payload.Accepted.Scheme)
	// Fresh channel → deposit-style payload fields
	require.NotEmpty(t, payload.Payload)
	_, hasAuth := payload.Payload["authorization"]
	_, hasPermit2 := payload.Payload["permit2Authorization"]
	_, hasVoucher := payload.Payload["voucher"]
	require.True(t, hasAuth || hasPermit2 || hasVoucher, "expected deposit auth or voucher in payload: %#v", payload.Payload)
}

func TestListChannelsEmpty(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	list, err := x402pay.ListChannels()
	require.NoError(t, err)
	require.Empty(t, list)
}
