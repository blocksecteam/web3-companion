// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"net/url"
	"os"
)

func TokenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Token search and info (Portals.fi)"}
	searchCmd := &cobra.Command{Use: "search", Short: "Search tokens",
		Run: func(cmd *cobra.Command, args []string) {
			q, _ := cmd.Flags().GetString("query")
			chain, _ := cmd.Flags().GetString("chain")
			values := url.Values{}
			values.Set("query", q)
			values.Set("networks", chain)
			PrintJSON(mustGet("/token/search?" + values.Encode()))
		}}
	searchCmd.Flags().String("query", "", "Search query")
	searchCmd.Flags().String("chain", "", "Chain name (e.g. ethereum, base, bsc, polygon) — required")
	_ = searchCmd.MarkFlagRequired("query")
	_ = searchCmd.MarkFlagRequired("chain")
	infoCmd := &cobra.Command{Use: "info <address>", Short: "Token info", Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			chain, _ := cmd.Flags().GetString("chain")
			PrintJSON(mustGet("/token/info/" + chain + "/" + args[0]))
		}}
	infoCmd.Flags().String("chain", "", "Chain name — required")
	_ = infoCmd.MarkFlagRequired("chain")
	cmd.AddCommand(searchCmd, infoCmd)
	return cmd
}

func PortfolioCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "portfolio [<chain> <address>]",
		Short: "Get portfolio balance via Portals.fi",
		Args:  cobra.RangeArgs(0, 2),
		Run: func(cmd *cobra.Command, args []string) {
			walletID, _ := cmd.Flags().GetString("wallet")
			chain, _ := cmd.Flags().GetString("chain")

			if walletID != "" {
				if chain == "" {
					PrintErrorJSON(fmt.Errorf("--chain is required with --wallet; pass one of the chains from `web3 chain list`"), nil)
					os.Exit(1)
				}
				address := walletAddressFromUserContext(mustGet("/users/context"), walletID)
				if address == "" {
					PrintErrorJSON(fmt.Errorf("wallet %q not found", walletID), nil)
					os.Exit(1)
				}
				PrintJSON(mustGet("/portfolio/" + chain + "/" + address))
				return
			}
			if len(args) != 2 {
				PrintErrorJSON(fmt.Errorf("provide --wallet <id> --chain <chain>, or <chain> <address>"), nil)
				os.Exit(1)
			}
			PrintJSON(mustGet("/portfolio/" + args[0] + "/" + args[1]))
		},
	}
	cmd.Flags().String("wallet", "", "Wallet ID (looks up address from SSM automatically)")
	cmd.Flags().String("chain", "", "Chain name or comma-separated list (required when using --wallet); e.g. 'ethereum' or 'ethereum,base,bsc,polygon'")
	return cmd
}

func walletAddressFromUserContext(data []byte, walletID string) string {
	var response struct {
		Wallets []struct {
			WalletID string `json:"wallet_id"`
			Address  string `json:"address"`
		} `json:"wallets"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return ""
	}
	for _, wallet := range response.Wallets {
		if wallet.WalletID == walletID {
			return wallet.Address
		}
	}
	return ""
}
