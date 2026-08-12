package wallet

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/crypto"
	solana "github.com/gagliardetto/solana-go"
	"github.com/tyler-smith/go-bip39"
)

// CreateEVM generates a new BIP39 mnemonic and derives account index.
func CreateEVM(index uint32) (*store.EVMSlot, string, error) {
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return nil, "", err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, "", err
	}
	slot, err := ImportEVMMnemonic(mnemonic, index)
	if err != nil {
		return nil, "", err
	}
	return slot, mnemonic, nil
}

func ImportEVMPrivateKey(hexKey string) (*store.EVMSlot, error) {
	hexKey = strings.TrimPrefix(strings.TrimSpace(hexKey), "0x")
	pk, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	addr := crypto.PubkeyToAddress(pk.PublicKey)
	return &store.EVMSlot{
		Address:    addr.Hex(),
		PrivateKey: hexKey,
	}, nil
}

func ImportEVMMnemonic(mnemonic string, index uint32) (*store.EVMSlot, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic")
	}
	seed := bip39.NewSeed(mnemonic, "")
	master, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
	if err != nil {
		return nil, err
	}
	// m/44'/60'/0'/0/index
	path := []uint32{
		hdkeychain.HardenedKeyStart + 44,
		hdkeychain.HardenedKeyStart + 60,
		hdkeychain.HardenedKeyStart + 0,
		0,
		index,
	}
	key := master
	for _, n := range path {
		key, err = key.Derive(n)
		if err != nil {
			return nil, fmt.Errorf("derive: %w", err)
		}
	}
	ec, err := key.ECPrivKey()
	if err != nil {
		return nil, err
	}
	hexKey := hex.EncodeToString(ec.Serialize())
	pk, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, err
	}
	addr := crypto.PubkeyToAddress(pk.PublicKey)
	return &store.EVMSlot{
		Address:    addr.Hex(),
		PrivateKey: hexKey,
		Mnemonic:   mnemonic,
		Index:      index,
	}, nil
}

func CreateSolana() (*store.SolanaSlot, error) {
	pk := solana.NewWallet()
	return &store.SolanaSlot{
		Address:    pk.PublicKey().String(),
		PrivateKey: pk.PrivateKey.String(),
	}, nil
}

func ImportSolanaPrivateKey(secret string) (*store.SolanaSlot, error) {
	secret = strings.TrimSpace(secret)
	if strings.HasPrefix(secret, "[") {
		return nil, fmt.Errorf("JSON byte-array keys not supported; use base58 secret key")
	}
	pk, err := solana.PrivateKeyFromBase58(secret)
	if err != nil {
		b, herr := hex.DecodeString(strings.TrimPrefix(secret, "0x"))
		if herr != nil {
			return nil, fmt.Errorf("invalid solana private key: %w", err)
		}
		switch len(b) {
		case 32:
			pk = solana.PrivateKey(ed25519.NewKeyFromSeed(b))
		case 64:
			pk = solana.PrivateKey(b)
		default:
			return nil, fmt.Errorf("hex key must be 32 or 64 bytes")
		}
	}
	return &store.SolanaSlot{
		Address:    pk.PublicKey().String(),
		PrivateKey: pk.String(),
	}, nil
}

// ImportSolanaMnemonic derives m/44'/501'/index'/0' via SLIP-0010 ed25519.
func ImportSolanaMnemonic(mnemonic string, index uint32) (*store.SolanaSlot, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic")
	}
	seed := bip39.NewSeed(mnemonic, "")
	path := []uint32{
		0x80000000 + 44,
		0x80000000 + 501,
		0x80000000 + index,
		0x80000000 + 0,
	}
	key, err := slip10Derive(seed, path)
	if err != nil {
		return nil, err
	}
	pk := solana.PrivateKey(ed25519.NewKeyFromSeed(key))
	return &store.SolanaSlot{
		Address:    pk.PublicKey().String(),
		PrivateKey: pk.String(),
	}, nil
}

func CreateHederaKeyOnly() (privKeyHex string, pubKeyHex string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(priv.Seed()), hex.EncodeToString(pub), nil
}

func ImportHedera(accountID, privateKey, network string) (*store.HederaSlot, error) {
	accountID = strings.TrimSpace(accountID)
	privateKey = strings.TrimSpace(privateKey)
	if accountID == "" {
		return nil, fmt.Errorf("account id required (0.0.x)")
	}
	if privateKey == "" {
		return nil, fmt.Errorf("private key required")
	}
	if network == "" {
		network = "testnet"
	}
	return &store.HederaSlot{
		AccountID:  accountID,
		PrivateKey: privateKey,
		Network:    network,
	}, nil
}

// slip10Derive implements SLIP-0010 ed25519 master + hardened child derivation.
func slip10Derive(seed []byte, path []uint32) ([]byte, error) {
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(seed)
	I := mac.Sum(nil)
	key, chain := I[:32], I[32:]
	for _, i := range path {
		if i&0x80000000 == 0 {
			return nil, fmt.Errorf("SLIP-0010 ed25519 only supports hardened derivation")
		}
		var data [1 + 32 + 4]byte
		data[0] = 0x00
		copy(data[1:33], key)
		binary.BigEndian.PutUint32(data[33:], i)
		mac = hmac.New(sha512.New, chain)
		mac.Write(data[:])
		I = mac.Sum(nil)
		key, chain = I[:32], I[32:]
	}
	return key, nil
}
