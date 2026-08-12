package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"golang.org/x/crypto/pbkdf2"
)

const (
	schemaVersion = 1
	FamilyEVM     = "evm"
	FamilySolana  = "solana"
	FamilyHedera  = "hedera"
)

var ErrNotFound = errors.New("wallet not found")
var ErrFamilyMissing = errors.New("family not configured")

// Wallet is the on-disk multi-family store (secrets encrypted optionally).
type Wallet struct {
	Version   int         `json:"version"`
	Encrypted bool        `json:"encrypted"`
	EVM       *EVMSlot    `json:"evm,omitempty"`
	Solana    *SolanaSlot `json:"solana,omitempty"`
	Hedera    *HederaSlot `json:"hedera,omitempty"`
	// Ciphertext holds whole-file encryption when Encrypted is true (base slots cleared).
	Salt       []byte `json:"salt,omitempty"`
	Nonce      []byte `json:"nonce,omitempty"`
	Ciphertext []byte `json:"ciphertext,omitempty"`
}

type EVMSlot struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey,omitempty"` // hex, no 0x required
	Mnemonic   string `json:"mnemonic,omitempty"`
	Index      uint32 `json:"index,omitempty"`
}

type SolanaSlot struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"` // base58
}

type HederaSlot struct {
	AccountID  string `json:"accountId"`
	PrivateKey string `json:"privateKey"`
	Network    string `json:"network,omitempty"`
}

// Status is a secret-free summary.
type Status struct {
	EVM       *string `json:"evm"`
	Solana    *string `json:"solana"`
	Hedera    *string `json:"hedera"`
	Path      string  `json:"path"`
	Encrypted bool    `json:"encrypted"`
}

type plainPayload struct {
	EVM    *EVMSlot    `json:"evm,omitempty"`
	Solana *SolanaSlot `json:"solana,omitempty"`
	Hedera *HederaSlot `json:"hedera,omitempty"`
}

func Load(password string) (*Wallet, error) {
	path, err := config.WalletPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var w Wallet
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("wallet.json: %w", err)
	}
	if w.Encrypted {
		if password == "" {
			password = os.Getenv(config.EnvPassword)
		}
		if password == "" {
			return nil, fmt.Errorf("wallet is encrypted; set %s or pass password", config.EnvPassword)
		}
		plain, err := decrypt(w.Salt, w.Nonce, w.Ciphertext, password)
		if err != nil {
			return nil, fmt.Errorf("decrypt wallet: %w", err)
		}
		var p plainPayload
		if err := json.Unmarshal(plain, &p); err != nil {
			return nil, err
		}
		w.EVM, w.Solana, w.Hedera = p.EVM, p.Solana, p.Hedera
		w.Salt, w.Nonce, w.Ciphertext = nil, nil, nil
	}
	return &w, nil
}

func (w *Wallet) Save(password string) error {
	if _, err := config.EnsureDir(); err != nil {
		return err
	}
	path, err := config.WalletPath()
	if err != nil {
		return err
	}
	out := Wallet{Version: schemaVersion, Encrypted: password != ""}
	if password != "" {
		p, err := json.Marshal(plainPayload{EVM: w.EVM, Solana: w.Solana, Hedera: w.Hedera})
		if err != nil {
			return err
		}
		salt, nonce, ct, err := encrypt(p, password)
		if err != nil {
			return err
		}
		out.Salt, out.Nonce, out.Ciphertext = salt, nonce, ct
	} else {
		out.EVM, out.Solana, out.Hedera = w.EVM, w.Solana, w.Hedera
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (w *Wallet) Status() (Status, error) {
	path, err := config.WalletPath()
	if err != nil {
		return Status{}, err
	}
	s := Status{Path: path, Encrypted: w.Encrypted}
	if w.EVM != nil && w.EVM.Address != "" {
		a := w.EVM.Address
		s.EVM = &a
	}
	if w.Solana != nil && w.Solana.Address != "" {
		a := w.Solana.Address
		s.Solana = &a
	}
	if w.Hedera != nil && w.Hedera.AccountID != "" {
		a := w.Hedera.AccountID
		s.Hedera = &a
	}
	return s, nil
}

func (w *Wallet) HasFamily(family string) bool {
	switch strings.ToLower(family) {
	case FamilyEVM:
		return w.EVM != nil && w.EVM.PrivateKey != ""
	case FamilySolana:
		return w.Solana != nil && w.Solana.PrivateKey != ""
	case FamilyHedera:
		return w.Hedera != nil && w.Hedera.PrivateKey != "" && w.Hedera.AccountID != ""
	default:
		return false
	}
}

func encrypt(plain []byte, password string) (salt, nonce, ct []byte, err error) {
	salt = make([]byte, 16)
	if _, err = io.ReadFull(rand.Reader, salt); err != nil {
		return
	}
	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return
	}
	ct = gcm.Seal(nil, nonce, plain, nil)
	return
}

func decrypt(salt, nonce, ct []byte, password string) ([]byte, error) {
	key := pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ct, nil)
}

// LoadOrEmpty returns empty wallet if missing.
func LoadOrEmpty(password string) (*Wallet, error) {
	w, err := Load(password)
	if errors.Is(err, ErrNotFound) {
		return &Wallet{Version: schemaVersion}, nil
	}
	return w, err
}
