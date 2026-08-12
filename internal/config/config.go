package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	AppName      = "saw"
	WalletFile   = "wallet.json"
	ConfigFile   = "config.env"
	ChannelsDir  = "channels"
	EnvPassword  = "SAW_PASSWORD"
	EnvConfigDir = "SAW_CONFIG_DIR"
	EnvBatchMult = "SAW_BATCH_DEPOSIT_MULTIPLIER"
)

// Dir returns ~/.config/saw (or SAW_CONFIG_DIR).
func Dir() (string, error) {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", AppName), nil
}

func EnsureDir() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

func WalletPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, WalletFile), nil
}

func ConfigPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, ConfigFile), nil
}

// ChannelsPath is ~/.config/saw/channels (batch-settlement session storage).
func ChannelsPath() (string, error) {
	d, err := EnsureDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, ChannelsDir)
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return p, nil
}

// BatchDepositMultiplier returns SAW_BATCH_DEPOSIT_MULTIPLIER or 5.
func BatchDepositMultiplier() int {
	v := strings.TrimSpace(os.Getenv(EnvBatchMult))
	if v == "" {
		return 5
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 5
	}
	return n
}

// Config holds non-secret runtime settings (RPCs).
type Config struct {
	// EVMRPC maps chain id (decimal string) -> RPC URL, from SAW_RPC_<id>
	EVMRPC map[string]string
	SolanaRPC string
	HederaNetwork string // testnet|mainnet
	HederaMirror  string
}

func Load() (*Config, error) {
	c := &Config{
		EVMRPC:        map[string]string{},
		HederaNetwork: "testnet",
	}
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		if err := loadEnvFile(path, c); err != nil {
			return nil, err
		}
	}
	// Process env overrides
	for _, e := range os.Environ() {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		applyEnv(c, k, v)
	}
	return c, nil
}

func loadEnvFile(path string, c *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		applyEnv(c, strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`))
	}
	return sc.Err()
}

func applyEnv(c *Config, k, v string) {
	switch {
	case strings.HasPrefix(k, "SAW_RPC_"):
		id := strings.TrimPrefix(k, "SAW_RPC_")
		if id != "" && v != "" {
			c.EVMRPC[id] = v
		}
	case k == "SAW_SOLANA_RPC":
		c.SolanaRPC = v
	case k == "SAW_HEDERA_NETWORK":
		c.HederaNetwork = v
	case k == "SAW_HEDERA_MIRROR":
		c.HederaMirror = v
	}
}

func (c *Config) EVMRPCURL(chainID int64) (string, error) {
	key := strconv.FormatInt(chainID, 10)
	if u, ok := c.EVMRPC[key]; ok && u != "" {
		return u, nil
	}
	return "", fmt.Errorf("no RPC configured for chain %d (set SAW_RPC_%d in %s)", chainID, chainID, ConfigFile)
}

// WriteExample writes a sample config.env if missing.
func WriteExample() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if _, err := EnsureDir(); err != nil {
		return err
	}
	const sample = `# saw RPC endpoints (no secrets)
# SAW_RPC_8453=https://mainnet.base.org
# SAW_RPC_84532=https://sepolia.base.org
# SAW_SOLANA_RPC=https://api.mainnet-beta.solana.com
# SAW_HEDERA_NETWORK=testnet
# SAW_HEDERA_MIRROR=
`
	return os.WriteFile(path, []byte(sample), 0o600)
}
