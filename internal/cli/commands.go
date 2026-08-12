package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AxLabs/simple-agent-wallet/internal/config"
	"github.com/AxLabs/simple-agent-wallet/internal/store"
	"github.com/AxLabs/simple-agent-wallet/internal/tx"
	x402pay "github.com/AxLabs/simple-agent-wallet/internal/x402"
	"github.com/spf13/cobra"
)

func loadWallet() (*store.Wallet, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	w, err := store.Load(resolvePassword())
	if err != nil {
		return nil, nil, err
	}
	return w, cfg, nil
}

func newAddressCmd() *cobra.Command {
	var family string
	cmd := &cobra.Command{
		Use:   "address",
		Short: "Print configured addresses",
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := store.Load(resolvePassword())
			if err != nil {
				return err
			}
			st, err := w.Status()
			if err != nil {
				return err
			}
			if family != "" {
				switch strings.ToLower(family) {
				case store.FamilyEVM:
					if st.EVM == nil {
						return store.ErrFamilyMissing
					}
					fmt.Println(*st.EVM)
				case store.FamilySolana:
					if st.Solana == nil {
						return store.ErrFamilyMissing
					}
					fmt.Println(*st.Solana)
				case store.FamilyHedera:
					if st.Hedera == nil {
						return store.ErrFamilyMissing
					}
					fmt.Println(*st.Hedera)
				default:
					return fmt.Errorf("unknown family")
				}
				return nil
			}
			return printJSON(st)
		},
	}
	cmd.Flags().StringVar(&family, "family", "", "evm|solana|hedera")
	return cmd
}

