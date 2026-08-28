package x402pay_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/wallet"
	x402pay "github.com/AxLabs/simple-agent-wallet/internal/x402"
	"github.com/ethereum/go-ethereum/common"
	solana "github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
	"github.com/x402-foundation/x402/go/v2/types"
)

const (
	testEVMKey    = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	testERC20     = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
	testHederaKey = "302e020100300506032b657004220420a869f4c6191b9c8c99933e7f6b6611711737e4b1a1a5a4cb5370e719a1f6df98"
	testHTS       = "0.0.429274"
)

func TestCheckSelectedBalanceComparison(t *testing.T) {
	cases := []struct {
		name      string
		available string
		required  string
		wantErr   bool
		shortfall string
	}{
		{name: "insufficient", available: "99", required: "100", wantErr: true, shortfall: "1"},
		{name: "exact", available: "100", required: "100"},
		{name: "sufficient", available: "101", required: "100"},
	}
	for _, tc := range cases {
		t.Run("evm/"+tc.name, func(t *testing.T) {
			w := evmWallet(t)
			cfg := evmCfg(t, encodeUint256(mustBig(tc.available)), "")
			err := x402pay.CheckSelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
				Family:  store.FamilyEVM,
				Network: "eip155:84532",
				Asset:   testERC20,
				Amount:  tc.required,
			})
			assertBalanceCompare(t, err, tc.wantErr, "eip155:84532", testERC20, tc.required, tc.available, tc.shortfall)
		})
		t.Run("solana/"+tc.name, func(t *testing.T) {
			w := solWallet(t)
			mint := solana.NewWallet().PublicKey()
			cfg := solTokenCfg(t, mint, mustBig(tc.available), false)
			err := x402pay.CheckSelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
				Family:  store.FamilySolana,
				Network: "solana:mainnet",
				Asset:   mint.String(),
				Amount:  tc.required,
			})
			assertBalanceCompare(t, err, tc.wantErr, "solana:mainnet", mint.String(), tc.required, tc.available, tc.shortfall)
		})
		t.Run("hedera/"+tc.name, func(t *testing.T) {
			w := hederaWallet(t)
			cfg := hederaCfg(t, w.Hedera.AccountID, testHTS, mustBig(tc.available), http.StatusOK)
			err := x402pay.CheckSelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
				Family:  store.FamilyHedera,
				Network: hederamech.HederaTestnetCAIP2,
				Asset:   testHTS,
				Amount:  tc.required,
			})
			assertBalanceCompare(t, err, tc.wantErr, hederamech.HederaTestnetCAIP2, testHTS, tc.required, tc.available, tc.shortfall)
		})
	}
}

func TestCheckSelectedBalanceLookupFailure(t *testing.T) {
	t.Run("evm", func(t *testing.T) {
		w := evmWallet(t)
		err := x402pay.CheckSelectedBalance(context.Background(), w, &config.Config{EVMRPC: map[string]string{}}, x402pay.AcceptView{
			Family:  store.FamilyEVM,
			Network: "eip155:84532",
			Asset:   testERC20,
			Amount:  "100",
		})
		assertLookupFailure(t, err, "eip155:84532", testERC20)
	})
	t.Run("solana", func(t *testing.T) {
		w := solWallet(t)
		mint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
		err := x402pay.CheckSelectedBalance(context.Background(), w, &config.Config{}, x402pay.AcceptView{
			Family:  store.FamilySolana,
			Network: "solana:mainnet",
			Asset:   mint,
			Amount:  "100",
		})
		assertLookupFailure(t, err, "solana:mainnet", mint)
	})
	t.Run("hedera", func(t *testing.T) {
		w := hederaWallet(t)
		cfg := hederaCfg(t, w.Hedera.AccountID, testHTS, big.NewInt(0), http.StatusInternalServerError)
		err := x402pay.CheckSelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilyHedera,
			Network: hederamech.HederaTestnetCAIP2,
			Asset:   testHTS,
			Amount:  "100",
		})
		assertLookupFailure(t, err, hederamech.HederaTestnetCAIP2, testHTS)
	})
	t.Run("invalid amount", func(t *testing.T) {
		w := evmWallet(t)
		err := x402pay.CheckSelectedBalance(context.Background(), w, &config.Config{EVMRPC: map[string]string{}}, x402pay.AcceptView{
			Family:  store.FamilyEVM,
			Network: "eip155:84532",
			Asset:   testERC20,
			Amount:  "not-a-number",
		})
		assertLookupFailure(t, err, "eip155:84532", testERC20)
	})
}

