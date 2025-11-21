package output

import (
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/devlongs/mev-inspector/internal/config"
	"github.com/devlongs/mev-inspector/pkg/types"
)

// Logger handles output formatting for detected MEV
type Logger struct {
	stats *Stats
}

// Stats tracks MEV detection statistics
type Stats struct {
	BlocksProcessed uint64
	SwapsDetected   uint64
	ArbitragesFound uint64
	TotalProfitWei  *big.Int
	TotalNetProfit  *big.Int
	StartTime       time.Time
}

// NewLogger creates a new MEV logger
func NewLogger(cfg config.LoggingConfig) *Logger {
	// Configure zerolog
	switch cfg.Format {
	case "json":
		// Default JSON output
	case "console":
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
		})
	}

	// Set log level
	switch cfg.Level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	}

	return &Logger{
		stats: &Stats{
			TotalProfitWei: big.NewInt(0),
			TotalNetProfit: big.NewInt(0),
			StartTime:      time.Now(),
		},
	}
}

// LogBlockStart logs the start of block processing
func (l *Logger) LogBlockStart(blockNumber uint64, txCount int) {
	log.Debug().
		Uint64("block", blockNumber).
		Int("txCount", txCount).
		Msg("Processing block")
}

// LogBlockComplete logs completion of block processing
func (l *Logger) LogBlockComplete(blockNumber uint64, swaps int, arbs int, duration time.Duration) {
	l.stats.BlocksProcessed++
	l.stats.SwapsDetected += uint64(swaps)
	l.stats.ArbitragesFound += uint64(arbs)

	log.Info().
		Uint64("block", blockNumber).
		Int("swaps", swaps).
		Int("arbitrages", arbs).
		Dur("duration", duration).
		Msg("Block processed")
}

// LogArbitrage logs a detected arbitrage
func (l *Logger) LogArbitrage(arb *types.Arbitrage) {
	profitETH := weiToEther(arb.Profit)
	netProfitETH := "N/A"
	if arb.NetProfitWei != nil {
		netProfitETH = weiToEther(arb.NetProfitWei)
		l.stats.TotalNetProfit.Add(l.stats.TotalNetProfit, arb.NetProfitWei)
	}
	l.stats.TotalProfitWei.Add(l.stats.TotalProfitWei, arb.Profit)

	// Build path string
	path := buildPathString(arb.Path)

	log.Info().
		Str("txHash", arb.TxHash.Hex()).
		Uint64("block", arb.BlockNumber).
		Str("arbitrageur", arb.Arbitrageur.Hex()).
		Str("profitETH", profitETH).
		Str("netProfitETH", netProfitETH).
		Uint64("gasUsed", arb.GasUsed).
		Str("path", path).
		Int("hops", len(arb.Path)).
		Msg("ARBITRAGE DETECTED")
}

// LogSwap logs a single swap event (debug level)
func (l *Logger) LogSwap(swap *types.Swap) {
	log.Debug().
		Str("txHash", swap.TxHash.Hex()).
		Str("pool", swap.Pool.Hex()).
		Str("protocol", swap.Protocol).
		Str("token0", swap.Token0.Hex()).
		Str("token1", swap.Token1.Hex()).
		Msg("Swap detected")
}

// LogStats logs current statistics
func (l *Logger) LogStats() {
	elapsed := time.Since(l.stats.StartTime)
	blocksPerSec := float64(l.stats.BlocksProcessed) / elapsed.Seconds()

	log.Info().
		Uint64("blocksProcessed", l.stats.BlocksProcessed).
		Uint64("swapsDetected", l.stats.SwapsDetected).
		Uint64("arbitragesFound", l.stats.ArbitragesFound).
		Str("totalProfit", weiToEther(l.stats.TotalProfitWei)+" ETH").
		Str("totalNetProfit", weiToEther(l.stats.TotalNetProfit)+" ETH").
		Float64("blocksPerSec", blocksPerSec).
		Dur("uptime", elapsed).
		Msg("MEV Inspector Stats")
}

// LogError logs an error
func (l *Logger) LogError(err error, context string) {
	log.Error().
		Err(err).
		Str("context", context).
		Msg("Error occurred")
}

// GetStats returns current statistics
func (l *Logger) GetStats() *Stats {
	return l.stats
}

// weiToEther converts wei to ether string with 6 decimal places
func weiToEther(wei *big.Int) string {
	if wei == nil {
		return "0"
	}

	// 1 ETH = 10^18 wei
	ether := new(big.Float).SetInt(wei)
	divisor := new(big.Float).SetInt(big.NewInt(1e18))
	ether.Quo(ether, divisor)

	return fmt.Sprintf("%.6f", ether)
}

// buildPathString creates a human-readable path string showing token flow
func buildPathString(swaps []types.Swap) string {
	if len(swaps) == 0 {
		return ""
	}

	// Determine the input token for the first swap
	var path string
	for i, swap := range swaps {
		var tokenIn, tokenOut string

		// Determine token in/out based on amounts
		if swap.Amount0In != nil && swap.Amount0In.Sign() > 0 {
			tokenIn = swap.Token0.Hex()[:10]
			tokenOut = swap.Token1.Hex()[:10]
		} else if swap.Amount1In != nil && swap.Amount1In.Sign() > 0 {
			tokenIn = swap.Token1.Hex()[:10]
			tokenOut = swap.Token0.Hex()[:10]
		} else {
			// Fallback: check output amounts
			if swap.Amount0Out != nil && swap.Amount0Out.Sign() > 0 {
				tokenIn = swap.Token1.Hex()[:10]
				tokenOut = swap.Token0.Hex()[:10]
			} else {
				tokenIn = swap.Token0.Hex()[:10]
				tokenOut = swap.Token1.Hex()[:10]
			}
		}

		if i == 0 {
			path = tokenIn + " -> " + tokenOut
		} else {
			path += " -> " + tokenOut
		}
	}

	return path
}
