// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"github.com/spf13/cobra"

	"os"
	"strings"
)

var apiClient *APIClient

var version = "0.1.0"

func SetBuildInfo(v string) {
	if strings.TrimSpace(v) == "" {
		version = "0.1.0"
	} else {
		version = v
	}
}

func ResetAPIClientForTest() {
	apiClient = nil
}

func Run() {
	apiClient = NewAPIClient()

	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		PrintErrorJSON(err, nil)
		os.Exit(1)
	}
}

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "web3",
		Short:         "web3 companion-internal CLI — secure wallet and DeFi operations",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{\n  \"version\": \"{{.Version}}\"\n}\n")

	root.AddCommand(
		SSMCmd(),
		SwapCmd(),
		BridgeCmd(),
		TokenCmd(),
		PortfolioCmd(),
		ChainCmd(),
		SecurityCmd(),
		ContextCmd(),
		versionCmd(),
	)
	configureJSONCommandTree(root)

	return root
}

func SecurityCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "security", Short: "Transaction simulation and checks"}
	checkCmd := &cobra.Command{Use: "check", Short: "Simulate and check a transaction",
		Run: func(cmd *cobra.Command, args []string) {
			requireContainerClient()
			transactionID, _ := cmd.Flags().GetString("transaction-id")
			PrintJSON(StripSimulationTrace(mustPost("/security/check", BuildSecurityCheckParams(transactionID))))
		}}
	checkCmd.Flags().String("transaction-id", "", "Transaction intent ID to check")
	_ = checkCmd.MarkFlagRequired("transaction-id")
	traceCmd := &cobra.Command{Use: "trace", Short: "Show the full simulation trace for a transaction intent",
		Run: func(cmd *cobra.Command, args []string) {
			requireContainerClient()
			transactionID, _ := cmd.Flags().GetString("transaction-id")
			PrintJSON(ExtractSimulationTrace(mustPost("/security/check", BuildSecurityCheckParams(transactionID))))
		}}
	traceCmd.Flags().String("transaction-id", "", "Transaction intent ID")
	_ = traceCmd.MarkFlagRequired("transaction-id")
	overrideCmd := &cobra.Command{Use: "override", Short: "Explicitly override security review for a transaction intent",
		Run: func(cmd *cobra.Command, args []string) {
			requireContainerClient()
			transactionID, _ := cmd.Flags().GetString("transaction-id")
			reason, _ := cmd.Flags().GetString("reason")
			challengeID, _ := cmd.Flags().GetString("challenge-id")
			PrintJSON(mustPost("/security/override", BuildSecurityOverrideParams(transactionID, reason, challengeID)))
		}}
	overrideCmd.Flags().String("transaction-id", "", "Transaction intent ID to override")
	overrideCmd.Flags().String("reason", "", "User-provided reason for accepting the risk")
	overrideCmd.Flags().String("challenge-id", "", "Approved Passkey challenge ID")
	_ = overrideCmd.MarkFlagRequired("transaction-id")
	_ = overrideCmd.MarkFlagRequired("reason")
	cmd.AddCommand(
		checkCmd,
		traceCmd,
		overrideCmd,
	)
	return cmd
}
