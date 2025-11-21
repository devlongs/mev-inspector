package arbitrage

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"

	"github.com/longs/mev-inspector/internal/eth"
	"github.com/longs/mev-inspector/pkg/types"
)

// WETH address on mainnet
var WETH = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")

// Detector detects arbitrage opportunities from swap events
type Detector struct {
	client *eth.Client
}

// NewDetector creates a new arbitrage detector
func NewDetector(client *eth.Client) *Detector {
	return &Detector{
		client: client,
	}
}

// DetectArbitrage analyzes swaps within a transaction to detect arbitrage
func (d *Detector) DetectArbitrage(ctx context.Context, txHash common.Hash, swaps []types.Swap) ([]types.Arbitrage, error) {
	if len(swaps) < 2 {
		return nil, nil
	}

	// Sort swaps by log index to maintain execution order
	sort.Slice(swaps, func(i, j int) bool {
		return swaps[i].LogIndex < swaps[j].LogIndex
	})

	var arbitrages []types.Arbitrage

	// Detect cyclic arbitrage (A -> B -> C -> A)
	cyclicArb := d.detectCyclicArbitrage(swaps)
	if cyclicArb != nil {
		// Fetch transaction details for gas info
		if receipt, err := d.client.TransactionReceipt(ctx, txHash); err == nil {
			cyclicArb.GasUsed = receipt.GasUsed
			if tx, _, err := d.client.GetTransaction(ctx, txHash); err == nil {
				cyclicArb.GasPrice = tx.GasPrice()
				gasCost := new(big.Int).Mul(big.NewInt(int64(cyclicArb.GasUsed)), cyclicArb.GasPrice)
				cyclicArb.NetProfitWei = new(big.Int).Sub(cyclicArb.Profit, gasCost)
			}
		}
		arbitrages = append(arbitrages, *cyclicArb)
	}

	// Detect cross-DEX arbitrage (same pair, different pools)
	crossDexArbs := d.detectCrossDEXArbitrage(swaps)
	for _, arb := range crossDexArbs {
		if receipt, err := d.client.TransactionReceipt(ctx, txHash); err == nil {
			arb.GasUsed = receipt.GasUsed
			if tx, _, err := d.client.GetTransaction(ctx, txHash); err == nil {
				arb.GasPrice = tx.GasPrice()
				gasCost := new(big.Int).Mul(big.NewInt(int64(arb.GasUsed)), arb.GasPrice)
				arb.NetProfitWei = new(big.Int).Sub(arb.Profit, gasCost)
			}
		}
		arbitrages = append(arbitrages, arb)
	}

	return arbitrages, nil
}

// detectCyclicArbitrage detects A -> B -> C -> A style arbitrage
func (d *Detector) detectCyclicArbitrage(swaps []types.Swap) *types.Arbitrage {
	if len(swaps) < 2 {
		return nil
	}

	// Build a token flow graph
	// Track: which token goes in, which comes out for each swap
	type tokenFlow struct {
		tokenIn    common.Address
		tokenOut   common.Address
		amountIn   *big.Int
		amountOut  *big.Int
	}

	flows := make([]tokenFlow, 0, len(swaps))

	for _, swap := range swaps {
		var flow tokenFlow

		// Determine token in/out based on amounts
		if swap.Amount0In.Sign() > 0 && swap.Amount1Out.Sign() > 0 {
			flow = tokenFlow{
				tokenIn:   swap.Token0,
				tokenOut:  swap.Token1,
				amountIn:  swap.Amount0In,
				amountOut: swap.Amount1Out,
			}
		} else if swap.Amount1In.Sign() > 0 && swap.Amount0Out.Sign() > 0 {
			flow = tokenFlow{
				tokenIn:   swap.Token1,
				tokenOut:  swap.Token0,
				amountIn:  swap.Amount1In,
				amountOut: swap.Amount0Out,
			}
		} else {
			// Handle V3 style where both amounts can be set
			if swap.Amount0In.Sign() > 0 {
				flow.tokenIn = swap.Token0
				flow.amountIn = swap.Amount0In
			} else if swap.Amount1In.Sign() > 0 {
				flow.tokenIn = swap.Token1
				flow.amountIn = swap.Amount1In
			}
			if swap.Amount0Out.Sign() > 0 {
				flow.tokenOut = swap.Token0
				flow.amountOut = swap.Amount0Out
			} else if swap.Amount1Out.Sign() > 0 {
				flow.tokenOut = swap.Token1
				flow.amountOut = swap.Amount1Out
			}
		}

		if flow.tokenIn != (common.Address{}) && flow.tokenOut != (common.Address{}) {
			flows = append(flows, flow)
		}
	}

	if len(flows) < 2 {
		return nil
	}

	// Check if first token in == last token out (cyclic)
	firstToken := flows[0].tokenIn
	lastToken := flows[len(flows)-1].tokenOut

	if firstToken != lastToken {
		return nil
	}

	// Check if the path is connected (output of swap N feeds into swap N+1)
	for i := 0; i < len(flows)-1; i++ {
		if flows[i].tokenOut != flows[i+1].tokenIn {
			return nil
		}
	}

	// Calculate profit
	amountIn := flows[0].amountIn
	amountOut := flows[len(flows)-1].amountOut

	if amountOut.Cmp(amountIn) <= 0 {
		return nil // No profit
	}

	profit := new(big.Int).Sub(amountOut, amountIn)

	// Get the arbitrageur (sender of first swap or transaction sender)
	arbitrageur := swaps[0].Sender

	log.Info().
		Str("txHash", swaps[0].TxHash.Hex()).
		Str("profit", profit.String()).
		Str("token", firstToken.Hex()).
		Int("numSwaps", len(swaps)).
		Msg("Detected cyclic arbitrage")

	return &types.Arbitrage{
		TxHash:      swaps[0].TxHash,
		BlockNumber: swaps[0].BlockNumber,
		Arbitrageur: arbitrageur,
		Path:        swaps,
		TokenStart:  firstToken,
		TokenEnd:    lastToken,
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		Profit:      profit,
		ProfitToken: firstToken,
	}
}

