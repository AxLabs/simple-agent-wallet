package tx

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	evmmech "github.com/x402-foundation/x402/go/v2/mechanisms/evm"
)

var erc20ABI = mustABI(`[
  {"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},
  {"constant":false,"inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"type":"function"},
  {"constant":false,"inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"type":"function"},
  {"constant":true,"inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"type":"function"}
]`)

func mustABI(s string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(s))
	if err != nil {
		panic(err)
	}
	return a
}

func DialEVM(cfg *config.Config, chainID int64) (*ethclient.Client, error) {
	url, err := cfg.EVMRPCURL(chainID)
	if err != nil {
		return nil, err
	}
	return ethclient.Dial(url)
}

func privateKey(w *store.Wallet) (*ecdsa.PrivateKey, common.Address, error) {
	if w.EVM == nil || w.EVM.PrivateKey == "" {
		return nil, common.Address{}, store.ErrFamilyMissing
	}
	pk, err := crypto.HexToECDSA(strings.TrimPrefix(w.EVM.PrivateKey, "0x"))
	if err != nil {
		return nil, common.Address{}, err
	}
	return pk, crypto.PubkeyToAddress(pk.PublicKey), nil
}

func sendTx(ctx context.Context, client *ethclient.Client, pk *ecdsa.PrivateKey, from common.Address, to *common.Address, value *big.Int, data []byte) (common.Hash, error) {
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Hash{}, err
	}
	chainID, err := client.NetworkID(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	gasTip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return common.Hash{}, err
	}
	gasFeeCap := new(big.Int).Add(header.BaseFee, new(big.Int).Mul(gasTip, big.NewInt(2)))
	call := ethereum.CallMsg{From: from, To: to, Value: value, Data: data}
	gas, err := client.EstimateGas(ctx, call)
	if err != nil {
		gas = 200000
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTip,
		GasFeeCap: gasFeeCap,
		Gas:       gas,
		To:        to,
		Value:     value,
		Data:      data,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), pk)
	if err != nil {
		return common.Hash{}, err
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return common.Hash{}, err
	}
	return signed.Hash(), nil
}

func TransferNative(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64, to string, amountWei *big.Int) (string, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	pk, from, err := privateKey(w)
	if err != nil {
		return "", err
	}
	addr := common.HexToAddress(to)
	h, err := sendTx(ctx, client, pk, from, &addr, amountWei, nil)
	return h.Hex(), err
}

func TransferERC20(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64, token, to string, amount *big.Int) (string, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	pk, from, err := privateKey(w)
	if err != nil {
		return "", err
	}
	data, err := erc20ABI.Pack("transfer", common.HexToAddress(to), amount)
	if err != nil {
		return "", err
	}
	tok := common.HexToAddress(token)
	h, err := sendTx(ctx, client, pk, from, &tok, big.NewInt(0), data)
	return h.Hex(), err
}

func ApprovePermit2(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64, token string) (string, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	pk, from, err := privateKey(w)
	if err != nil {
		return "", err
	}
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	data, err := erc20ABI.Pack("approve", common.HexToAddress(evmmech.PERMIT2Address), max)
	if err != nil {
		return "", err
	}
	tok := common.HexToAddress(token)
	h, err := sendTx(ctx, client, pk, from, &tok, big.NewInt(0), data)
	return h.Hex(), err
}

func AllowancePermit2(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64, token string) (*big.Int, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	_, from, err := privateKey(w)
	if err != nil {
		return nil, err
	}
	data, err := erc20ABI.Pack("allowance", from, common.HexToAddress(evmmech.PERMIT2Address))
	if err != nil {
		return nil, err
	}
	tok := common.HexToAddress(token)
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &tok, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	vals, err := erc20ABI.Unpack("allowance", out)
	if err != nil {
		return nil, err
	}
	return vals[0].(*big.Int), nil
}

func Call(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64, to string, valueWei *big.Int, data []byte) (string, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	pk, from, err := privateKey(w)
	if err != nil {
		return "", err
	}
	addr := common.HexToAddress(to)
	if valueWei == nil {
		valueWei = big.NewInt(0)
	}
	h, err := sendTx(ctx, client, pk, from, &addr, valueWei, data)
	return h.Hex(), err
}

func NativeBalance(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64) (*big.Int, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	_, from, err := privateKey(w)
	if err != nil {
		return nil, err
	}
	return client.BalanceAt(ctx, from, nil)
}

func ERC20Balance(ctx context.Context, cfg *config.Config, w *store.Wallet, chainID int64, token string) (*big.Int, error) {
	client, err := DialEVM(cfg, chainID)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	_, from, err := privateKey(w)
	if err != nil {
		return nil, err
	}
	data, err := erc20ABI.Pack("balanceOf", from)
	if err != nil {
		return nil, err
	}
	tok := common.HexToAddress(token)
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &tok, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	vals, err := erc20ABI.Unpack("balanceOf", out)
	if err != nil {
		return nil, err
	}
	return vals[0].(*big.Int), nil
}

// EncodeABI encodes function call data from signature + args.
// signature example: "transfer(address,uint256)"
func EncodeABI(signature string, args []string) ([]byte, error) {
	signature = strings.TrimSpace(signature)
	_, typesPart, ok := strings.Cut(signature, "(")
	if !ok || !strings.HasSuffix(typesPart, ")") {
		return nil, fmt.Errorf("signature must look like name(type,...)")
	}
	typesPart = strings.TrimSuffix(typesPart, ")")
	var argTypes []string
	if typesPart != "" {
		argTypes = splitTypes(typesPart)
	}
	if len(args) != len(argTypes) {
		return nil, fmt.Errorf("got %d args, signature expects %d", len(args), len(argTypes))
	}
	arguments := make(abi.Arguments, 0, len(argTypes))
	vals := make([]interface{}, 0, len(argTypes))
	for i, t := range argTypes {
		ty, err := abi.NewType(t, "", nil)
		if err != nil {
			return nil, fmt.Errorf("type %s: %w", t, err)
		}
		arguments = append(arguments, abi.Argument{Type: ty})
		v, err := parseArg(t, args[i])
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	packed, err := arguments.Pack(vals...)
	if err != nil {
		return nil, err
	}
	sig := crypto.Keccak256([]byte(signature))[:4]
	return append(sig, packed...), nil
}

func splitTypes(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

func parseArg(typ, raw string) (interface{}, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case typ == "address":
		return common.HexToAddress(raw), nil
	case strings.HasPrefix(typ, "uint") || strings.HasPrefix(typ, "int"):
		n, ok := new(big.Int).SetString(raw, 0)
		if !ok {
			return nil, fmt.Errorf("invalid int: %s", raw)
		}
		return n, nil
	case typ == "bool":
		return raw == "true" || raw == "1", nil
	case typ == "bytes" || strings.HasPrefix(typ, "bytes"):
		b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
		return b, err
	case typ == "string":
		return raw, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", typ)
	}
}

func ParseAmount(s string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(strings.TrimSpace(s), 0)
	if !ok {
		return nil, fmt.Errorf("invalid amount %q", s)
	}
	return n, nil
}
