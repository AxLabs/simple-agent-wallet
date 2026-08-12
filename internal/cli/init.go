package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/wallet"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Interactive wallet setup (or use create/import/status)",
		RunE:  runInitWizard,
	}
	cmd.AddCommand(newInitCreateCmd())
	cmd.AddCommand(newInitImportCmd())
	cmd.AddCommand(newInitStatusCmd())
	return cmd
}

func newInitStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show configured families (no secrets)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := store.LoadOrEmpty(resolvePassword())
			if err != nil {
				return err
			}
			st, err := w.Status()
			if err != nil {
				return err
			}
			return printJSON(st)
		},
	}
}

func newInitCreateCmd() *cobra.Command {
	var family string
	var index uint32
	var encrypt bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new key for a family",
		RunE: func(cmd *cobra.Command, args []string) error {
			family = strings.ToLower(family)
			if family == "" {
				return fmt.Errorf("--family required (evm|solana|hedera)")
			}
			w, err := store.LoadOrEmpty(resolvePassword())
			if err != nil {
				return err
			}
			if err := confirmOverwrite(w, family); err != nil {
				return err
			}
			switch family {
			case store.FamilyEVM:
				slot, mnemonic, err := wallet.CreateEVM(index)
				if err != nil {
					return err
				}
				w.EVM = slot
				fmt.Fprintln(os.Stderr, "Created EVM wallet. Write down this mnemonic (shown once):")
				fmt.Fprintln(os.Stderr, mnemonic)
			case store.FamilySolana:
				slot, err := wallet.CreateSolana()
				if err != nil {
					return err
				}
				w.Solana = slot
				fmt.Fprintln(os.Stderr, "Created Solana keypair.")
			case store.FamilyHedera:
				priv, pub, err := wallet.CreateHederaKeyOnly()
				if err != nil {
					return err
				}
				fmt.Fprintln(os.Stderr, "Hedera on-network account create is out of scope.")
				fmt.Fprintln(os.Stderr, "Generated key — create/fund an account elsewhere, then: saw init import --family hedera --account-id 0.0.x")
				fmt.Fprintf(os.Stderr, "ed25519 seed (hex): %s\n", priv)
				fmt.Fprintf(os.Stderr, "public key (hex): %s\n", pub)
				return nil
			default:
				return fmt.Errorf("unknown family %q", family)
			}
			pw := resolvePassword()
			if encrypt && pw == "" {
				pw, err = promptSecret("Encryption password: ")
				if err != nil {
					return err
				}
			}
			if err := w.Save(pw); err != nil {
				return err
			}
			_ = config.WriteExample()
			st, _ := w.Status()
			return printJSON(st)
		},
	}
	cmd.Flags().StringVar(&family, "family", "", "evm|solana|hedera")
	cmd.Flags().Uint32Var(&index, "index", 0, "HD account index (EVM)")
	cmd.Flags().BoolVar(&encrypt, "encrypt", false, "encrypt wallet with password")
	return cmd
}

func newInitImportCmd() *cobra.Command {
	var family, accountID, network string
	var index uint32
	var useMnemonic bool
	var encrypt bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import key/mnemonic from stdin",
		RunE: func(cmd *cobra.Command, args []string) error {
			family = strings.ToLower(family)
			if family == "" {
				return fmt.Errorf("--family required")
			}
			secret, err := readSecret("Paste secret (private key or mnemonic), then EOF/newline: ")
			if err != nil {
				return err
			}
			w, err := store.LoadOrEmpty(resolvePassword())
			if err != nil {
				return err
			}
			if err := confirmOverwrite(w, family); err != nil {
				return err
			}
			switch family {
			case store.FamilyEVM:
				if useMnemonic || looksLikeMnemonic(secret) {
					w.EVM, err = wallet.ImportEVMMnemonic(secret, index)
				} else {
					w.EVM, err = wallet.ImportEVMPrivateKey(secret)
				}
			case store.FamilySolana:
				if useMnemonic || looksLikeMnemonic(secret) {
					w.Solana, err = wallet.ImportSolanaMnemonic(secret, index)
				} else {
					w.Solana, err = wallet.ImportSolanaPrivateKey(secret)
				}
			case store.FamilyHedera:
				if accountID == "" {
					return fmt.Errorf("--account-id required for hedera")
				}
				w.Hedera, err = wallet.ImportHedera(accountID, secret, network)
			default:
				return fmt.Errorf("unknown family %q", family)
			}
			if err != nil {
				return err
			}
			pw := resolvePassword()
			if encrypt && pw == "" {
				pw, err = promptSecret("Encryption password: ")
				if err != nil {
					return err
				}
			}
			if err := w.Save(pw); err != nil {
				return err
			}
			_ = config.WriteExample()
			st, _ := w.Status()
			return printJSON(st)
		},
	}
	cmd.Flags().StringVar(&family, "family", "", "evm|solana|hedera")
	cmd.Flags().StringVar(&accountID, "account-id", "", "Hedera account id 0.0.x")
	cmd.Flags().StringVar(&network, "network", "testnet", "Hedera network testnet|mainnet")
	cmd.Flags().Uint32Var(&index, "index", 0, "HD index")
	cmd.Flags().BoolVar(&useMnemonic, "mnemonic", false, "treat stdin as mnemonic")
	cmd.Flags().BoolVar(&encrypt, "encrypt", false, "encrypt wallet")
	return cmd
}

