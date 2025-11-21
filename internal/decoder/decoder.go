package decoder

import (
	"context"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog/log"

	"github.com/devlongs/mev-inspector/internal/dex/uniswapv2"
	"github.com/devlongs/mev-inspector/internal/dex/uniswapv3"
	"github.com/devlongs/mev-inspector/internal/eth"
	"github.com/devlongs/mev-inspector/pkg/types"
)

// Decoder combines multiple DEX decoders
type Decoder struct {
	v2Decoder *uniswapv2.Decoder
	v3Decoder *uniswapv3.Decoder
	enableV2  bool
	enableV3  bool
}

// NewDecoder creates a unified decoder for all supported DEXes
func NewDecoder(client *eth.Client, enableV2, enableV3 bool) *Decoder {
	var v2 *uniswapv2.Decoder
	var v3 *uniswapv3.Decoder

	if enableV2 {
		v2 = uniswapv2.NewDecoder(client)
	}
	if enableV3 {
		v3 = uniswapv3.NewDecoder(client)
	}

	return &Decoder{
		v2Decoder: v2,
		v3Decoder: v3,
		enableV2:  enableV2,
		enableV3:  enableV3,
	}
}

// GetAllSwapLogs fetches swap logs from all enabled DEXes
func (d *Decoder) GetAllSwapLogs(ctx context.Context, fromBlock, toBlock uint64) ([]ethtypes.Log, error) {
	var allLogs []ethtypes.Log

	if d.enableV2 && d.v2Decoder != nil {
		v2Logs, err := d.v2Decoder.GetSwapLogs(ctx, fromBlock, toBlock)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get V2 swap logs")
		} else {
			allLogs = append(allLogs, v2Logs...)
		}
	}

	if d.enableV3 && d.v3Decoder != nil {
		v3Logs, err := d.v3Decoder.GetSwapLogs(ctx, fromBlock, toBlock)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get V3 swap logs")
		} else {
			allLogs = append(allLogs, v3Logs...)
		}
	}

	// Sort by block number and log index
	sort.Slice(allLogs, func(i, j int) bool {
		if allLogs[i].BlockNumber != allLogs[j].BlockNumber {
			return allLogs[i].BlockNumber < allLogs[j].BlockNumber
		}
		return allLogs[i].Index < allLogs[j].Index
	})

	return allLogs, nil
}

// DecodeSwapLog decodes a swap log based on its event signature
func (d *Decoder) DecodeSwapLog(ctx context.Context, log ethtypes.Log) (*types.Swap, error) {
	// Check if it's a V2 or V3 swap based on topic
	if len(log.Topics) == 0 {
		return nil, nil
	}

	switch log.Topics[0] {
	case uniswapv2.SwapEventSignature:
		if d.v2Decoder != nil {
			return d.v2Decoder.DecodeSwapLog(ctx, log)
		}
	case uniswapv3.SwapEventSignature:
		if d.v3Decoder != nil {
			return d.v3Decoder.DecodeSwapLog(ctx, log)
		}
	}

	return nil, nil
}

// GroupSwapsByTransaction groups swap logs by their transaction hash
func (d *Decoder) GroupSwapsByTransaction(logs []ethtypes.Log) map[common.Hash][]ethtypes.Log {
	groups := make(map[common.Hash][]ethtypes.Log)

	for _, log := range logs {
		groups[log.TxHash] = append(groups[log.TxHash], log)
	}

	return groups
}

// DecodeSwapsForTransaction decodes all swap logs for a single transaction
func (d *Decoder) DecodeSwapsForTransaction(ctx context.Context, logs []ethtypes.Log) ([]types.Swap, error) {
	var swaps []types.Swap

	for _, log := range logs {
		swap, err := d.DecodeSwapLog(ctx, log)
		if err != nil {
			// Log error but continue processing other swaps
			continue
		}
		if swap != nil {
			swaps = append(swaps, *swap)
		}
	}

	// Sort by log index
	sort.Slice(swaps, func(i, j int) bool {
		return swaps[i].LogIndex < swaps[j].LogIndex
	})

	return swaps, nil
}
