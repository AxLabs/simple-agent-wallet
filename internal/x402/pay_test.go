package x402pay_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/wallet"
	x402pay "github.com/AxLabs/simple-agent-wallet/internal/x402"
	"github.com/stretchr/testify/require"
	"github.com/x402-foundation/x402/go/v2/types"
)

func TestFamilyOf(t *testing.T) {
	require.Equal(t, "evm", x402pay.FamilyOf("eip155:8453"))
	require.Equal(t, "solana", x402pay.FamilyOf("solana:mainnet"))
	require.Equal(t, "hedera", x402pay.FamilyOf("hedera:testnet"))
	require.Equal(t, "evm", x402pay.FamilyOf("base"))
}

func TestSelectAccept(t *testing.T) {
	accepts := []x402pay.AcceptView{
		{Index: 0, Network: "eip155:8453", Asset: "0xA", Amount: "1", Family: "evm", AssetTransferMethod: "eip3009"},
		{Index: 1, Network: "eip155:8453", Asset: "0xA", Amount: "1", Family: "evm", AssetTransferMethod: "permit2"},
		{Index: 2, Network: "solana:mainnet", Asset: "mint", Amount: "2", Family: "solana", FeePayer: "Fee111"},
		{Index: 3, Network: "hedera:testnet", Asset: "0.0.0", Amount: "3", Family: "hedera", FeePayer: "0.0.1"},
	}
	a, err := x402pay.SelectAccept(accepts, x402pay.SelectOpts{Method: "permit2"})
	require.NoError(t, err)
	require.Equal(t, 1, a.Index)

	a, err = x402pay.SelectAccept(accepts, x402pay.SelectOpts{Network: "hedera:testnet"})
	require.NoError(t, err)
	require.Equal(t, "0.0.1", a.FeePayer)

	_, err = x402pay.SelectAccept(accepts, x402pay.SelectOpts{Network: "eip155:1"})
	require.Error(t, err)
}

func TestInspectV2Header(t *testing.T) {
	pr := types.PaymentRequired{
		X402Version: 2,
		Accepts: []types.PaymentRequirements{{
			Scheme:  "exact",
			Network: "eip155:8453",
			Asset:   "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
			Amount:  "1000",
			PayTo:   "0x1111111111111111111111111111111111111111",
			Extra:   map[string]interface{}{"assetTransferMethod": "permit2", "name": "USD Coin", "version": "2"},
		}, {
			Scheme:  "exact",
			Network: "solana:mainnet",
			Asset:   "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			Amount:  "1000",
			PayTo:   "SoL1111111111111111111111111111111111111112",
			Extra:   map[string]interface{}{"feePayer": "FeePayer111111111111111111111111111111111"},
		}},
	}
	raw, _ := json.Marshal(pr)
	b64 := base64.StdEncoding.EncodeToString(raw)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("PAYMENT-REQUIRED", b64)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	insp, _, _, err := x402pay.Fetch402(context.Background(), "GET", srv.URL, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, insp.X402Version)
	require.Len(t, insp.Accepts, 2)
	require.Equal(t, "permit2", insp.Accepts[0].AssetTransferMethod)
	require.Equal(t, "solana", insp.Accepts[1].Family)
	require.NotEmpty(t, insp.Accepts[1].FeePayer)
}

func TestInspectV1Body(t *testing.T) {
	body := `{"x402Version":1,"accepts":[{"scheme":"exact","network":"base","maxAmountRequired":"500","resource":"https://x","payTo":"0x2222222222222222222222222222222222222222","maxTimeoutSeconds":60,"asset":"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913","extra":{"name":"USD Coin","version":"2"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	insp, _, _, err := x402pay.Fetch402(context.Background(), "GET", srv.URL, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, insp.X402Version)
	require.Equal(t, "500", insp.Accepts[0].Amount)
	require.Equal(t, "evm", insp.Accepts[0].Family)
	require.Equal(t, "eip3009", insp.Accepts[0].AssetTransferMethod)
}

func TestPayMissingFamily(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	w := &store.Wallet{Version: 1}
	slot, err := wallet.ImportEVMPrivateKey("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = slot
	require.NoError(t, w.Save(""))

	pr := types.PaymentRequired{
		X402Version: 2,
		Accepts: []types.PaymentRequirements{{
			Scheme:  "exact",
			Network: "hedera:testnet",
			Asset:   "0.0.0",
			Amount:  "1",
			PayTo:   "0.0.2",
			Extra:   map[string]interface{}{"feePayer": "0.0.3"},
		}},
	}
	raw, _ := json.Marshal(pr)
	b64 := base64.StdEncoding.EncodeToString(raw)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("PAYMENT-REQUIRED", b64)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	cfg := &config.Config{EVMRPC: map[string]string{}}
	_, err = x402pay.Pay(context.Background(), w, cfg, "GET", srv.URL, nil, nil, x402pay.SelectOpts{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "hedera")
}

func TestPayEIP3009HTTP(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	w := &store.Wallet{Version: 1}
	slot, err := wallet.ImportEVMPrivateKey("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = slot
	require.NoError(t, w.Save(""))

	pr := types.PaymentRequired{
		X402Version: 2,
		Accepts: []types.PaymentRequirements{{
			Scheme:            "exact",
			Network:           "eip155:84532",
			Asset:             "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
			Amount:            "10000",
			PayTo:             "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
			MaxTimeoutSeconds: 60,
			Extra: map[string]interface{}{
				"name":    "USDC",
				"version": "2",
			},
		}},
	}
	raw, _ := json.Marshal(pr)
	b64 := base64.StdEncoding.EncodeToString(raw)

	var sawPayment bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PAYMENT-SIGNATURE") != "" || r.Header.Get("X-PAYMENT") != "" {
			sawPayment = true
			w.Header().Set("PAYMENT-RESPONSE", base64.StdEncoding.EncodeToString([]byte(`{"success":true,"transaction":"0xabc"}`)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("PAYMENT-REQUIRED", b64)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	cfg := &config.Config{EVMRPC: map[string]string{}}
	res, err := x402pay.Pay(context.Background(), w, cfg, "GET", srv.URL, nil, nil, x402pay.SelectOpts{})
	require.NoError(t, err)
	require.True(t, sawPayment)
	require.True(t, res.OK)
	require.Equal(t, "eip3009", res.Selected.AssetTransferMethod)
}

func TestPayPermit2PayloadShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	w := &store.Wallet{Version: 1}
	slot, err := wallet.ImportEVMPrivateKey("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	w.EVM = slot

	built, err := x402pay.BuildClient(w, &config.Config{}, "eip155:84532")
	require.NoError(t, err)
	payload, err := built.Client.CreatePaymentPayload(context.Background(), types.PaymentRequirements{
		Scheme:            "exact",
		Network:           "eip155:84532",
		Asset:             "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
		Amount:            "10000",
		PayTo:             "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		MaxTimeoutSeconds: 60,
		Extra: map[string]interface{}{
			"name":                "USDC",
			"version":             "2",
			"assetTransferMethod": "permit2",
		},
	}, nil, nil)
	require.NoError(t, err)
	require.Contains(t, payload.Payload, "permit2Authorization")
	require.Contains(t, payload.Payload, "signature")
}
