package clientcontroller

import (
	"fmt"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/api"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/babylon"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/cosmwasm"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/generic"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/opstackl2"
	cosmwasmcfg "github.com/babylonlabs-io/finality-provider/cosmwasmclient/config"
	fpcfg "github.com/babylonlabs-io/finality-provider/finality-provider/config"
	"go.uber.org/zap"
)

const (
	BabylonConsumerChainType   = "babylon"
	OPStackL2ConsumerChainType = "OPStackL2"
	WasmConsumerChainType      = "wasm"
	GenericChainType           = "generic"
)

// NewClientController TODO: this is always going to be babylon so rename accordingly
func NewClientController(config *fpcfg.Config, logger *zap.Logger) (api.ClientController, error) {
	cc, err := babylon.NewBabylonController(config.BabylonConfig, &config.BTCNetParams, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Babylon rpc client: %w", err)
	}

	return cc, err
}

func NewConsumerController(config *fpcfg.Config, logger *zap.Logger) (api.ConsumerController, error) {
	var (
		ccc api.ConsumerController
		err error
	)

	switch config.ChainType {
	case BabylonConsumerChainType:
		ccc, err = babylon.NewBabylonConsumerController(config.BabylonConfig, &config.BTCNetParams, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Babylon rpc client: %w", err)
		}
	case OPStackL2ConsumerChainType:
		ccc, err = opstackl2.NewOPStackL2ConsumerController(config.OPStackL2Config, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create OPStack L2 consumer client: %w", err)
		}
	case WasmConsumerChainType:
		wasmEncodingCfg := cosmwasmcfg.GetWasmdEncodingConfig()
		ccc, err = cosmwasm.NewCosmwasmConsumerController(config.CosmwasmConfig, wasmEncodingCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Wasm rpc client: %w", err)
		}
	case GenericChainType:
		ccc, err = generic.NewGenericConsumerController(config.ConsumerGenericConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Storage consumer client: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported consumer chain")
	}

	return ccc, err
}
