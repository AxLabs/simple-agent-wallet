package x402pay

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/tx"
	solana "github.com/gagliardetto/solana-go"
	evmmech "github.com/x402-foundation/x402/go/v2/mechanisms/evm"
	evmv1 "github.com/x402-foundation/x402/go/v2/mechanisms/evm/v1"
)

const (
	// SkipBalanceCheckFlag is the cobra flag that bypasses the pay preflight.
	SkipBalanceCheckFlag = "skip-balance-check"

	evmNativeSentinel = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

// InsufficientBalanceError is returned when the payer holds less than the selected amount.
type InsufficientBalanceError struct {
	Network   string
	Asset     string
	Required  *big.Int
	Available *big.Int
	Shortfall *big.Int
}

func (e *InsufficientBalanceError) Error() string {
	return fmt.Sprintf("insufficient balance: network=%s asset=%s required=%s available=%s shortfall=%s",
		e.Network, e.Asset, e.Required, e.Available, e.Shortfall)
}

// BalanceCheckError is returned when the selected-asset balance cannot be queried.
type BalanceCheckError struct {
	Network string
	Asset   string
	Err     error
}

func (e *BalanceCheckError) Error() string {
	return fmt.Sprintf("cannot check selected token balance (network=%s asset=%s): %v; pass --%s to sign anyway",
		e.Network, e.Asset, e.Err, SkipBalanceCheckFlag)
}

func (e *BalanceCheckError) Unwrap() error { return e.Err }

// CheckSelectedBalance queries the payer's selected-asset balance and fails closed
// when the lookup fails or the available amount is below the required base units.
func CheckSelectedBalance(ctx context.Context, w *store.Wallet, cfg *config.Config, selected AcceptView) error {
	required, ok := new(big.Int).SetString(strings.TrimSpace(selected.Amount), 10)
	if !ok {
		return &BalanceCheckError{
			Network: selected.Network,
			Asset:   selected.Asset,
			Err:     fmt.Errorf("invalid amount %q", selected.Amount),
		}
	}
	available, err := QuerySelectedBalance(ctx, w, cfg, selected)
	if err != nil {
		var already *BalanceCheckError
		if errors.As(err, &already) {
			return err
		}
		return &BalanceCheckError{Network: selected.Network, Asset: selected.Asset, Err: err}
	}
	if available.Cmp(required) < 0 {
		shortfall := new(big.Int).Sub(required, available)
		return &InsufficientBalanceError{
			Network:   selected.Network,
			Asset:     selected.Asset,
			Required:  required,
			Available: available,
			Shortfall: shortfall,
		}
	}
	return nil
}

// QuerySelectedBalance returns the payer's balance for the selected network and asset.
func QuerySelectedBalance(ctx context.Context, w *store.Wallet, cfg *config.Config, selected AcceptView) (*big.Int, error) {
	asset := stripAssetPrefix(selected.Asset)
	switch selected.Family {
	case store.FamilyEVM:
		chainID, err := evmChainID(selected.Network)
		if err != nil {
			return nil, err
		}
		if isEVMNativeAsset(asset) {
			bal, err := tx.NativeBalance(ctx, cfg, w, chainID)
			if err != nil {
				return nil, err
			}
			return bal, nil
		}
		return tx.ERC20Balance(ctx, cfg, w, chainID, asset)
	case store.FamilySolana:
		if isSolanaNativeAsset(asset) {
			lamports, err := tx.SolanaBalance(ctx, cfg, w)
			if err != nil {
				return nil, err
			}
			return new(big.Int).SetUint64(lamports), nil
		}
		return tx.SolanaTokenBalance(ctx, cfg, w, asset)
	case store.FamilyHedera:
		return tx.HederaBalance(ctx, cfg, w, selected.Network, asset)
	default:
		return nil, fmt.Errorf("unsupported family %q", selected.Family)
	}
}

func evmChainID(network string) (int64, error) {
	if id, err := ChainIDFromNetwork(network); err == nil {
		return id, nil
	}
	if nc, ok := evmmech.NetworkConfigs[network]; ok && nc.ChainID != nil {
		return nc.ChainID.Int64(), nil
	}
	if id, ok := evmv1.NetworkChainIDs[network]; ok && id != nil {
		return id.Int64(), nil
	}
	return 0, fmt.Errorf("not an eip155 network: %s", network)
}

func isEVMNativeAsset(asset string) bool {
	a := strings.TrimSpace(asset)
	switch strings.ToLower(a) {
	case "", "native", "eth":
		return true
	}
	if strings.EqualFold(a, evmNativeSentinel) {
		return true
	}
	hex := strings.TrimPrefix(strings.ToLower(a), "0x")
	return len(hex) == 40 && strings.Trim(hex, "0") == ""
}

func isSolanaNativeAsset(asset string) bool {
	a := strings.TrimSpace(asset)
	switch strings.ToLower(a) {
	case "", "native", "sol":
		return true
	}
	pk, err := solana.PublicKeyFromBase58(a)
	if err != nil {
		return false
	}
	return pk.Equals(solana.SystemProgramID)
}