func TestQuerySelectedBalanceNativeVsToken(t *testing.T) {
	t.Run("evm native", func(t *testing.T) {
		w := evmWallet(t)
		cfg := evmCfg(t, "", encodeHexBig(big.NewInt(42)))
		got, err := x402pay.QuerySelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilyEVM,
			Network: "eip155:84532",
			Asset:   "0x0000000000000000000000000000000000000000",
		})
		require.NoError(t, err)
		require.Equal(t, "42", got.String())
	})
	t.Run("evm erc20 prefix", func(t *testing.T) {
		w := evmWallet(t)
		cfg := evmCfg(t, encodeUint256(big.NewInt(7)), "")
		got, err := x402pay.QuerySelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilyEVM,
			Network: "base-sepolia",
			Asset:   "erc20:" + testERC20,
		})
		require.NoError(t, err)
		require.Equal(t, "7", got.String())
	})
	t.Run("solana native", func(t *testing.T) {
		w := solWallet(t)
		cfg := solNativeCfg(t, 99)
		got, err := x402pay.QuerySelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilySolana,
			Network: "solana:mainnet",
			Asset:   solana.SystemProgramID.String(),
		})
		require.NoError(t, err)
		require.Equal(t, "99", got.String())
	})
	t.Run("solana missing token account is zero", func(t *testing.T) {
		w := solWallet(t)
		mint := solana.NewWallet().PublicKey()
		cfg := solTokenCfg(t, mint, big.NewInt(0), true)
		got, err := x402pay.QuerySelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilySolana,
			Network: "solana:mainnet",
			Asset:   mint.String(),
		})
		require.NoError(t, err)
		require.Equal(t, "0", got.String())
	})
	t.Run("hedera hbar", func(t *testing.T) {
		w := hederaWallet(t)
		cfg := hederaCfg(t, w.Hedera.AccountID, hederamech.HBARAssetID, big.NewInt(50), http.StatusOK)
		got, err := x402pay.QuerySelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilyHedera,
			Network: hederamech.HederaTestnetCAIP2,
			Asset:   hederamech.HBARAssetID,
		})
		require.NoError(t, err)
		require.Equal(t, "50", got.String())
	})
	t.Run("hedera unassociated token is zero", func(t *testing.T) {
		w := hederaWallet(t)
		cfg := hederaCfg(t, w.Hedera.AccountID, testHTS, nil, http.StatusOK)
		got, err := x402pay.QuerySelectedBalance(context.Background(), w, cfg, x402pay.AcceptView{
			Family:  store.FamilyHedera,
			Network: hederamech.HederaTestnetCAIP2,
			Asset:   testHTS,
		})
		require.NoError(t, err)
		require.Equal(t, "0", got.String())
	})
}

