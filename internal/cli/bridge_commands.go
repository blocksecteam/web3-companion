// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"github.com/spf13/cobra"
)

func BridgeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bridge", Short: "Cross-chain bridge"}
	quoteCmd := &cobra.Command{Use: "quote", Short: "Get bridge quote",
		Run: func(cmd *cobra.Command, args []string) {
			PrintJSON(mustPost("/bridge/quote", BuildBridgeQuoteParams(cmd)))
		}}
	addBridgeRouteFlags(quoteCmd)
	quoteCmd.Flags().String("from-address", "", "Wallet/source address")
	_ = quoteCmd.MarkFlagRequired("from-address")

	buildCmd := &cobra.Command{Use: "build", Short: "Create a bridge transaction intent",
		Run: func(cmd *cobra.Command, args []string) {
			runBridgeBuild(cmd)
		}}
	addBridgeRouteFlags(buildCmd)
	buildCmd.Flags().String("wallet", "", "Wallet ID")
	_ = buildCmd.MarkFlagRequired("wallet")
	_ = buildCmd.MarkFlagRequired("bridge")

	cmd.AddCommand(
		&cobra.Command{Use: "protocols", Short: "List bridge protocols",
			Run: func(cmd *cobra.Command, args []string) { PrintJSON(mustGet("/bridge/protocols")) }},
		quoteCmd,
		buildCmd,
	)
	return cmd
}

func addBridgeRouteFlags(cmd *cobra.Command) {
	cmd.Flags().String("from-chain", "", "Source chain")
	cmd.Flags().String("to-chain", "", "Destination chain")
	cmd.Flags().String("from", "", "Source token: 'native' or contract address")
	cmd.Flags().String("to", "", "Destination token: 'native' or contract address")
	cmd.Flags().String("amount", "", "Human-readable amount (e.g. 5, 0.001)")
	cmd.Flags().String("bridge", "", "Bridge protocol")
	cmd.Flags().Float64("slippage", 0.5, "Slippage percent")
	_ = cmd.MarkFlagRequired("from-chain")
	_ = cmd.MarkFlagRequired("to-chain")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("amount")
}

func BuildBridgeQuoteParams(cmd *cobra.Command) map[string]interface{} {
	fromChain, _ := cmd.Flags().GetString("from-chain")
	toChain, _ := cmd.Flags().GetString("to-chain")
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	amt, _ := cmd.Flags().GetString("amount")
	fromAddress, _ := cmd.Flags().GetString("from-address")
	walletID, _ := cmd.Flags().GetString("wallet")
	bridgeID, _ := cmd.Flags().GetString("bridge")
	slippage, _ := cmd.Flags().GetFloat64("slippage")
	return map[string]interface{}{
		"from_chain":   fromChain,
		"to_chain":     toChain,
		"from_token":   from,
		"to_token":     to,
		"amount":       amt,
		"from_address": fromAddress,
		"wallet_id":    walletID,
		"bridge":       bridgeID,
		"slippage":     slippage,
	}
}

func runBridgeBuild(cmd *cobra.Command) {
	requireContainerClient()
	PrintJSON(mustPost("/bridge/build", BuildBridgeQuoteParams(cmd)))
}
