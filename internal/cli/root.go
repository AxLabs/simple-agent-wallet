package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	password  string
	confirm   bool
	outJSON   bool
)

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "saw",
		Short:         "Simple Agent Wallet — multi-family x402 payments",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&password, "password", "", "wallet password (prefer SAW_PASSWORD)")
	root.PersistentFlags().BoolVar(&outJSON, "json", false, "JSON output")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newAddressCmd())
	root.AddCommand(newBalanceCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newPayCmd())
	root.AddCommand(newChannelsCmd())
	root.AddCommand(newRefundCmd())
	root.AddCommand(newApprovePermit2Cmd())
	root.AddCommand(newTransferCmd())
	root.AddCommand(newCallCmd())
	root.AddCommand(newABICmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func resolvePassword() string {
	if password != "" {
		return password
	}
	return os.Getenv("SAW_PASSWORD")
}
