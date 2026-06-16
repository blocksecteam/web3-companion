// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"github.com/spf13/cobra"
)

func ChainCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "chain", Short: "Chain operations — gas, broadcast, tx status"}
	broadcastCmd := &cobra.Command{Use: "broadcast <chain>", Short: "Broadcast a signed transaction", Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireContainerClient()
			transactionID, _ := cmd.Flags().GetString("transaction-id")
			PrintJSON(mustPost("/chain/broadcast", map[string]interface{}{
				"chain":          args[0],
				"transaction_id": transactionID,
			}))
		}}
	broadcastCmd.Flags().String("transaction-id", "", "Transaction intent ID to broadcast")
	_ = broadcastCmd.MarkFlagRequired("transaction-id")
	transferCmd := &cobra.Command{Use: "transfer", Short: "Transfer native or ERC-20 tokens",
		Run: func(cmd *cobra.Command, args []string) {
			requireContainerClient()
			chain, _ := cmd.Flags().GetString("chain")
			tok, _ := cmd.Flags().GetString("token")
			to, _ := cmd.Flags().GetString("to")
			amount, _ := cmd.Flags().GetString("amount")
			walletID, _ := cmd.Flags().GetString("wallet")

			PrintJSON(mustPost("/chain/transfer", map[string]interface{}{
				"chain":     chain,
				"wallet_id": walletID,
				"token":     tok,
				"to":        to,
				"amount":    amount,
			}))
		}}
	transferCmd.Flags().String("chain", "", "Chain name")
	transferCmd.Flags().String("token", "", "Token: 'native' or ERC-20 contract address")
	transferCmd.Flags().String("to", "", "Recipient address")
	transferCmd.Flags().String("amount", "", "Human-readable amount (e.g. 0.1)")
	transferCmd.Flags().String("wallet", "", "Wallet ID for signing")
	_ = transferCmd.MarkFlagRequired("chain")
	_ = transferCmd.MarkFlagRequired("token")
	_ = transferCmd.MarkFlagRequired("to")
	_ = transferCmd.MarkFlagRequired("amount")
	_ = transferCmd.MarkFlagRequired("wallet")

	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List supported chains and canonical token addresses",
			Run: func(cmd *cobra.Command, args []string) { printChainList() }},
		&cobra.Command{Use: "gas <chain>", Short: "Get gas price", Args: cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) { PrintJSON(mustGet("/chain/gas/" + args[0])) }},
		broadcastCmd,
		&cobra.Command{Use: "tx <chain> <hash>", Short: "Transaction status", Args: cobra.ExactArgs(2),
			Run: func(cmd *cobra.Command, args []string) {
				PrintJSON(mustGet("/chain/transaction/" + args[0] + "/" + args[1]))
			}},
		transferCmd,
	)
	return cmd
}

func printChainList() {
	PrintJSON(mustGet("/chain/supported"))
}