func TestPayBalancePreflight(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	type family struct {
		name    string
		accept  types.PaymentRequirements
		wallet  func(t *testing.T) *store.Wallet
		cfg     func(t *testing.T, w *store.Wallet, accept *types.PaymentRequirements, available *big.Int, failLookup bool) *config.Config
		canSign bool
	}
	families := []family{
		{
			name:    "evm",
			canSign: true,
			accept: types.PaymentRequirements{
				Scheme:            "exact",
				Network:           "eip155:84532",
				Asset:             testERC20,
				Amount:            "10000",
				PayTo:             "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
				MaxTimeoutSeconds: 60,
				Extra:             map[string]interface{}{"name": "USDC", "version": "2"},
			},
			wallet: evmWallet,
			cfg: func(t *testing.T, _ *store.Wallet, _ *types.PaymentRequirements, available *big.Int, failLookup bool) *config.Config {
				t.Helper()
				if failLookup {
					return &config.Config{EVMRPC: map[string]string{}}
				}
				return evmCfg(t, encodeUint256(available), "")
			},
		},
		{
			name:    "solana",
			canSign: false,
			accept: types.PaymentRequirements{
				Scheme:            "exact",
				Network:           "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
				Amount:            "10000",
				PayTo:             "SoL1111111111111111111111111111111111111112",
				MaxTimeoutSeconds: 60,
				Extra:             map[string]interface{}{"feePayer": "FeePayer111111111111111111111111111111111"},
			},
			wallet: solWallet,
			cfg: func(t *testing.T, _ *store.Wallet, accept *types.PaymentRequirements, available *big.Int, failLookup bool) *config.Config {
				t.Helper()
				mint := solana.NewWallet().PublicKey()
				accept.Asset = mint.String()
				if failLookup {
					return &config.Config{}
				}
				return solTokenCfg(t, mint, available, false)
			},
		},
		{
			name:    "hedera",
			canSign: true,
			accept: types.PaymentRequirements{
				Scheme:            "exact",
				Network:           hederamech.HederaTestnetCAIP2,
				Asset:             testHTS,
				Amount:            "10000",
				PayTo:             "0.0.7001",
				MaxTimeoutSeconds: 180,
				Extra:             map[string]interface{}{"feePayer": "0.0.5001"},
			},
			wallet: hederaWallet,
			cfg: func(t *testing.T, w *store.Wallet, _ *types.PaymentRequirements, available *big.Int, failLookup bool) *config.Config {
				t.Helper()
				status := http.StatusOK
				if failLookup {
					status = http.StatusInternalServerError
				}
				return hederaCfg(t, w.Hedera.AccountID, testHTS, available, status)
			},
		},
	}

	for _, fam := range families {
		t.Run(fam.name+"/insufficient", func(t *testing.T) {
			w := fam.wallet(t)
			accept := fam.accept
			cfg := fam.cfg(t, w, &accept, big.NewInt(1), false)
			signed := 0
			url := payServer(t, accept, &signed)
			_, err := x402pay.Pay(context.Background(), w, cfg, "GET", url, nil, nil, x402pay.SelectOpts{})
			require.Error(t, err)
			var insuff *x402pay.InsufficientBalanceError
			require.ErrorAs(t, err, &insuff)
			require.Equal(t, 0, signed)
		})
		t.Run(fam.name+"/exact", func(t *testing.T) {
			if !fam.canSign {
				w := fam.wallet(t)
				accept := fam.accept
				cfg := fam.cfg(t, w, &accept, big.NewInt(10000), false)
				err := x402pay.CheckSelectedBalance(context.Background(), w, cfg, x402pay.ViewFromV2(0, accept))
				require.NoError(t, err)
				return
			}
			w := fam.wallet(t)
			accept := fam.accept
			cfg := fam.cfg(t, w, &accept, big.NewInt(10000), false)
			signed := 0
			url := payServer(t, accept, &signed)
			res, err := x402pay.Pay(context.Background(), w, cfg, "GET", url, nil, nil, x402pay.SelectOpts{})
			require.NoError(t, err)
			require.Equal(t, 1, signed)
			require.True(t, res.OK)
		})
		t.Run(fam.name+"/sufficient", func(t *testing.T) {
			if !fam.canSign {
				w := fam.wallet(t)
				accept := fam.accept
				cfg := fam.cfg(t, w, &accept, big.NewInt(50000), false)
				err := x402pay.CheckSelectedBalance(context.Background(), w, cfg, x402pay.ViewFromV2(0, accept))
				require.NoError(t, err)
				return
			}
			w := fam.wallet(t)
			accept := fam.accept
			cfg := fam.cfg(t, w, &accept, big.NewInt(50000), false)
			signed := 0
			url := payServer(t, accept, &signed)
			res, err := x402pay.Pay(context.Background(), w, cfg, "GET", url, nil, nil, x402pay.SelectOpts{})
			require.NoError(t, err)
			require.Equal(t, 1, signed)
			require.True(t, res.OK)
		})
		t.Run(fam.name+"/lookup-failure", func(t *testing.T) {
			w := fam.wallet(t)
			accept := fam.accept
			cfg := fam.cfg(t, w, &accept, nil, true)
			signed := 0
			url := payServer(t, accept, &signed)
			_, err := x402pay.Pay(context.Background(), w, cfg, "GET", url, nil, nil, x402pay.SelectOpts{})
			require.Error(t, err)
			assertLookupFailure(t, err, accept.Network, accept.Asset)
			require.Equal(t, 0, signed)
		})
		t.Run(fam.name+"/opt-out", func(t *testing.T) {
			w := fam.wallet(t)
			accept := fam.accept
			cfg := fam.cfg(t, w, &accept, nil, true)
			signed := 0
			url := payServer(t, accept, &signed)
			if !fam.canSign {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_, err := x402pay.Pay(ctx, w, cfg, "GET", url, nil, nil, x402pay.SelectOpts{SkipBalanceCheck: true})
				require.Error(t, err)
				var checkErr *x402pay.BalanceCheckError
				var insuff *x402pay.InsufficientBalanceError
				require.False(t, errors.As(err, &checkErr), "opt-out should not report a balance-check error: %v", err)
				require.False(t, errors.As(err, &insuff), "opt-out should not report insufficient balance: %v", err)
				require.Equal(t, 0, signed)
				return
			}
			res, err := x402pay.Pay(context.Background(), w, cfg, "GET", url, nil, nil, x402pay.SelectOpts{SkipBalanceCheck: true})
			require.NoError(t, err)
			require.Equal(t, 1, signed)
			require.True(t, res.OK)
		})
	}
}

