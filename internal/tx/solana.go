package tx

import (
	"context"
	"fmt"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	solana "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/programs/system"
)

func SolanaClient(cfg *config.Config) (*rpc.Client, error) {
	if cfg.SolanaRPC == "" {
		return nil, fmt.Errorf("set SAW_SOLANA_RPC in config.env")
	}
	return rpc.New(cfg.SolanaRPC), nil
}

func TransferSOL(ctx context.Context, cfg *config.Config, w *store.Wallet, to string, lamports uint64) (string, error) {
	if w.Solana == nil || w.Solana.PrivateKey == "" {
		return "", store.ErrFamilyMissing
	}
	client, err := SolanaClient(cfg)
	if err != nil {
		return "", err
	}
	pk, err := solana.PrivateKeyFromBase58(w.Solana.PrivateKey)
	if err != nil {
		return "", err
	}
	toPub, err := solana.PublicKeyFromBase58(to)
	if err != nil {
		return "", err
	}
	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", err
	}
	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			system.NewTransferInstruction(lamports, pk.PublicKey(), toPub).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(pk.PublicKey()),
	)
	if err != nil {
		return "", err
	}
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(pk.PublicKey()) {
			return &pk
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sig, err := client.SendTransaction(ctx, tx)
	if err != nil {
		return "", err
	}
	return sig.String(), nil
}

func SolanaBalance(ctx context.Context, cfg *config.Config, w *store.Wallet) (uint64, error) {
	if w.Solana == nil {
		return 0, store.ErrFamilyMissing
	}
	client, err := SolanaClient(cfg)
	if err != nil {
		return 0, err
	}
	pub, err := solana.PublicKeyFromBase58(w.Solana.Address)
	if err != nil {
		return 0, err
	}
	out, err := client.GetBalance(ctx, pub, rpc.CommitmentFinalized)
	if err != nil {
		return 0, err
	}
	return out.Value, nil
}
