package tx_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/tx"
	"github.com/stretchr/testify/require"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
)

func TestHederaBalanceHBARAndHTS(t *testing.T) {
	w := &store.Wallet{Version: 1, Hedera: &store.HederaSlot{AccountID: "0.0.9001"}}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/tokens"):
			require.Equal(t, "0.0.429274", r.URL.Query().Get("token.id"))
			_ = json.NewEncoder(rw).Encode(map[string]any{
				"tokens": []map[string]any{{"token_id": "0.0.429274", "balance": 77}},
			})
		default:
			_ = json.NewEncoder(rw).Encode(map[string]any{
				"balance": map[string]any{"balance": 1234},
			})
		}
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{HederaMirror: srv.URL}

	hbar, err := tx.HederaBalance(context.Background(), cfg, w, hederamech.HederaTestnetCAIP2, hederamech.HBARAssetID)
	require.NoError(t, err)
	require.Equal(t, "1234", hbar.String())

	hts, err := tx.HederaBalance(context.Background(), cfg, w, hederamech.HederaTestnetCAIP2, "0.0.429274")
	require.NoError(t, err)
	require.Equal(t, "77", hts.String())
}

func TestHederaBalanceLookupFailure(t *testing.T) {
	w := &store.Wallet{Version: 1, Hedera: &store.HederaSlot{AccountID: "0.0.9001"}}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Error(rw, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	_, err := tx.HederaBalance(context.Background(), &config.Config{HederaMirror: srv.URL}, w, hederamech.HederaTestnetCAIP2, hederamech.HBARAssetID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
}

func TestHederaBalanceMissingFamily(t *testing.T) {
	_, err := tx.HederaBalance(context.Background(), &config.Config{}, &store.Wallet{Version: 1}, hederamech.HederaTestnetCAIP2, hederamech.HBARAssetID)
	require.ErrorIs(t, err, store.ErrFamilyMissing)
}

func TestHederaBalanceUnassociatedIsZero(t *testing.T) {
	w := &store.Wallet{Version: 1, Hedera: &store.HederaSlot{AccountID: "0.0.9001"}}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write([]byte(`{"tokens":[]}`))
	}))
	t.Cleanup(srv.Close)
	got, err := tx.HederaBalance(context.Background(), &config.Config{HederaMirror: srv.URL}, w, "testnet", "0.0.1")
	require.NoError(t, err)
	require.Equal(t, big.NewInt(0), got)
}
