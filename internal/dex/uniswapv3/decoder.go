package uniswapv3

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog/log"

	"github.com/devlongs/mev-inspector/internal/eth"
	"github.com/devlongs/mev-inspector/pkg/types"
)

// Uniswap V3 Swap event signature
// event Swap(address indexed sender, address indexed recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
var SwapEventSignature = common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67")

// Common Uniswap V3 factory address
var UniswapV3Factory = common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984")

// Decoder decodes Uniswap V3 swap events
type Decoder struct {
	client    *eth.Client
	poolCache map[common.Address]*PoolInfo
}

// PoolInfo holds cached information about a V3 pool
type PoolInfo struct {
	Token0 common.Address
	Token1 common.Address
	Fee    uint32
}

// NewDecoder creates a new Uniswap V3 decoder
func NewDecoder(client *eth.Client) *Decoder {
	return &Decoder{
		client:    client,
		poolCache: make(map[common.Address]*PoolInfo),
	}
}

// GetSwapLogs fetches all Uniswap V3 swap logs in a block range
func (d *Decoder) GetSwapLogs(ctx context.Context, fromBlock, toBlock uint64) ([]ethtypes.Log, error) {
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Topics: [][]common.Hash{
			{SwapEventSignature},
		},
	}

	return d.client.GetLogs(ctx, query)
}

// DecodeSwapLog decodes a single V3 swap log into a Swap struct
func (d *Decoder) DecodeSwapLog(ctx context.Context, log ethtypes.Log) (*types.Swap, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("invalid swap log: expected 3 topics, got %d", len(log.Topics))
	}

	if log.Topics[0] != SwapEventSignature {
		return nil, fmt.Errorf("not a Uniswap V3 swap event")
	}

	// Decode indexed parameters from topics
	sender := common.HexToAddress(log.Topics[1].Hex())
	recipient := common.HexToAddress(log.Topics[2].Hex())

	// Decode non-indexed parameters from data
	// amount0 (int256), amount1 (int256), sqrtPriceX96 (uint160), liquidity (uint128), tick (int24)
	if len(log.Data) < 160 {
		return nil, fmt.Errorf("invalid swap log data length: expected 160 bytes, got %d", len(log.Data))
	}

	// amount0 and amount1 are signed integers
	amount0 := new(big.Int).SetBytes(log.Data[0:32])
	if log.Data[0]&0x80 != 0 {
		// Negative number - convert from two's complement
		amount0.Sub(amount0, new(big.Int).Lsh(big.NewInt(1), 256))
	}

	amount1 := new(big.Int).SetBytes(log.Data[32:64])
	if log.Data[32]&0x80 != 0 {
		amount1.Sub(amount1, new(big.Int).Lsh(big.NewInt(1), 256))
	}

	sqrtPriceX96 := new(big.Int).SetBytes(log.Data[64:96])
	liquidity := new(big.Int).SetBytes(log.Data[96:128])
	tick := new(big.Int).SetBytes(log.Data[128:160])
	if log.Data[128]&0x80 != 0 {
		tick.Sub(tick, new(big.Int).Lsh(big.NewInt(1), 256))
	}

	// Get pool info (token0, token1, fee)
	poolInfo, err := d.getPoolInfo(ctx, log.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool info: %w", err)
	}

	// Convert V3 amounts to V2-style (separate in/out amounts)
	var amount0In, amount1In, amount0Out, amount1Out *big.Int

	if amount0.Sign() > 0 {
		// Token0 went into the pool (user sold token0)
		amount0In = new(big.Int).Set(amount0)
		amount0Out = big.NewInt(0)
	} else {
		// Token0 came out of the pool (user bought token0)
		amount0In = big.NewInt(0)
		amount0Out = new(big.Int).Neg(amount0)
	}

	if amount1.Sign() > 0 {
		// Token1 went into the pool
		amount1In = new(big.Int).Set(amount1)
		amount1Out = big.NewInt(0)
	} else {
		// Token1 came out of the pool
		amount1In = big.NewInt(0)
		amount1Out = new(big.Int).Neg(amount1)
	}

	return &types.Swap{
		TxHash:       log.TxHash,
		BlockNumber:  log.BlockNumber,
		LogIndex:     log.Index,
		Pool:         log.Address,
		Protocol:     "uniswap_v3",
		Sender:       sender,
		Recipient:    recipient,
		Token0:       poolInfo.Token0,
		Token1:       poolInfo.Token1,
		Amount0In:    amount0In,
		Amount1In:    amount1In,
		Amount0Out:   amount0Out,
		Amount1Out:   amount1Out,
		SqrtPriceX96: sqrtPriceX96,
		Liquidity:    liquidity,
		Tick:         tick,
	}, nil
}

