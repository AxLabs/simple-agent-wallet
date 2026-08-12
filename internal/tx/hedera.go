package tx

import (
	"fmt"
	"math/big"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
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
