package eth

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"

	"github.com/devlongs/mev-inspector/internal/config"
)

// Client wraps the Ethereum client with retry logic and convenience methods
type Client struct {
	client  *ethclient.Client
	cfg     config.RPCConfig
	chainID *big.Int
}

// NewClient creates a new Ethereum client
func NewClient(cfg config.RPCConfig) (*Client, error) {
	client, err := ethclient.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout)
	defer cancel()

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	log.Info().
		Str("url", cfg.URL).
		Str("chainID", chainID.String()).
		Msg("Connected to Ethereum node")

	return &Client{
		client:  client,
		cfg:     cfg,
		chainID: chainID,
	}, nil
}

// Close closes the client connection
func (c *Client) Close() {
	c.client.Close()
}

// ChainID returns the chain ID
func (c *Client) ChainID() *big.Int {
	return c.chainID
}

// BlockNumber returns the latest block number with retry
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	var blockNum uint64
	var err error

	for i := 0; i < c.cfg.RetryAttempts; i++ {
		blockNum, err = c.client.BlockNumber(ctx)
		if err == nil {
			return blockNum, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Failed to get block number, retrying...")
		time.Sleep(c.cfg.RetryDelay)
	}

	return 0, fmt.Errorf("failed to get block number after %d attempts: %w", c.cfg.RetryAttempts, err)
}

// BlockByNumber returns a block by number with retry
func (c *Client) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	var block *types.Block
	var err error

	for i := 0; i < c.cfg.RetryAttempts; i++ {
		block, err = c.client.BlockByNumber(ctx, number)
		if err == nil {
			return block, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Failed to get block, retrying...")
		time.Sleep(c.cfg.RetryDelay)
	}

	return nil, fmt.Errorf("failed to get block after %d attempts: %w", c.cfg.RetryAttempts, err)
}

// GetLogs fetches logs with the given filter with retry
func (c *Client) GetLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	var logs []types.Log
	var err error

	for i := 0; i < c.cfg.RetryAttempts; i++ {
		logs, err = c.client.FilterLogs(ctx, query)
		if err == nil {
			return logs, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Failed to get logs, retrying...")
		time.Sleep(c.cfg.RetryDelay)
	}

	return nil, fmt.Errorf("failed to get logs after %d attempts: %w", c.cfg.RetryAttempts, err)
}

// TransactionReceipt returns the receipt of a transaction with retry
func (c *Client) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	var receipt *types.Receipt
	var err error

	for i := 0; i < c.cfg.RetryAttempts; i++ {
		receipt, err = c.client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Failed to get receipt, retrying...")
		time.Sleep(c.cfg.RetryDelay)
	}

	return nil, fmt.Errorf("failed to get receipt after %d attempts: %w", c.cfg.RetryAttempts, err)
}

// GetTransaction returns a transaction by hash with retry
func (c *Client) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, bool, error) {
	var tx *types.Transaction
	var isPending bool
	var err error

	for i := 0; i < c.cfg.RetryAttempts; i++ {
		tx, isPending, err = c.client.TransactionByHash(ctx, txHash)
		if err == nil {
			return tx, isPending, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Failed to get transaction, retrying...")
		time.Sleep(c.cfg.RetryDelay)
	}

	return nil, false, fmt.Errorf("failed to get transaction after %d attempts: %w", c.cfg.RetryAttempts, err)
}

// CallContract executes a contract call with retry
func (c *Client) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	var result []byte
	var err error

	for i := 0; i < c.cfg.RetryAttempts; i++ {
		result, err = c.client.CallContract(ctx, msg, blockNumber)
		if err == nil {
			return result, nil
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Failed to call contract, retrying...")
		time.Sleep(c.cfg.RetryDelay)
	}

	return nil, fmt.Errorf("failed to call contract after %d attempts: %w", c.cfg.RetryAttempts, err)
}

// SubscribeNewHead subscribes to new block headers (requires WebSocket)
func (c *Client) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	return c.client.SubscribeNewHead(ctx, ch)
}
