// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package onchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/blocksecteam/web3-companion/internal/token"
)

var uniswapV2Router = map[int]string{
	1:    "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D",
	8453: "0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24",
	137:  "0xedf6066a2b290C185783862C7F4776A2C8077AD1",
}

var uniswapV2FactoryBSC = map[int]string{
	56: "0x8909Dc15e40173Ff4699343b6eB8132c65e18eC6",
}

var uniswapV3Quoter = map[int]string{
	1:    "0x61fFE014bA17989E743c5F6cB21bF9697530B21e",
	56:   "0x78D78E420Da98ad378D7799bE8f4AF69033EB077",
	8453: "0x3d4e44Eb1374240CE5F1B871ab261CD16335B76a",
	137:  "0x61fFE014bA17989E743c5F6cB21bF9697530B21e",
}

var uniswapV4Quoter = map[int]string{
	1:    "0x52F0E24D1c21C8A0cB1e5a5dD6198556BD9E1203",
	56:   "0x9F75dD27D6664c475B90e105573E550ff69437B0",
	8453: "0x0d5e0F971ED27FBfF6c2837bf31316121532048D",
	137:  "0xb3d5c3Dfc3a7aEbFF71895A7191796BFFc2c81b9",
}

var uniV3FeeTiers = []int{100, 500, 3000, 10000}

var pancakeV2Factory = map[int]string{
	1:    "0x1097053Fd2ea711dad45caCcc45EfF7548fCB362",
	56:   "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73",
	8453: "0x02a84c1b3BBD7401a5f7fa98a384EBC70bB5749E",
}

var pancakeV3Quoter = map[int]string{
	1:    "0xB048Bbc1Ee6b733FFfCFb9e9CeF7375518e25997",
	56:   "0xB048Bbc1Ee6b733FFfCFb9e9CeF7375518e25997",
	8453: "0xB048Bbc1Ee6b733FFfCFb9e9CeF7375518e25997",
}

var pancakeInfinityCLQuoter = map[int]string{
	56:   "0xd0737C9762912dD34c3271197E362Aa736Df0926",
	8453: "0xd0737C9762912dD34c3271197E362Aa736Df0926",
}

var PancakeInfinityCLPoolManager = map[int]string{
	56:   "0xa0FfB9c1CE1Fe56963B0321B32E7A0302114058b",
	8453: "0xa0FfB9c1CE1Fe56963B0321B32E7A0302114058b",
}

var pancakeFeeTiers = []int{100, 500, 2500, 10000}

type routeResult struct {
	routeKey string
	amount   *big.Int
	meta     map[string]interface{}
}

func BestUniswapRoute(ctx context.Context, rpcURL string, chainID int, tokenIn, tokenOut string, amountIn *big.Int) (string, *big.Int, map[string]interface{}, error) {
	type _unused_local_job struct {
		name string
		fn   func() (*big.Int, map[string]interface{})
	}
	var jobs []routeJob

	if router, ok := uniswapV2Router[chainID]; ok {
		jobs = append(jobs, routeJob{
			name: "v2",
			fn: func() (*big.Int, map[string]interface{}) {
				out, err := CallV2Router(ctx, rpcURL, router, tokenIn, tokenOut, amountIn)
				if err != nil || out == nil || out.Sign() <= 0 {
					return nil, nil
				}
				return out, map[string]interface{}{"version": "v2", "token_in": tokenIn, "token_out": tokenOut}
			},
		})
	}

	if quoter, ok := uniswapV3Quoter[chainID]; ok {
		for _, fee := range uniV3FeeTiers {
			fee := fee
			jobs = append(jobs, routeJob{
				name: fmt.Sprintf("v3_%d", fee),
				fn: func() (*big.Int, map[string]interface{}) {
					out, err := CallV3Quoter(ctx, rpcURL, quoter, tokenIn, tokenOut, fee, amountIn)
					if err != nil || out == nil || out.Sign() <= 0 {
						return nil, nil
					}
					return out, map[string]interface{}{"version": "v3", "fee": fee, "token_in": tokenIn, "token_out": tokenOut}
				},
			})
		}
	}

	if factory, ok := uniswapV2FactoryBSC[chainID]; ok {
		jobs = append(jobs, routeJob{
			name: "v2_reserves",
			fn: func() (*big.Int, map[string]interface{}) {
				out, err := CallV2Reserves(ctx, rpcURL, factory, tokenIn, tokenOut, amountIn, 997, 1000)
				if err != nil || out == nil || out.Sign() <= 0 {
					return nil, nil
				}
				return out, map[string]interface{}{"version": "v2", "token_in": tokenIn, "token_out": tokenOut}
			},
		})
	}

	if quoter, ok := uniswapV4Quoter[chainID]; ok {
		v4Ins := []string{tokenIn}
		v4Outs := []string{tokenOut}
		if token.IsNativeToken(tokenIn) {
			v4Ins = append(v4Ins, zeroAddr)
		}
		if token.IsNativeToken(tokenOut) {
			v4Outs = append(v4Outs, zeroAddr)
		}
		for _, ti := range v4Ins {
			for _, to := range v4Outs {
				for _, fee := range uniV3FeeTiers {
					ti, to, fee := ti, to, fee
					jobs = append(jobs, routeJob{
						name: fmt.Sprintf("v4_%d", fee),
						fn: func() (*big.Int, map[string]interface{}) {
							out, err := CallV4Quoter(ctx, rpcURL, quoter, ti, to, fee, amountIn)
							if err != nil || out == nil || out.Sign() <= 0 {
								return nil, nil
							}
							meta := buildV4Meta(ti, to, fee)
							return out, meta
						},
					})
				}
			}
		}
	}

	return runBestRoute(ctx, jobs)
}