// getPoolInfo fetches and caches pool information
func (d *Decoder) getPoolInfo(ctx context.Context, poolAddress common.Address) (*PoolInfo, error) {
	// Check cache first
	if info, ok := d.poolCache[poolAddress]; ok {
		return info, nil
	}

	// Fetch token0, token1, and fee from the pool contract
	token0, err := d.callToken0(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get token0: %w", err)
	}

	token1, err := d.callToken1(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get token1: %w", err)
	}

	fee, err := d.callFee(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get fee: %w", err)
	}

	info := &PoolInfo{
		Token0: token0,
		Token1: token1,
		Fee:    fee,
	}

	// Cache the result
	d.poolCache[poolAddress] = info

	log.Debug().
		Str("pool", poolAddress.Hex()).
		Str("token0", token0.Hex()).
		Str("token1", token1.Hex()).
		Uint32("fee", fee).
		Msg("Cached V3 pool info")

	return info, nil
}

// callToken0 calls the token0() function on a V3 pool contract
func (d *Decoder) callToken0(ctx context.Context, poolAddress common.Address) (common.Address, error) {
	// token0() selector: 0x0dfe1681
	data := common.Hex2Bytes("0dfe1681")

	msg := ethereum.CallMsg{
		To:   &poolAddress,
		Data: data,
	}

	result, err := d.client.CallContract(ctx, msg, nil)
	if err != nil {
		return common.Address{}, err
	}

	if len(result) < 32 {
		return common.Address{}, fmt.Errorf("invalid token0 response")
	}

	return common.BytesToAddress(result[12:32]), nil
}

// callToken1 calls the token1() function on a V3 pool contract
func (d *Decoder) callToken1(ctx context.Context, poolAddress common.Address) (common.Address, error) {
	// token1() selector: 0xd21220a7
	data := common.Hex2Bytes("d21220a7")

	msg := ethereum.CallMsg{
		To:   &poolAddress,
		Data: data,
	}

	result, err := d.client.CallContract(ctx, msg, nil)
	if err != nil {
		return common.Address{}, err
	}

	if len(result) < 32 {
		return common.Address{}, fmt.Errorf("invalid token1 response")
	}

	return common.BytesToAddress(result[12:32]), nil
}

// callFee calls the fee() function on a V3 pool contract
func (d *Decoder) callFee(ctx context.Context, poolAddress common.Address) (uint32, error) {
	// fee() selector: 0xddca3f43
	data := common.Hex2Bytes("ddca3f43")

	msg := ethereum.CallMsg{
		To:   &poolAddress,
		Data: data,
	}

	result, err := d.client.CallContract(ctx, msg, nil)
	if err != nil {
		return 0, err
	}

	if len(result) < 32 {
		return 0, fmt.Errorf("invalid fee response")
	}

	fee := new(big.Int).SetBytes(result)
	return uint32(fee.Uint64()), nil
}

// IsV3Pool checks if an address is likely a Uniswap V3 pool
func (d *Decoder) IsV3Pool(ctx context.Context, address common.Address) bool {
	_, err := d.callFee(ctx, address)
	return err == nil
}

// ParseSwapABI returns the ABI for parsing swap events
func ParseSwapABI() (abi.Event, error) {
	const swapEventABI = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"sender","type":"address"},{"indexed":true,"name":"recipient","type":"address"},{"indexed":false,"name":"amount0","type":"int256"},{"indexed":false,"name":"amount1","type":"int256"},{"indexed":false,"name":"sqrtPriceX96","type":"uint160"},{"indexed":false,"name":"liquidity","type":"uint128"},{"indexed":false,"name":"tick","type":"int24"}],"name":"Swap","type":"event"}]`

	parsed, err := abi.JSON(strings.NewReader(swapEventABI))
	if err != nil {
		return abi.Event{}, err
	}

	return parsed.Events["Swap"], nil
}
