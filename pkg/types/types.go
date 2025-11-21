package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Token represents an ERC20 token
type Token struct {
	Address  common.Address
	Symbol   string
	Decimals uint8
}

// Pool represents a DEX liquidity pool
type Pool struct {
	Address  common.Address
	Token0   Token
	Token1   Token
	Protocol string // "uniswap_v2" or "uniswap_v3"
	Fee      uint32 // V3 fee tier (500, 3000, 10000)
}

// Swap represents a single swap event
type Swap struct {
	TxHash      common.Hash
	BlockNumber uint64
	LogIndex    uint
	Pool        common.Address
	Protocol    string
	Sender      common.Address
	Recipient   common.Address
	Token0      common.Address
	Token1      common.Address
	Amount0In   *big.Int
	Amount1In   *big.Int
	Amount0Out  *big.Int
	Amount1Out  *big.Int
	// V3 specific
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         *big.Int
}

// SwapPath represents a sequence of swaps in a single transaction
type SwapPath struct {
	Swaps []Swap
}

// Arbitrage represents a detected arbitrage opportunity
type Arbitrage struct {
	TxHash       common.Hash
	BlockNumber  uint64
	Arbitrageur  common.Address
	Path         []Swap
	TokenStart   common.Address
	TokenEnd     common.Address
	AmountIn     *big.Int
	AmountOut    *big.Int
	Profit       *big.Int
	ProfitToken  common.Address
	GasUsed      uint64
	GasPrice     *big.Int
	NetProfitWei *big.Int
}

// ArbitrageType indicates the type of arbitrage detected
type ArbitrageType string

const (
	ArbitrageTypeCyclic   ArbitrageType = "cyclic"   // A -> B -> C -> A
	ArbitrageTypeCrossDEX ArbitrageType = "cross_dex" // Same pair, different pools
)
