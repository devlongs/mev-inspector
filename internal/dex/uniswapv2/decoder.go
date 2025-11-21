package uniswapv2

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

// Uniswap V2 Swap event signature
// event Swap(address indexed sender, uint amount0In, uint amount1In, uint amount0Out, uint amount1Out, address indexed to)
var SwapEventSignature = common.HexToHash("0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822")

// Sync event signature for reserve updates
// event Sync(uint112 reserve0, uint112 reserve1)
var SyncEventSignature = common.HexToHash("0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1")

// Common Uniswap V2 factory addresses
var (
	UniswapV2Factory   = common.HexToAddress("0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f")
	SushiswapFactory   = common.HexToAddress("0xC0AEe478e3658e2610c5F7A4A2E1777cE9e4f2Ac")
)

// Decoder decodes Uniswap V2 swap events
type Decoder struct {
	client    *eth.Client
	poolCache map[common.Address]*PoolInfo
}

// PoolInfo holds cached information about a V2 pool
type PoolInfo struct {
	Token0   common.Address
	Token1   common.Address
	Reserve0 *big.Int
	Reserve1 *big.Int
}

// NewDecoder creates a new Uniswap V2 decoder
func NewDecoder(client *eth.Client) *Decoder {
	return &Decoder{
		client:    client,
		poolCache: make(map[common.Address]*PoolInfo),
	}
}

// GetSwapLogs fetches all Uniswap V2 swap logs in a block range
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

// DecodeSwapLog decodes a single swap log into a Swap struct
func (d *Decoder) DecodeSwapLog(ctx context.Context, log ethtypes.Log) (*types.Swap, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("invalid swap log: expected 3 topics, got %d", len(log.Topics))
	}

	if log.Topics[0] != SwapEventSignature {
		return nil, fmt.Errorf("not a Uniswap V2 swap event")
	}

	// Decode indexed parameters from topics
	sender := common.HexToAddress(log.Topics[1].Hex())
	recipient := common.HexToAddress(log.Topics[2].Hex())

	// Decode non-indexed parameters from data
	if len(log.Data) < 128 {
		return nil, fmt.Errorf("invalid swap log data length: expected 128 bytes, got %d", len(log.Data))
	}

	amount0In := new(big.Int).SetBytes(log.Data[0:32])
	amount1In := new(big.Int).SetBytes(log.Data[32:64])
	amount0Out := new(big.Int).SetBytes(log.Data[64:96])
	amount1Out := new(big.Int).SetBytes(log.Data[96:128])

	// Get pool info (token0, token1)
	poolInfo, err := d.getPoolInfo(ctx, log.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool info: %w", err)
	}

	return &types.Swap{
		TxHash:      log.TxHash,
		BlockNumber: log.BlockNumber,
		LogIndex:    log.Index,
		Pool:        log.Address,
		Protocol:    "uniswap_v2",
		Sender:      sender,
		Recipient:   recipient,
		Token0:      poolInfo.Token0,
		Token1:      poolInfo.Token1,
		Amount0In:   amount0In,
		Amount1In:   amount1In,
		Amount0Out:  amount0Out,
		Amount1Out:  amount1Out,
	}, nil
}

// getPoolInfo fetches and caches pool information
func (d *Decoder) getPoolInfo(ctx context.Context, poolAddress common.Address) (*PoolInfo, error) {
	// Check cache first
	if info, ok := d.poolCache[poolAddress]; ok {
		return info, nil
	}

	// Fetch token0 and token1 from the pool contract
	token0, err := d.callToken0(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get token0: %w", err)
	}

	token1, err := d.callToken1(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get token1: %w", err)
	}

	info := &PoolInfo{
		Token0: token0,
		Token1: token1,
	}

	// Cache the result
	d.poolCache[poolAddress] = info

	log.Debug().
		Str("pool", poolAddress.Hex()).
		Str("token0", token0.Hex()).
		Str("token1", token1.Hex()).
		Msg("Cached V2 pool info")

	return info, nil
}

// callToken0 calls the token0() function on a V2 pair contract
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

// callToken1 calls the token1() function on a V2 pair contract
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

// GetReserves fetches current reserves from a V2 pool
func (d *Decoder) GetReserves(ctx context.Context, poolAddress common.Address) (*big.Int, *big.Int, error) {
	// getReserves() selector: 0x0902f1ac
	data := common.Hex2Bytes("0902f1ac")

	msg := ethereum.CallMsg{
		To:   &poolAddress,
		Data: data,
	}

	result, err := d.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, nil, err
	}

	if len(result) < 64 {
		return nil, nil, fmt.Errorf("invalid getReserves response")
	}

	reserve0 := new(big.Int).SetBytes(result[0:32])
	reserve1 := new(big.Int).SetBytes(result[32:64])

	return reserve0, reserve1, nil
}

// IsV2Pool checks if an address is likely a Uniswap V2 pool
func (d *Decoder) IsV2Pool(ctx context.Context, address common.Address) bool {
	_, err := d.callToken0(ctx, address)
	if err != nil {
		return false
	}
	_, err = d.callToken1(ctx, address)
	return err == nil
}

// ParseSwapABI returns the ABI for parsing swap events
func ParseSwapABI() (abi.Event, error) {
	const swapEventABI = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"sender","type":"address"},{"indexed":false,"name":"amount0In","type":"uint256"},{"indexed":false,"name":"amount1In","type":"uint256"},{"indexed":false,"name":"amount0Out","type":"uint256"},{"indexed":false,"name":"amount1Out","type":"uint256"},{"indexed":true,"name":"to","type":"address"}],"name":"Swap","type":"event"}]`

	parsed, err := abi.JSON(strings.NewReader(swapEventABI))
	if err != nil {
		return abi.Event{}, err
	}

	return parsed.Events["Swap"], nil
}