func runInitWizard(cmd *cobra.Command, args []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("interactive init requires a TTY; use saw init create/import")
	}
	fmt.Println("saw init — choose family:")
	fmt.Println("  1) EVM")
	fmt.Println("  2) Solana")
	fmt.Println("  3) Hedera")
	choice, err := prompt("Family [1-3]: ")
	if err != nil {
		return err
	}
	var family string
	switch strings.TrimSpace(choice) {
	case "1", "evm", "EVM":
		family = store.FamilyEVM
	case "2", "solana", "Solana":
		family = store.FamilySolana
	case "3", "hedera", "Hedera":
		family = store.FamilyHedera
	default:
		return fmt.Errorf("invalid choice")
	}
	mode, err := prompt("Create new or Import existing? [c/i]: ")
	if err != nil {
		return err
	}
	w, err := store.LoadOrEmpty(resolvePassword())
	if err != nil {
		return err
	}
	if err := confirmOverwrite(w, family); err != nil {
		return err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch family {
	case store.FamilyEVM:
		if mode == "c" || mode == "create" {
			idx := uint32(0)
			if s, _ := prompt("Account index [0]: "); strings.TrimSpace(s) != "" {
				n, _ := strconv.Atoi(strings.TrimSpace(s))
				idx = uint32(n)
			}
			slot, mnemonic, err := wallet.CreateEVM(idx)
			if err != nil {
				return err
			}
			w.EVM = slot
			fmt.Println("Mnemonic (write down, shown once):")
			fmt.Println(mnemonic)
		} else {
			kind, _ := prompt("Import private key or mnemonic? [k/m]: ")
			secret, err := promptSecret("Secret: ")
			if err != nil {
				return err
			}
			if strings.HasPrefix(strings.ToLower(kind), "m") {
				idx := uint32(0)
				if s, _ := prompt("Account index [0]: "); strings.TrimSpace(s) != "" {
					n, _ := strconv.Atoi(strings.TrimSpace(s))
					idx = uint32(n)
				}
				w.EVM, err = wallet.ImportEVMMnemonic(secret, idx)
			} else {
				w.EVM, err = wallet.ImportEVMPrivateKey(secret)
			}
			if err != nil {
				return err
			}
		}
	case store.FamilySolana:
		if mode == "c" || mode == "create" {
			w.Solana, err = wallet.CreateSolana()
		} else {
			secret, err := promptSecret("Solana secret key (base58) or mnemonic: ")
			if err != nil {
				return err
			}
			if looksLikeMnemonic(secret) {
				w.Solana, err = wallet.ImportSolanaMnemonic(secret, 0)
			} else {
				w.Solana, err = wallet.ImportSolanaPrivateKey(secret)
			}
		}
		if err != nil {
			return err
		}
	case store.FamilyHedera:
		if mode == "c" || mode == "create" {
			priv, pub, err := wallet.CreateHederaKeyOnly()
			if err != nil {
				return err
			}
			fmt.Println("On-network Hedera account create is out of scope for v1.")
			fmt.Println("Use this key when creating an account elsewhere, then re-run import.")
			fmt.Println("ed25519 seed (hex):", priv)
			fmt.Println("public key (hex):", pub)
			return nil
		}
		acc, err := prompt("Account ID (0.0.x): ")
		if err != nil {
			return err
		}
		net, _ := prompt("Network [testnet]: ")
		if strings.TrimSpace(net) == "" {
			net = "testnet"
		}
		secret, err := promptSecret("Private key: ")
		if err != nil {
			return err
		}
		w.Hedera, err = wallet.ImportHedera(acc, secret, net)
		if err != nil {
			return err
		}
	}
	enc, _ := prompt("Encrypt wallet with password? [y/N]: ")
	pw := resolvePassword()
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(enc)), "y") {
		pw, err = promptSecret("Password: ")
		if err != nil {
			return err
		}
	}
	if err := w.Save(pw); err != nil {
		return err
	}
	_ = config.WriteExample()
	st, _ := w.Status()
	return printJSON(st)
}

func confirmOverwrite(w *store.Wallet, family string) error {
	if !w.HasFamily(family) {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("family %s already configured; delete wallet.json or run interactive init", family)
	}
	ans, err := prompt(fmt.Sprintf("Overwrite existing %s slot? [y/N]: ", family))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
		return fmt.Errorf("aborted")
	}
	return nil
}

func looksLikeMnemonic(s string) bool {
	parts := strings.Fields(strings.TrimSpace(s))
	return len(parts) == 12 || len(parts) == 24
}

func prompt(msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func promptSecret(msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	return readSecret("")
}

func readSecret(promptMsg string) (string, error) {
	if promptMsg != "" && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, promptMsg)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
