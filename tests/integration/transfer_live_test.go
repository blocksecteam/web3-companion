// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"math/big"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/erc7821"
	"github.com/blocksecteam/web3-companion/internal/swap"
	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestTransfer_Native_RPCSimulation(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	owner := liveOwner()
	recipient := "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510"

	for _, c := range []struct {
		name    string
		chainID int
	}{
		{"ethereum", 1},
		{"base", 8453},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			blockNumber := currentBlockNumber(c.name)

			rawAmount, err := token.ReadableToRaw("0.001", 18)
			if err != nil {
				t.Fatalf("ReadableToRaw: %v", err)
			}
			n := new(big.Int)
			n.SetString(rawAmount, 10)
			hexValue := "0x" + n.Text(16)

			sim := simulateRPCTrace(t, c.chainID, blockNumber, owner, recipient, "0x", hexValue)
			if !sim.Success {
				t.Errorf("%s: native transfer failed - %s", c.name, sim.ErrorInfo)
			} else {
				t.Logf("%s: native transfer ok - gasUsed=%d", c.name, sim.GasUsed)
			}
		})
	}
}

func TestTransfer_ERC20_RPCSimulation(t *testing.T) {
	if !checkIntegrationEnabled(t) {
		return
	}
	initRPCURLs()
	owner := liveOwner()
	recipient := "0x97d618E3a0AFECCFB5C50B5dF258cD7E48650510"

	resolver := swap.NewDecimalsResolver(
		swap.NewRPCDecimalsSource(chainRPCURLs, nil),
		nil,
	)

	for _, c := range []struct {
		name    string
		chainID int
		usdc    string
	}{
		{"ethereum", 1, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"},
		{"base", 8453, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			rpcURL, ok := chainRPCURLs[c.name]
			if !ok || rpcURL == "" {
				t.Skip("RPC URL not set")
			}

			bal := checkUSDCBalance(c.name, rpcURL, c.usdc, owner)
			if bal.Cmp(big.NewInt(100_000)) < 0 {
				t.Skipf("%s: insufficient USDC balance (%s)", c.name, bal)
			}

			blockNumber := currentBlockNumber(c.name)

			decimals, err := resolver.Resolve(integrationCtx(t), c.name, c.usdc)
			if err != nil {
				t.Skipf("%s: resolve decimals: %v", c.name, err)
			}

			rawAmount, err := token.ReadableToRaw("0.1", decimals)
			if err != nil {
				t.Fatalf("ReadableToRaw: %v", err)
			}
			amt := new(big.Int)
			amt.SetString(rawAmount, 10)

			calldata := token.EncodeERC20Transfer(recipient, amt)

			calls := []erc7821.HexCall{{
				To:    c.usdc,
				Value: big.NewInt(0),
				Data:  calldata,
			}}

			sim := simulateERC7821(t, c.chainID, blockNumber, owner, calls, big.NewInt(0))
			if !sim.Success {
				t.Errorf("%s: ERC-20 transfer simulation failed - %s", c.name, sim.ErrorInfo)
			} else {
				t.Logf("%s: ERC-20 transfer ok - gasUsed=%d", c.name, sim.GasUsed)
			}
		})
	}
}