// detectCrossDEXArbitrage detects same-pair arbitrage across different pools
func (d *Detector) detectCrossDEXArbitrage(swaps []types.Swap) []types.Arbitrage {
	var arbitrages []types.Arbitrage

	// Group swaps by token pair
	type pairKey struct {
		token0 common.Address
		token1 common.Address
	}

	pairSwaps := make(map[pairKey][]types.Swap)

	for _, swap := range swaps {
		// Normalize pair (always smaller address first)
		var key pairKey
		if swap.Token0.Hex() < swap.Token1.Hex() {
			key = pairKey{token0: swap.Token0, token1: swap.Token1}
		} else {
			key = pairKey{token0: swap.Token1, token1: swap.Token0}
		}
		pairSwaps[key] = append(pairSwaps[key], swap)
	}

	// Look for pairs with swaps on different pools
	for pair, swapsForPair := range pairSwaps {
		if len(swapsForPair) < 2 {
			continue
		}

		// Check if swaps are on different pools
		pools := make(map[common.Address]bool)
		for _, swap := range swapsForPair {
			pools[swap.Pool] = true
		}

		if len(pools) < 2 {
			continue // Same pool, not cross-DEX arbitrage
		}

		// Look for buy on one pool, sell on another
		var buySwap, sellSwap *types.Swap

		for i := range swapsForPair {
			swap := &swapsForPair[i]
			// If WETH is token1 and amount1In > 0, user is buying token0 with WETH
			// If WETH is token0 and amount0In > 0, user is buying token1 with WETH
			if swap.Token0 == WETH || swap.Token1 == WETH {
				if (swap.Token0 == WETH && swap.Amount0In.Sign() > 0) ||
					(swap.Token1 == WETH && swap.Amount1In.Sign() > 0) {
					if buySwap == nil {
						buySwap = swap
					}
				} else {
					if sellSwap == nil {
						sellSwap = swap
					}
				}
			}
		}

		if buySwap != nil && sellSwap != nil && buySwap.Pool != sellSwap.Pool {
			// Calculate profit in WETH
			var wethIn, wethOut *big.Int

			if buySwap.Token0 == WETH {
				wethIn = buySwap.Amount0In
			} else {
				wethIn = buySwap.Amount1In
			}

			if sellSwap.Token0 == WETH {
				wethOut = sellSwap.Amount0Out
			} else {
				wethOut = sellSwap.Amount1Out
			}

			if wethOut.Cmp(wethIn) > 0 {
				profit := new(big.Int).Sub(wethOut, wethIn)

				log.Info().
					Str("txHash", buySwap.TxHash.Hex()).
					Str("profit", profit.String()).
					Str("pair", fmt.Sprintf("%s-%s", pair.token0.Hex()[:10], pair.token1.Hex()[:10])).
					Msg("Detected cross-DEX arbitrage")

				arbitrages = append(arbitrages, types.Arbitrage{
					TxHash:      buySwap.TxHash,
					BlockNumber: buySwap.BlockNumber,
					Arbitrageur: buySwap.Sender,
					Path:        []types.Swap{*buySwap, *sellSwap},
					TokenStart:  WETH,
					TokenEnd:    WETH,
					AmountIn:    wethIn,
					AmountOut:   wethOut,
					Profit:      profit,
					ProfitToken: WETH,
				})
			}
		}
	}

	return arbitrages
}

// IsProfitable checks if an arbitrage is profitable after gas costs
func (d *Detector) IsProfitable(arb *types.Arbitrage) bool {
	if arb.NetProfitWei == nil {
		return arb.Profit.Sign() > 0
	}
	return arb.NetProfitWei.Sign() > 0
}