func assertBalanceCompare(t *testing.T, err error, wantErr bool, network, asset, required, available, shortfall string) {
	t.Helper()
	if !wantErr {
		require.NoError(t, err)
		return
	}
	var insuff *x402pay.InsufficientBalanceError
	require.ErrorAs(t, err, &insuff)
	require.Equal(t, network, insuff.Network)
	require.Equal(t, asset, insuff.Asset)
	require.Equal(t, required, insuff.Required.String())
	require.Equal(t, available, insuff.Available.String())
	require.Equal(t, shortfall, insuff.Shortfall.String())
	require.Contains(t, insuff.Error(), "network="+network)
	require.Contains(t, insuff.Error(), "shortfall="+shortfall)
}

func assertLookupFailure(t *testing.T, err error, network, asset string) {
	t.Helper()
	require.Error(t, err)
	var checkErr *x402pay.BalanceCheckError
	require.ErrorAs(t, err, &checkErr)
	require.Equal(t, network, checkErr.Network)
	require.Equal(t, asset, checkErr.Asset)
	require.Contains(t, err.Error(), "--"+x402pay.SkipBalanceCheckFlag)
	var insuff *x402pay.InsufficientBalanceError
	require.False(t, errors.As(err, &insuff))
}

func evmWallet(t *testing.T) *store.Wallet {
	t.Helper()
	slot, err := wallet.ImportEVMPrivateKey(testEVMKey)
	require.NoError(t, err)
	return &store.Wallet{Version: 1, EVM: slot}
}

func solWallet(t *testing.T) *store.Wallet {
	t.Helper()
	slot, err := wallet.CreateSolana()
	require.NoError(t, err)
	return &store.Wallet{Version: 1, Solana: slot}
}

