package x402pay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/ethereum/go-ethereum/ethclient"
	x402 "github.com/x402-foundation/x402/go/v2"
	evmmech "github.com/x402-foundation/x402/go/v2/mechanisms/evm"
	batchsettlement "github.com/x402-foundation/x402/go/v2/mechanisms/evm/batch-settlement"
	batchedclient "github.com/x402-foundation/x402/go/v2/mechanisms/evm/batch-settlement/client"
	evmclient "github.com/x402-foundation/x402/go/v2/mechanisms/evm/exact/client"
	evmv1client "github.com/x402-foundation/x402/go/v2/mechanisms/evm/exact/v1/client"
	hederamech "github.com/x402-foundation/x402/go/v2/mechanisms/hedera"
	hederaclient "github.com/x402-foundation/x402/go/v2/mechanisms/hedera/exact/client"
	svmclient "github.com/x402-foundation/x402/go/v2/mechanisms/svm/exact/client"
	svmv1client "github.com/x402-foundation/x402/go/v2/mechanisms/svm/exact/v1/client"
	evmsigner "github.com/x402-foundation/x402/go/v2/signers/evm"
	svmsigner "github.com/x402-foundation/x402/go/v2/signers/svm"
)

// Built holds a registered x402 client plus optional batch-settlement scheme handle.
type Built struct {
	Client *x402.X402Client
	Batch  *batchedclient.BatchSettlementEvmScheme
}

// BuildClient registers exact + batch-settlement (EVM) and other family schemes.
// When network is eip155:N and SAW_RPC_N is set, the EVM signer gets an ethclient
// (batch-settlement channel recovery / contract reads).
func BuildClient(w *store.Wallet, cfg *config.Config, network string) (*Built, error) {
	out := &Built{Client: x402.Newx402Client()}
	if w.HasFamily(store.FamilyEVM) {
		var eth *ethclient.Client
		if chainID, err := ChainIDFromNetwork(network); err == nil && cfg != nil {
			if url, err := cfg.EVMRPCURL(chainID); err == nil {
				eth, err = ethclient.Dial(url)
				if err != nil {
					return nil, fmt.Errorf("dial RPC for chain %d: %w", chainID, err)
				}
			}
		}
		signer, err := evmsigner.NewClientSignerFromPrivateKeyWithClient(w.EVM.PrivateKey, eth)
		if err != nil {
			return nil, err
		}
		out.Client.Register("eip155:*", evmclient.NewExactEvmScheme(signer, nil))

		channelsDir, err := config.ChannelsPath()
		if err != nil {
			return nil, err
		}
		batch := batchedclient.NewBatchSettlementEvmScheme(signer, &batchedclient.BatchSettlementEvmSchemeOptions{
			DepositMultiplier: config.BatchDepositMultiplier(),
			Storage: batchedclient.NewFileClientChannelStorage(batchsettlement.FileChannelStorageOptions{
				Directory: channelsDir,
			}),
		})
		out.Batch = batch
		out.Client.Register("eip155:*", batch)

		v1 := evmv1client.NewExactEvmSchemeV1(signer)
		for _, n := range []string{"base", "base-sepolia", "ethereum", "sepolia", "polygon", "abstract", "abstract-testnet", "peak"} {
			out.Client.RegisterV1(x402.Network(n), v1)
		}
	}
	if w.HasFamily(store.FamilySolana) {
		signer, err := svmsigner.NewClientSignerFromPrivateKey(w.Solana.PrivateKey)
		if err != nil {
			return nil, err
		}
		out.Client.Register("solana:*", svmclient.NewExactSvmScheme(signer))
		out.Client.Register("solana", svmclient.NewExactSvmScheme(signer))
		out.Client.RegisterV1("solana", svmv1client.NewExactSvmSchemeV1(signer))
		out.Client.RegisterV1("solana-devnet", svmv1client.NewExactSvmSchemeV1(signer))
	}
	if w.HasFamily(store.FamilyHedera) {
		net := w.Hedera.Network
		if net == "" && cfg != nil {
			net = cfg.HederaNetwork
		}
		net = hederaCAIP2(net)
		signer, err := hederamech.NewPrivateKeyClientSigner(w.Hedera.AccountID, w.Hedera.PrivateKey, net)
		if err != nil {
			return nil, fmt.Errorf("hedera signer: %w", err)
		}
		scheme := hederaclient.NewExactHederaScheme(signer)
		out.Client.Register(x402.Network(hederamech.HederaTestnetCAIP2), scheme)
		out.Client.Register(x402.Network(hederamech.HederaMainnetCAIP2), scheme)
		out.Client.Register("hedera:*", scheme)
	}
	return out, nil
}

// RefundBatch cooperatively refunds a batch-settlement channel for the resource URL.
func RefundBatch(ctx context.Context, w *store.Wallet, cfg *config.Config, url, amount string) (*x402.SettleResponse, error) {
	if !w.HasFamily(store.FamilyEVM) {
		return nil, fmt.Errorf("batch-settlement refund requires an EVM wallet")
	}
	insp, _, _, err := Fetch402(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	network := ""
	for _, a := range insp.Accepts {
		if strings.EqualFold(a.Scheme, evmmech.SchemeBatched) {
			network = a.Network
			break
		}
	}
	if network == "" && len(insp.Accepts) > 0 {
		network = insp.Accepts[0].Network
	}
	built, err := BuildClient(w, cfg, network)
	if err != nil {
		return nil, err
	}
	if built.Batch == nil {
		return nil, fmt.Errorf("batch-settlement scheme not registered")
	}
	opts := &batchedclient.RefundOptions{}
	if amount != "" {
		opts.Amount = amount
	}
	return built.Batch.Refund(ctx, url, opts)
}

// ChannelSession is a secret-free view of a persisted batch-settlement session.
type ChannelSession struct {
	ChannelID               string `json:"channelId"`
	ChargedCumulativeAmount string `json:"chargedCumulativeAmount,omitempty"`
	Balance                 string `json:"balance,omitempty"`
	TotalClaimed            string `json:"totalClaimed,omitempty"`
	DepositAmount           string `json:"depositAmount,omitempty"`
	SignedMaxClaimable      string `json:"signedMaxClaimable,omitempty"`
}

// ListChannels reads local batch-settlement session files (no signatures).
func ListChannels() ([]ChannelSession, error) {
	root, err := config.ChannelsPath()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "client")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ChannelSession{}, nil
		}
		return nil, err
	}
	var out []ChannelSession
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sess ChannelSession
		if err := json.Unmarshal(raw, &sess); err != nil {
			continue
		}
		sess.ChannelID = id
		out = append(out, sess)
	}
	return out, nil
}
