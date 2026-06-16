// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"github.com/spf13/cobra"

	"os"
	"strings"
	"time"
)

func SSMCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ssm", Short: "Secure Signature Module — wallet inspection and signing"}

	cmd.AddCommand(
		ssmSignCmd(),
		ssmDelegateCmd(),
		ssmDelegateEstimateCmd(),
		&cobra.Command{Use: "sign-status <challenge_id>", Short: "Poll a WebAuthn signing challenge",
			Args: cobra.ExactArgs(1), Run: func(cmd *cobra.Command, args []string) {
				PrintJSON(mustGet("/ssm/sign/" + args[0]))
			}},
	)
	return cmd
}

func ssmDelegateEstimateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate-estimate",
		Short: "Preview the EIP-7702 delegation cost for a wallet on one chain",
	}
	cmd.Flags().String("wallet", "", "Wallet ID to estimate for")
	cmd.Flags().String("chain", "", "Chain to estimate against")
	_ = cmd.MarkFlagRequired("wallet")
	_ = cmd.MarkFlagRequired("chain")
	cmd.Run = func(cmd *cobra.Command, args []string) {
		requireContainerClient()
		walletID, _ := cmd.Flags().GetString("wallet")
		chain, _ := cmd.Flags().GetString("chain")
		path := "/ssm/delegate/estimate?wallet_id=" +
			strings.TrimSpace(walletID) +
			"&chain=" + strings.TrimSpace(chain)
		PrintJSON(mustGet(path))
	}
	return cmd
}

func ssmDelegateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate",
		Short: "Initiate EIP-7702 delegation for a wallet",
	}
	cmd.Flags().String("wallet", "", "Wallet ID to delegate")
	cmd.Flags().String("chain", "", "Chain to delegate on")
	cmd.Flags().String("challenge-id", "", "Approved Passkey challenge ID used to resume delegation")
	_ = cmd.MarkFlagRequired("wallet")
	_ = cmd.MarkFlagRequired("chain")
	cmd.Run = func(cmd *cobra.Command, args []string) {
		requireContainerClient()
		walletID, _ := cmd.Flags().GetString("wallet")
		chain, _ := cmd.Flags().GetString("chain")
		challengeID, _ := cmd.Flags().GetString("challenge-id")
		PrintJSON(mustPost("/ssm/delegate", WithChallengeID(map[string]interface{}{
			"wallet_id": strings.TrimSpace(walletID),
			"chain":     strings.TrimSpace(chain),
		}, challengeID)))
	}
	return cmd
}

func ssmSignCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sign", Short: "Request transaction signature"}
	cmd.Flags().String("transaction-id", "", "Transaction intent ID to sign")
	cmd.Flags().Bool("wait", false, "Wait for WebAuthn approval and return the signed transaction status")
	cmd.Flags().Duration("poll-interval", 2*time.Second, "Polling interval when --wait is set")
	cmd.Flags().Duration("timeout", 5*time.Minute, "Maximum wait time when --wait is set")
	cmd.Flags().String("challenge-id", "", "Approved WebAuthn challenge ID to consume")
	cmd.Run = func(cmd *cobra.Command, args []string) {
		requireContainerClient()
		transactionID, _ := cmd.Flags().GetString("transaction-id")
		wait, _ := cmd.Flags().GetBool("wait")
		pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		challengeID, _ := cmd.Flags().GetString("challenge-id")

		signResp := mustPost("/ssm/sign", WithChallengeID(map[string]interface{}{
			"transaction_id": strings.TrimSpace(transactionID),
		}, challengeID))
		if wait {
			resolved, _, err := ResolveSignedResponse(signResp, SignPollOptions{Wait: true, PollInterval: pollInterval, Timeout: timeout})
			if err != nil {
				PrintErrorJSON(err, nil)
				os.Exit(1)
			}
			PrintJSON(resolved)
			return
		}
		PrintJSON(signResp)
	}
	return cmd
}
