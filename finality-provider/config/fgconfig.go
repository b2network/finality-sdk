package config

import "time"

type FGConfig struct {
	BitcoinRPCHost    string        `long:"bitcoin-rpc-host" description:"rpc host address of the bitcoin node"`
	BitcoinRPCUser    string        `long:"bitcoin-rpc-user" description:"rpc user of the bitcoin node"`
	BitcoinRPCPass    string        `long:"bitcoin-rpc-pass" description:"rpc password of the bitcoin node"`
	FGContractAddress string        `long:"fg-contract-address" description:"BabylonChain op finality gadget contract address"`
	BBNChainID        string        `long:"bbn-chain-id" description:"BabylonChain chain ID"`
	BBNRPCAddress     string        `long:"bbn-rpc-address" description:"BabylonChain chain RPC address"`
	BitcoinDisableTLS bool          `long:"bitcoin-disable-tls" description:"disable TLS for RPC connections"`
	PollInterval      time.Duration `long:"retry-interval" description:"interval in seconds to recheck Babylon finality of block"`
}
