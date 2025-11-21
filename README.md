# MEV Inspector

A real-time MEV (Maximal Extractable Value) inspector for Ethereum mainnet written in Go. Detects arbitrage transactions on Uniswap V2 and V3.

## Features

- Real-time block monitoring via RPC polling
- Uniswap V2 and V3 swap event decoding
- Cyclic arbitrage detection (A -> B -> C -> A)
- Cross-DEX arbitrage detection (same pair, different pools)
- Profit calculation with gas cost analysis
- Structured logging with statistics

## Installation

```bash
git clone https://github.com/devlongs/mev-inspector.git
cd mev-inspector
go build -o bin/mev-inspector ./cmd/inspector
```

## Configuration

Create a `config.yaml` file or use environment variables:

```yaml
rpc:
  url: "https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY"
  retry_attempts: 3
  retry_delay: "1s"
  request_timeout: "30s"

inspector:
  poll_interval: "12s"
  batch_size: 10
  start_block: 0
  enable_uniswap_v2: true
  enable_uniswap_v3: true

logging:
  level: "info"
  format: "console"
```

Or use environment variables with `MEV_` prefix:

```bash
export MEV_RPC_URL="https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY"
export MEV_LOGGING_LEVEL="debug"
```

## Usage

```bash
./bin/mev-inspector
```

### Example Output

```
22:20:17 INF Detected cyclic arbitrage numSwaps=7 profit=5121895385006080 token=0xC02aaA39... txHash=0x005f9068...
22:20:17 INF ARBITRAGE DETECTED block=23850004 hops=7 profitETH=0.005122 netProfitETH=0.004990 gasUsed=441626
22:20:28 INF MEV Inspector Stats blocksProcessed=5 swapsDetected=153 arbitragesFound=8 totalProfit="0.006267 ETH"
```

## Project Structure

```
mev-inspector/
├── cmd/inspector/main.go        # Entry point
├── internal/
│   ├── config/                  # Configuration management
│   ├── eth/                     # Ethereum RPC client
│   ├── decoder/                 # Unified swap decoder
│   ├── dex/
│   │   ├── uniswapv2/           # V2 swap event decoder
│   │   └── uniswapv3/           # V3 swap event decoder
│   ├── arbitrage/               # Arbitrage detection logic
│   └── output/                  # Logging and statistics
└── pkg/types/                   # Shared types
```

## How It Works

1. Polls for new blocks at configured interval
2. Fetches swap logs from Uniswap V2 and V3 pools
3. Groups swaps by transaction
4. Analyzes token flows to detect:
   - Cyclic arbitrage: Token returns to starting point with profit
   - Cross-DEX arbitrage: Buy/sell same pair on different pools
5. Calculates gross profit and net profit (after gas)

## Requirements

- Go 1.21+
- Ethereum RPC endpoint (Alchemy, Infura, QuickNode, or own node)

## License

MIT
