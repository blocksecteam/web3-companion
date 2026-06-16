// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"github.com/spf13/cobra"
)

func SwapCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "swap", Short: "DEX swap — quote and execute"}
	quoteCmd := &cobra.Command{Use: "quote", Short: "Get swap quote",
		Run: func(cmd *cobra.Command, args []string) {
			PrintJSON(mustPost("/swap/quote", BuildSwapParams(cmd)))
		}}
	AddSwapRouteFlags(quoteCmd)
	quoteCmd.Flags().String("from-address", "", "Wallet/source address")
	_ = quoteCmd.MarkFlagRequired("from-address")

	buildCmd := &cobra.Command{Use: "build", Short: "Quote and create a swap transaction intent",
		Run: func(cmd *cobra.Command, args []string) {
			runSwapBuild(cmd)
		}}
	AddSwapRouteFlags(buildCmd)
	buildCmd.Flags().String("wallet", "", "Wallet ID")
	_ = buildCmd.MarkFlagRequired("wallet")
	cmd.AddCommand(
		&cobra.Command{Use: "protocols", Short: "List swap protocols",
			Run: func(cmd *cobra.Command, args []string) { PrintJSON(mustGet("/swap/protocols")) }},
		quoteCmd,
		buildCmd,
	)
	return cmd
}

func AddSwapRouteFlags(cmd *cobra.Command) {
	cmd.Flags().String("chain", "", "Chain name")
	cmd.Flags().String("from", "", "Source token address or symbol")
	cmd.Flags().String("to", "", "Target token address or symbol")
	cmd.Flags().String("amount", "", "Human-readable amount (e.g. 0.001)")
	cmd.Flags().String("protocol", "", "Protocol name")
	cmd.Flags().Float64("slippage", 0.5, "Slippage percent")
	_ = cmd.MarkFlagRequired("chain")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("amount")
}

func BuildSwapParams(cmd *cobra.Command) map[string]interface{} {
	chain, _ := cmd.Flags().GetString("chain")
	from, _ := cmd.Flags().GetString("from")
	fromAddress, _ := cmd.Flags().GetString("from-address")
	walletID, _ := cmd.Flags().GetString("wallet")
	to, _ := cmd.Flags().GetString("to")
	amt, _ := cmd.Flags().GetString("amount")
	protocol, _ := cmd.Flags().GetString("protocol")
	slippage, _ := cmd.Flags().GetFloat64("slippage")
	return map[string]interface{}{
		"chain": chain, "from_address": fromAddress, "source_asset": from,
		"target_asset": to, "amount": amt,
		"protocol": protocol, "slippage": slippage, "wallet_id": walletID,
	}
}

func runSwapBuild(cmd *cobra.Command) {
	requireContainerClient()
	PrintJSON(mustPost("/swap/build", BuildSwapParams(cmd)))
}
