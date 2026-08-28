package tx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
)

func hederaClient(network string) *hiero.Client {
	switch network {
	case "mainnet", "hedera:mainnet":
		return hiero.ClientForMainnet()
	default:
		return hiero.ClientForTestnet()
	}
}

func TransferHBAR(cfg *config.Config, w *store.Wallet, to string, tinybars int64) (string, error) {
	if w.Hedera == nil {
		return "", store.ErrFamilyMissing
	}
	client := hederaClient(w.Hedera.Network)
	defer client.Close()
	key, err := hederamech.ParsePrivateKey(w.Hedera.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("hedera private key: %w", err)
	}
	from, err := hiero.AccountIDFromString(w.Hedera.AccountID)
	if err != nil {
		return "", err
	}
	client.SetOperator(from, key)
	toID, err := hiero.AccountIDFromString(to)
	if err != nil {
		return "", err
	}
	tx, err := hiero.NewTransferTransaction().
		AddHbarTransfer(from, hiero.HbarFromTinybar(-tinybars)).
		AddHbarTransfer(toID, hiero.HbarFromTinybar(tinybars)).
		Execute(client)
	if err != nil {
		return "", err
	}
	receipt, err := tx.GetReceipt(client)
	if err != nil {
		return "", err
	}
	if receipt.Status != hiero.StatusSuccess {
		return "", fmt.Errorf("hedera transfer status: %v", receipt.Status)
	}
	return tx.TransactionID.String(), nil
}

func ParseTinybars(s string) (int64, error) {
	n, ok := new(big.Int).SetString(s, 0)
	if !ok {
		return 0, fmt.Errorf("invalid amount")
	}
	if !n.IsInt64() {
		return 0, fmt.Errorf("amount out of int64 range")
	}
	return n.Int64(), nil
}

type hederaMirrorAccount struct {
	Balance struct {
		Balance int64 `json:"balance"`
	} `json:"balance"`
}

type hederaMirrorTokensResponse struct {
	Tokens []struct {
		TokenID string `json:"token_id"`
		Balance int64  `json:"balance"`
	} `json:"tokens"`
}

// HederaBalance returns HBAR (tinybars) or an HTS token balance from the Mirror Node.
// An unassociated HTS token is a zero balance, not a lookup failure.
func HederaBalance(ctx context.Context, cfg *config.Config, w *store.Wallet, network, asset string) (*big.Int, error) {
	if w.Hedera == nil || w.Hedera.AccountID == "" {
		return nil, store.ErrFamilyMissing
	}
	base, err := hederaMirrorBase(cfg, network)
	if err != nil {
		return nil, err
	}
	account := w.Hedera.AccountID
	asset = strings.TrimSpace(asset)
	if asset == "" || hederamech.IsHbarAsset(asset) {
		return hederaHBARBalance(ctx, base, account)
	}
	return hederaTokenBalance(ctx, base, account, asset)
}

func hederaMirrorBase(cfg *config.Config, network string) (string, error) {
	if cfg != nil && strings.TrimSpace(cfg.HederaMirror) != "" {
		return strings.TrimRight(cfg.HederaMirror, "/"), nil
	}
	caip := hederaNetworkCAIP2(network)
	nc, err := hederamech.GetNetworkConfig(caip)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(nc.MirrorURL, "/"), nil
}

func hederaNetworkCAIP2(net string) string {
	switch strings.ToLower(strings.TrimSpace(net)) {
	case "", "testnet", hederamech.HederaTestnetCAIP2:
		return hederamech.HederaTestnetCAIP2
	case "mainnet", hederamech.HederaMainnetCAIP2:
		return hederamech.HederaMainnetCAIP2
	default:
		if strings.HasPrefix(net, "hedera:") {
			return net
		}
		return hederamech.HederaTestnetCAIP2
	}
}

func hederaHBARBalance(ctx context.Context, mirrorBase, accountID string) (*big.Int, error) {
	var account hederaMirrorAccount
	url := fmt.Sprintf("%s/api/v1/accounts/%s", mirrorBase, accountID)
	if err := hederaGetJSON(ctx, url, &account); err != nil {
		return nil, err
	}
	return big.NewInt(account.Balance.Balance), nil
}

func hederaTokenBalance(ctx context.Context, mirrorBase, accountID, tokenID string) (*big.Int, error) {
	var tokens hederaMirrorTokensResponse
	url := fmt.Sprintf("%s/api/v1/accounts/%s/tokens?token.id=%s", mirrorBase, accountID, tokenID)
	if err := hederaGetJSON(ctx, url, &tokens); err != nil {
		return nil, err
	}
	if len(tokens.Tokens) == 0 {
		return big.NewInt(0), nil
	}
	return big.NewInt(tokens.Tokens[0].Balance), nil
}

func hederaGetJSON(ctx context.Context, url string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 256 {
			msg = msg[:256]
		}
		return fmt.Errorf("hedera mirror request failed with status %d: %s", resp.StatusCode, msg)
	}
	if dest != nil && len(body) > 0 {
		return json.Unmarshal(body, dest)
	}
	return nil
}
