package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the MEV inspector
type Config struct {
	RPC       RPCConfig
	Inspector InspectorConfig
	Logging   LoggingConfig
}

// RPCConfig holds Ethereum RPC configuration
type RPCConfig struct {
	URL            string
	WSUrl          string
	RetryAttempts  int
	RetryDelay     time.Duration
	RequestTimeout time.Duration
}

// InspectorConfig holds inspector-specific settings
type InspectorConfig struct {
	PollInterval       time.Duration
	BatchSize          int
	StartBlock         uint64
	WorkerCount        int
	EnableUniswapV2    bool
	EnableUniswapV3    bool
	OnlyProfitable     bool // Only show arbitrages with positive net profit
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string // "json" or "console"
}

// Load reads configuration from environment and config file
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("rpc.url", "https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY")
	v.SetDefault("rpc.ws_url", "")
	v.SetDefault("rpc.retry_attempts", 3)
	v.SetDefault("rpc.retry_delay", "1s")
	v.SetDefault("rpc.request_timeout", "30s")

	v.SetDefault("inspector.poll_interval", "12s")
	v.SetDefault("inspector.batch_size", 100)
	v.SetDefault("inspector.start_block", 0)
	v.SetDefault("inspector.worker_count", 4)
	v.SetDefault("inspector.enable_uniswap_v2", true)
	v.SetDefault("inspector.enable_uniswap_v3", true)
	v.SetDefault("inspector.only_profitable", false)

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")

	// Environment variable support
	v.SetEnvPrefix("MEV")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Config file support
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("$HOME/.mev-inspector")

	// Read config file (optional)
	_ = v.ReadInConfig()

	retryDelay, _ := time.ParseDuration(v.GetString("rpc.retry_delay"))
	requestTimeout, _ := time.ParseDuration(v.GetString("rpc.request_timeout"))
	pollInterval, _ := time.ParseDuration(v.GetString("inspector.poll_interval"))

	cfg := &Config{
		RPC: RPCConfig{
			URL:            v.GetString("rpc.url"),
			WSUrl:          v.GetString("rpc.ws_url"),
			RetryAttempts:  v.GetInt("rpc.retry_attempts"),
			RetryDelay:     retryDelay,
			RequestTimeout: requestTimeout,
		},
		Inspector: InspectorConfig{
			PollInterval:       pollInterval,
			BatchSize:          v.GetInt("inspector.batch_size"),
			StartBlock:         v.GetUint64("inspector.start_block"),
			WorkerCount:        v.GetInt("inspector.worker_count"),
			EnableUniswapV2:    v.GetBool("inspector.enable_uniswap_v2"),
			EnableUniswapV3:    v.GetBool("inspector.enable_uniswap_v3"),
			OnlyProfitable:     v.GetBool("inspector.only_profitable"),
		},
		Logging: LoggingConfig{
			Level:  v.GetString("logging.level"),
			Format: v.GetString("logging.format"),
		},
	}

	return cfg, nil
}