func newBalanceCmd() *cobra.Command {
	var family, token string
	var chainID int64
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "Show native/token balances",
		RunE: func(cmd *cobra.Command, args []string) error {
			w, cfg, err := loadWallet()
			if err != nil {
				return err
			}
			ctx := context.Background()
			family = strings.ToLower(family)
			if family == "" {
				family = store.FamilyEVM
			}
			switch family {
			case store.FamilyEVM:
				if chainID == 0 {
					return fmt.Errorf("--chain-id required for EVM")
				}
				if token != "" {
					bal, err := tx.ERC20Balance(ctx, cfg, w, chainID, token)
					if err != nil {
						return err
					}
					fmt.Println(bal.String())
					return nil
				}
				bal, err := tx.NativeBalance(ctx, cfg, w, chainID)
				if err != nil {
					return err
				}
				fmt.Println(bal.String())
			case store.FamilySolana:
				bal, err := tx.SolanaBalance(ctx, cfg, w)
				if err != nil {
					return err
				}
				fmt.Println(bal)
			case store.FamilyHedera:
				return fmt.Errorf("hedera balance via mirror not implemented in v1; use mirror node explorer")
			default:
				return fmt.Errorf("unknown family")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&family, "family", "evm", "evm|solana|hedera")
	cmd.Flags().Int64Var(&chainID, "chain-id", 0, "EVM chain id")
	cmd.Flags().StringVar(&token, "token", "", "ERC-20 token address")
	return cmd
}

func newInspectCmd() *cobra.Command {
	var method, data string
	cmd := &cobra.Command{
		Use:   "inspect <url>",
		Short: "Decode x402 accepts from a URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var body []byte
			if data != "" {
				body = []byte(data)
			}
			if method == "" {
				if len(body) > 0 {
					method = "POST"
				} else {
					method = "GET"
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			insp, _, _, err := x402pay.Fetch402(ctx, method, args[0], body, nil)
			if err != nil {
				return err
			}
			return printJSON(insp)
		},
	}
	cmd.Flags().StringVar(&method, "method", "", "HTTP method")
	cmd.Flags().StringVar(&data, "data", "", "request body")
	return cmd
}

func newPayCmd() *cobra.Command {
	var method, data, network, asset, transferMethod, scheme, orderStatus string
	var index int
	cmd := &cobra.Command{
		Use:   "pay <url>",
		Short: "Pay a 402 resource (exact or batch-settlement; requires --confirm)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("refusing to pay without --confirm")
			}
			w, cfg, err := loadWallet()
			if err != nil {
				return err
			}
			var body []byte
			if data != "" {
				body = []byte(data)
			}
			if method == "" {
				if len(body) > 0 {
					method = "POST"
				} else {
					method = "GET"
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			sel := x402pay.SelectOpts{
				Network: network,
				Asset:   asset,
				Method:  transferMethod,
				Scheme:  scheme,
			}
			if cmd.Flags().Changed("index") {
				sel.PreferIndex = true
				sel.Index = index
			}
			res, err := x402pay.Pay(ctx, w, cfg, method, args[0], body, nil, sel)
			if err != nil {
				return err
			}
			if orderStatus != "" && res.PaymentResponse != nil {
				if txHash, ok := res.PaymentResponse["transaction"].(string); ok && txHash != "" {
					st, err := x402pay.PollOrderStatus(ctx, orderStatus, txHash, 2*time.Second, 60*time.Second)
					if err == nil {
						res.PaymentResponse["orderStatus"] = st
					}
				}
			}
			return printJSON(res)
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required to submit payment")
	cmd.Flags().StringVar(&method, "method", "", "HTTP method")
	cmd.Flags().StringVar(&data, "data", "", "request body")
	cmd.Flags().StringVar(&network, "network", "", "filter accept network")
	cmd.Flags().StringVar(&asset, "asset", "", "filter accept asset")
	cmd.Flags().StringVar(&transferMethod, "asset-transfer-method", "", "eip3009|permit2 (exact)")
	cmd.Flags().StringVar(&scheme, "scheme", "", "exact|batch-settlement")
	cmd.Flags().IntVar(&index, "index", 0, "accept index (only used when flag set)")
	cmd.Flags().StringVar(&orderStatus, "order-status", "", "poll URL with {tx} placeholder")
	return cmd
}

func newChannelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "List local batch-settlement channel sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := x402pay.ListChannels()
			if err != nil {
				return err
			}
			return printJSON(list)
		},
	}
	return cmd
}

func newRefundCmd() *cobra.Command {
	var amount string
	cmd := &cobra.Command{
		Use:   "refund <url>",
		Short: "Cooperative batch-settlement channel refund (requires --confirm)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("refusing without --confirm")
			}
			w, cfg, err := loadWallet()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			resp, err := x402pay.RefundBatch(ctx, w, cfg, args[0], amount)
			if err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required")
	cmd.Flags().StringVar(&amount, "amount", "", "partial refund in token base units (omit for full)")
	return cmd
}

func newApprovePermit2Cmd() *cobra.Command {
	var chainID int64
	var token string
	cmd := &cobra.Command{
		Use:   "approve-permit2",
		Short: "Approve Permit2 max allowance for an ERC-20",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("refusing without --confirm")
			}
			if chainID == 0 || token == "" {
				return fmt.Errorf("--chain-id and --token required")
			}
			w, cfg, err := loadWallet()
			if err != nil {
				return err
			}
			h, err := tx.ApprovePermit2(context.Background(), cfg, w, chainID, token)
			if err != nil {
				return err
			}
			return printJSON(map[string]string{"txHash": h})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required")
	cmd.Flags().Int64Var(&chainID, "chain-id", 0, "EVM chain id")
	cmd.Flags().StringVar(&token, "token", "", "ERC-20 address")
	return cmd
}

func newTransferCmd() *cobra.Command {
	var family, to, amount, token string
	var chainID int64
	cmd := &cobra.Command{
		Use:   "transfer",
		Short: "Transfer native or token assets",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("refusing without --confirm")
			}
			w, cfg, err := loadWallet()
			if err != nil {
				return err
			}
			family = strings.ToLower(family)
			ctx := context.Background()
			switch family {
			case store.FamilyEVM, "":
				if chainID == 0 || to == "" || amount == "" {
					return fmt.Errorf("--chain-id --to --amount required")
				}
				amt, err := tx.ParseAmount(amount)
				if err != nil {
					return err
				}
				var h string
				if token != "" {
					h, err = tx.TransferERC20(ctx, cfg, w, chainID, token, to, amt)
				} else {
					h, err = tx.TransferNative(ctx, cfg, w, chainID, to, amt)
				}
				if err != nil {
					return err
				}
				return printJSON(map[string]string{"txHash": h})
			case store.FamilySolana:
				lamports, err := strconv.ParseUint(amount, 10, 64)
				if err != nil {
					return err
				}
				h, err := tx.TransferSOL(ctx, cfg, w, to, lamports)
				if err != nil {
					return err
				}
				return printJSON(map[string]string{"signature": h})
			case store.FamilyHedera:
				tb, err := tx.ParseTinybars(amount)
				if err != nil {
					return err
				}
				h, err := tx.TransferHBAR(cfg, w, to, tb)
				if err != nil {
					return err
				}
				return printJSON(map[string]string{"transactionId": h})
			default:
				return fmt.Errorf("unknown family")
			}
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required")
	cmd.Flags().StringVar(&family, "family", "evm", "evm|solana|hedera")
	cmd.Flags().Int64Var(&chainID, "chain-id", 0, "EVM chain id")
	cmd.Flags().StringVar(&to, "to", "", "recipient")
	cmd.Flags().StringVar(&amount, "amount", "", "atomic units")
	cmd.Flags().StringVar(&token, "token", "", "ERC-20 token (EVM)")
	return cmd
}

func newCallCmd() *cobra.Command {
	var to, data, value string
	var chainID int64
	cmd := &cobra.Command{
		Use:   "call",
		Short: "Send raw EVM contract call",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("refusing without --confirm")
			}
			w, cfg, err := loadWallet()
			if err != nil {
				return err
			}
			if chainID == 0 || to == "" {
				return fmt.Errorf("--chain-id and --to required")
			}
			var raw []byte
			if data != "" {
				raw, err = parseHex(data)
				if err != nil {
					return err
				}
			}
			val, _ := tx.ParseAmount(value)
			h, err := tx.Call(context.Background(), cfg, w, chainID, to, val, raw)
			if err != nil {
				return err
			}
			return printJSON(map[string]string{"txHash": h})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required")
	cmd.Flags().Int64Var(&chainID, "chain-id", 0, "chain id")
	cmd.Flags().StringVar(&to, "to", "", "contract")
	cmd.Flags().StringVar(&data, "data", "", "hex calldata")
	cmd.Flags().StringVar(&value, "value", "0", "wei")
	return cmd
}

func newABICmd() *cobra.Command {
	cmd := &cobra.Command{Use: "abi", Short: "ABI helpers"}
	encode := &cobra.Command{
		Use:   "encode <signature> [args...]",
		Short: "Encode calldata from signature",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := tx.EncodeABI(args[0], args[1:])
			if err != nil {
				return err
			}
			fmt.Printf("0x%x\n", out)
			return nil
		},
	}
	cmd.AddCommand(encode)
	return cmd
}

func parseHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	return hex.DecodeString(s)
}
