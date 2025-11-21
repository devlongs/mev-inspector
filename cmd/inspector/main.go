package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/devlongs/mev-inspector/internal/arbitrage"
	"github.com/devlongs/mev-inspector/internal/config"
	"github.com/devlongs/mev-inspector/internal/decoder"
	"github.com/devlongs/mev-inspector/internal/eth"
	"github.com/devlongs/mev-inspector/internal/output"
	"github.com/devlongs/mev-inspector/pkg/types"
)

// Inspector is the main MEV inspection engine
type Inspector struct {
	client   *eth.Client
	decoder  *decoder.Decoder
	detector *arbitrage.Detector
	logger   *output.Logger
	cfg      *config.Config

	lastBlock uint64
	mu        sync.Mutex
}

// NewInspector creates a new MEV inspector
func NewInspector(cfg *config.Config) (*Inspector, error) {
	client, err := eth.NewClient(cfg.RPC)
	if err != nil {
		return nil, err
	}

	// Create decoder with enabled DEXes
	dec := decoder.NewDecoder(client, cfg.Inspector.EnableUniswapV2, cfg.Inspector.EnableUniswapV3)

	// Create arbitrage detector
	det := arbitrage.NewDetector(client)

	lgr := output.NewLogger(cfg.Logging)

	return &Inspector{
		client:   client,
		decoder:  dec,
		detector: det,
		logger:   lgr,
		cfg:      cfg,
	}, nil
}

// Start begins the inspection loop
func (i *Inspector) Start(ctx context.Context) error {
	log.Info().Msg("Starting MEV Inspector...")

	currentBlock, err := i.client.BlockNumber(ctx)
	if err != nil {
		return err
	}

	// Set starting block
	if i.cfg.Inspector.StartBlock > 0 {
		i.lastBlock = i.cfg.Inspector.StartBlock - 1
	} else {
		i.lastBlock = currentBlock - 1
	}

	log.Info().
		Uint64("startBlock", i.lastBlock+1).
		Uint64("currentBlock", currentBlock).
		Msg("Inspector initialized")

	// Create ticker for polling
	ticker := time.NewTicker(i.cfg.Inspector.PollInterval)
	defer ticker.Stop()

	// Stats ticker (every 30 seconds)
	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

	// Process blocks in a loop
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Shutting down inspector...")
			return ctx.Err()

		case <-statsTicker.C:
			i.logger.LogStats()

		case <-ticker.C:
			if err := i.processNewBlocks(ctx); err != nil {
				i.logger.LogError(err, "processing blocks")
			}
		}
	}
}

// processNewBlocks fetches and processes any new blocks
func (i *Inspector) processNewBlocks(ctx context.Context) error {
	currentBlock, err := i.client.BlockNumber(ctx)
	if err != nil {
		return err
	}

	i.mu.Lock()
	fromBlock := i.lastBlock + 1
	i.mu.Unlock()

	if currentBlock < fromBlock {
		return nil // No new blocks
	}

	// Process blocks in batches
	toBlock := currentBlock
	if toBlock-fromBlock > uint64(i.cfg.Inspector.BatchSize) {
		toBlock = fromBlock + uint64(i.cfg.Inspector.BatchSize) - 1
	}

	log.Debug().
		Uint64("from", fromBlock).
		Uint64("to", toBlock).
		Msg("Processing block range")

	if err := i.processBlockRange(ctx, fromBlock, toBlock); err != nil {
		return err
	}

	i.mu.Lock()
	i.lastBlock = toBlock
	i.mu.Unlock()

	return nil
}

// processBlockRange processes a range of blocks
func (i *Inspector) processBlockRange(ctx context.Context, fromBlock, toBlock uint64) error {
	startTime := time.Now()

	// Fetch all swap logs in the range
	logs, err := i.decoder.GetAllSwapLogs(ctx, fromBlock, toBlock)
	if err != nil {
		return err
	}

	if len(logs) == 0 {
		for block := fromBlock; block <= toBlock; block++ {
			i.logger.LogBlockComplete(block, 0, 0, time.Since(startTime))
		}
		return nil
	}

	// Group logs by transaction
	txLogs := i.decoder.GroupSwapsByTransaction(logs)

	totalSwaps := 0
	totalArbitrages := 0

	// Process each transaction
	for txHash, txSwapLogs := range txLogs {
		swaps, err := i.decoder.DecodeSwapsForTransaction(ctx, txSwapLogs)
		if err != nil {
			i.logger.LogError(err, "decoding swaps")
			continue
		}

		totalSwaps += len(swaps)

		// Log individual swaps at debug level
		for _, swap := range swaps {
			i.logger.LogSwap(&swap)
		}

		// Detect arbitrage
		arbitrages, err := i.detector.DetectArbitrage(ctx, txHash, swaps)
		if err != nil {
			i.logger.LogError(err, "detecting arbitrage")
			continue
		}

		for _, arb := range arbitrages {
			i.logger.LogArbitrage(&arb)
			totalArbitrages++
		}
	}

	// Log completion for each block in range
	duration := time.Since(startTime)
	blocksProcessed := toBlock - fromBlock + 1
	avgSwapsPerBlock := totalSwaps / int(blocksProcessed)
	avgArbsPerBlock := totalArbitrages / int(blocksProcessed)

	for block := fromBlock; block <= toBlock; block++ {
		i.logger.LogBlockComplete(block, avgSwapsPerBlock, avgArbsPerBlock, duration/time.Duration(blocksProcessed))
	}

	return nil
}

// ProcessSingleBlock processes a single block (useful for testing)
func (i *Inspector) ProcessSingleBlock(ctx context.Context, blockNumber uint64) ([]types.Arbitrage, error) {
	logs, err := i.decoder.GetAllSwapLogs(ctx, blockNumber, blockNumber)
	if err != nil {
		return nil, err
	}

	var allArbitrages []types.Arbitrage

	txLogs := i.decoder.GroupSwapsByTransaction(logs)
	for txHash, txSwapLogs := range txLogs {
		swaps, err := i.decoder.DecodeSwapsForTransaction(ctx, txSwapLogs)
		if err != nil {
			continue
		}

		arbitrages, err := i.detector.DetectArbitrage(ctx, txHash, swaps)
		if err != nil {
			continue
		}

		allArbitrages = append(allArbitrages, arbitrages...)
	}

	return allArbitrages, nil
}

// Close shuts down the inspector
func (i *Inspector) Close() {
	i.client.Close()
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Create inspector
	inspector, err := NewInspector(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create inspector")
	}
	defer inspector.Close()

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	// Start the inspector
	if err := inspector.Start(ctx); err != nil && err != context.Canceled {
		log.Fatal().Err(err).Msg("Inspector error")
	}

	log.Info().Msg("MEV Inspector stopped")
}