func BestPancakeRoute(ctx context.Context, rpcURL string, chainID int, tokenIn, tokenOut string, amountIn *big.Int) (string, *big.Int, map[string]interface{}, error) {
	type _unused_local_job struct {
		name string
		fn   func() (*big.Int, map[string]interface{})
	}
	var jobs []routeJob

	if factory, ok := pancakeV2Factory[chainID]; ok {
		jobs = append(jobs, routeJob{
			name: "pancake_v2",
			fn: func() (*big.Int, map[string]interface{}) {
				out, err := CallV2Reserves(ctx, rpcURL, factory, tokenIn, tokenOut, amountIn, 9975, 10000)
				if err != nil || out == nil || out.Sign() <= 0 {
					return nil, nil
				}
				return out, map[string]interface{}{"version": "pancake_v2", "token_in": tokenIn, "token_out": tokenOut}
			},
		})
	}

	if quoter, ok := pancakeV3Quoter[chainID]; ok {
		for _, fee := range pancakeFeeTiers {
			fee := fee
			jobs = append(jobs, routeJob{
				name: fmt.Sprintf("pancake_v3_%d", fee),
				fn: func() (*big.Int, map[string]interface{}) {
					out, err := CallV3Quoter(ctx, rpcURL, quoter, tokenIn, tokenOut, fee, amountIn)
					if err != nil || out == nil || out.Sign() <= 0 {
						return nil, nil
					}
					return out, map[string]interface{}{"version": "pancake_v3", "fee": fee, "token_in": tokenIn, "token_out": tokenOut}
				},
			})
		}
	}

	if quoter, ok := pancakeInfinityCLQuoter[chainID]; ok {
		pm := PancakeInfinityCLPoolManager[chainID]
		for _, fee := range pancakeFeeTiers {
			fee := fee
			jobs = append(jobs, routeJob{
				name: fmt.Sprintf("pancake_infinity_%d", fee),
				fn: func() (*big.Int, map[string]interface{}) {
					out, err := CallPancakeInfinity(ctx, rpcURL, quoter, pm, tokenIn, tokenOut, fee, amountIn)
					if err != nil || out == nil || out.Sign() <= 0 {
						return nil, nil
					}
					meta := buildPancakeInfinityMeta(tokenIn, tokenOut, fee)
					return out, meta
				},
			})
		}
	}

	return runBestRoute(ctx, jobs)
}

type routeJob struct {
	name string
	fn   func() (*big.Int, map[string]interface{})
}

func runBestRoute(_ context.Context, jobs []routeJob) (string, *big.Int, map[string]interface{}, error) {
	type result struct {
		name   string
		amount *big.Int
		meta   map[string]interface{}
	}
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j routeJob) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[i] = result{name: j.name}
				}
			}()
			amount, meta := j.fn()
			results[i] = result{name: j.name, amount: amount, meta: meta}
		}(i, j)
	}
	wg.Wait()

	var best result
	for _, r := range results {
		if r.amount == nil || r.amount.Sign() <= 0 {
			continue
		}
		if best.amount == nil || r.amount.Cmp(best.amount) > 0 {
			best = r
		}
	}
	if best.amount == nil {
		return "", nil, nil, fmt.Errorf("no liquidity found on any route")
	}
	return best.name, best.amount, best.meta, nil
}

func buildV4Meta(tokenIn, tokenOut string, fee int) map[string]interface{} {
	inNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenIn, "0x"), 16)
	outNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenOut, "0x"), 16)
	var c0, c1 string
	var zfo bool
	if inNum.Cmp(outNum) < 0 {
		c0, c1, zfo = tokenIn, tokenOut, true
	} else {
		c0, c1, zfo = tokenOut, tokenIn, false
	}
	return map[string]interface{}{
		"version": "v4", "fee": fee,
		"v4_token_in": tokenIn, "v4_token_out": tokenOut,
		"currency0": c0, "currency1": c1,
		"zero_for_one": zfo,
		"tick_spacing": V3FeeToTickSpacing(fee),
	}
}

func buildPancakeInfinityMeta(tokenIn, tokenOut string, fee int) map[string]interface{} {
	inNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenIn, "0x"), 16)
	outNum, _ := new(big.Int).SetString(strings.TrimPrefix(tokenOut, "0x"), 16)
	var c0, c1 string
	var zfo bool
	if inNum.Cmp(outNum) < 0 {
		c0, c1, zfo = tokenIn, tokenOut, true
	} else {
		c0, c1, zfo = tokenOut, tokenIn, false
	}
	return map[string]interface{}{
		"version": "pancake_infinity_cl", "fee": fee,
		"tick_spacing": PancakeTickSpacing(fee),
		"currency0":    c0, "currency1": c1, "zero_for_one": zfo,
		"token_in": tokenIn, "token_out": tokenOut,
	}
}