func hederaWallet(t *testing.T) *store.Wallet {
	t.Helper()
	return &store.Wallet{Version: 1, Hedera: &store.HederaSlot{
		AccountID:  "0.0.9001",
		PrivateKey: testHederaKey,
		Network:    hederamech.HederaTestnetCAIP2,
	}}
}

func payServer(t *testing.T, accept types.PaymentRequirements, signed *int) string {
	t.Helper()
	pr := types.PaymentRequired{X402Version: 2, Accepts: []types.PaymentRequirements{accept}}
	raw, err := json.Marshal(pr)
	require.NoError(t, err)
	b64 := base64.StdEncoding.EncodeToString(raw)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PAYMENT-SIGNATURE") != "" || r.Header.Get("X-PAYMENT") != "" {
			*signed++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("PAYMENT-REQUIRED", b64)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func evmCfg(t *testing.T, callResult, balanceResult string) *config.Config {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.Method {
		case "eth_call":
			if callResult == "" {
				http.Error(w, "unexpected eth_call", http.StatusInternalServerError)
				return
			}
			writeRPC(w, req.ID, callResult)
		case "eth_getBalance":
			if balanceResult == "" {
				http.Error(w, "unexpected eth_getBalance", http.StatusInternalServerError)
				return
			}
			writeRPC(w, req.ID, balanceResult)
		default:
			writeRPC(w, req.ID, "0x1")
		}
	}))
	t.Cleanup(srv.Close)
	return &config.Config{EVMRPC: map[string]string{"84532": srv.URL}}
}

func solNativeCfg(t *testing.T, lamports uint64) *config.Config {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "getBalance", req.Method)
		writeRPC(w, req.ID, map[string]any{
			"context": map[string]any{"slot": 1},
			"value":   lamports,
		})
	}))
	t.Cleanup(srv.Close)
	return &config.Config{SolanaRPC: srv.URL}
}

func solTokenCfg(t *testing.T, mint solana.PublicKey, amount *big.Int, emptyAccounts bool) *config.Config {
	t.Helper()
	tokenAcc := solana.NewWallet().PublicKey()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		switch req.Method {
		case "getTokenAccountsByOwner":
			value := []any{}
			if !emptyAccounts {
				value = []any{map[string]any{
					"pubkey": tokenAcc.String(),
					"account": map[string]any{
						"lamports":   0,
						"owner":      "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
						"executable": false,
						"rentEpoch":  0,
						"data":       []any{"", "base64"},
					},
				}}
			}
			writeRPC(w, req.ID, map[string]any{
				"context": map[string]any{"slot": 1},
				"value":   value,
			})
		case "getTokenAccountBalance":
			writeRPC(w, req.ID, map[string]any{
				"context": map[string]any{"slot": 1},
				"value": map[string]any{
					"amount":         amount.String(),
					"decimals":       6,
					"uiAmount":       0,
					"uiAmountString": amount.String(),
				},
			})
		default:
			http.Error(w, "unexpected "+req.Method, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	_ = mint
	return &config.Config{SolanaRPC: srv.URL}
}

func hederaCfg(t *testing.T, accountID, asset string, amount *big.Int, status int) *config.Config {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("mirror down"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/tokens") {
			if amount == nil {
				_, _ = w.Write([]byte(`{"tokens":[]}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tokens": []map[string]any{{"token_id": asset, "balance": amount.Int64()}},
			})
			return
		}
		bal := int64(0)
		if amount != nil {
			bal = amount.Int64()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance": map[string]any{"balance": bal},
			"account": accountID,
		})
	}))
	t.Cleanup(srv.Close)
	return &config.Config{HederaMirror: srv.URL, HederaNetwork: "testnet"}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	resp := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result}
	_ = json.NewEncoder(w).Encode(resp)
}

func encodeUint256(n *big.Int) string {
	return "0x" + hex.EncodeToString(common.LeftPadBytes(n.Bytes(), 32))
}

func encodeHexBig(n *big.Int) string {
	if n.Sign() == 0 {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

func mustBig(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return n
}
