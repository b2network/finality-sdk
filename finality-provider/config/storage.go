package config

import (
	cwcfg "github.com/babylonlabs-io/finality-provider/cosmwasmclient/config"
	"go.uber.org/zap"
	"time"
)

type ConsumerGenericConfig struct {
	Namespace             string `long:"namespace"  description:"the namespace of the chain"`
	ServiceRPC            string `long:"service-rpc-address" description:"the rpc address of the service"`
	FinalityGadgetAddress string `long:"finality-gadget" description:"the contract address of the finality-gadget"`
	logger                *zap.Logger

	// Below configurations are needed for the Babylon client
	Key            string        `long:"key" description:"name of the babylon key to sign transactions with"`
	ChainID        string        `long:"chain-id" description:"chain id of the babylon chain to connect to"`
	RPCAddr        string        `long:"rpc-address" description:"address of the babylon rpc server to connect to"`
	AccountPrefix  string        `long:"acc-prefix" description:"babylon account prefix to use for addresses"`
	KeyringBackend string        `long:"keyring-type" description:"type of keyring to use"`
	GasAdjustment  float64       `long:"gas-adjustment" description:"adjustment factor when using babylon gas estimation"`
	GasPrices      string        `long:"gas-prices" description:"comma separated minimum babylon gas prices to accept for transactions"`
	KeyDirectory   string        `long:"key-dir" description:"directory to store babylon keys in"`
	Debug          bool          `long:"debug" description:"flag to print debug output"`
	Timeout        time.Duration `long:"timeout" description:"client timeout when doing queries"`
	BlockTimeout   time.Duration `long:"block-timeout" description:"block timeout when waiting for block events"`
	OutputFormat   string        `long:"output-format" description:"default output when printint responses"`
	SignModeStr    string        `long:"sign-mode" description:"sign mode to use"`
}

func (c *ConsumerGenericConfig) Validate() error {

	return nil
}

func (cfg *ConsumerGenericConfig) ToCosmwasmConfig() cwcfg.CosmwasmConfig {
	return cwcfg.CosmwasmConfig{
		Key:              cfg.Key,
		ChainID:          cfg.ChainID,
		RPCAddr:          cfg.RPCAddr,
		AccountPrefix:    cfg.AccountPrefix,
		KeyringBackend:   cfg.KeyringBackend,
		GasAdjustment:    cfg.GasAdjustment,
		GasPrices:        cfg.GasPrices,
		KeyDirectory:     cfg.KeyDirectory,
		Debug:            cfg.Debug,
		Timeout:          cfg.Timeout,
		BlockTimeout:     cfg.BlockTimeout,
		OutputFormat:     cfg.OutputFormat,
		SignModeStr:      cfg.SignModeStr,
		SubmitterAddress: "",
	}
}

func (cfg *ConsumerGenericConfig) ToBBNConfig() BBNConfig {
	return BBNConfig{
		Key:            cfg.Key,
		ChainID:        cfg.ChainID,
		RPCAddr:        cfg.RPCAddr,
		AccountPrefix:  cfg.AccountPrefix,
		KeyringBackend: cfg.KeyringBackend,
		GasAdjustment:  cfg.GasAdjustment,
		GasPrices:      cfg.GasPrices,
		KeyDirectory:   cfg.KeyDirectory,
		Debug:          cfg.Debug,
		Timeout:        cfg.Timeout,
		BlockTimeout:   cfg.BlockTimeout,
		OutputFormat:   cfg.OutputFormat,
		SignModeStr:    cfg.SignModeStr,
	}
}
