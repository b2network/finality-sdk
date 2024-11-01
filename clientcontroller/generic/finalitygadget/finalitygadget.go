package finalitygadget

import (
	"context"
	"errors"
	"fmt"
	"github.com/babylonlabs-io/finality-gadget/btcclient"
	"github.com/babylonlabs-io/finality-gadget/cwclient"
	"github.com/babylonlabs-io/finality-gadget/db"
	"github.com/babylonlabs-io/finality-gadget/finalitygadget"
	"github.com/babylonlabs-io/finality-gadget/testutil/mocks"
	"github.com/babylonlabs-io/finality-provider/finality-provider/config"
	"math"
	"time"

	bbnclient "github.com/babylonlabs-io/babylon/client/client"
	bbncfg "github.com/babylonlabs-io/babylon/client/config"
	fgbbnclient "github.com/babylonlabs-io/finality-gadget/bbnclient"
	"github.com/babylonlabs-io/finality-gadget/types"
	"go.uber.org/zap"
	"strings"
)

var _ IFinalityGadgetCustom = &FinalityGadgetCustom{}

type FinalityGadgetCustom struct {
	btcClient    finalitygadget.IBitcoinClient
	cwClient     finalitygadget.ICosmWasmClient
	bbnClient    finalitygadget.IBabylonClient
	db           db.IDatabaseHandler
	pollInterval time.Duration
	logger       *zap.Logger
}

func NewFinalityGadgetCustom(cfg *config.FGConfig, db db.IDatabaseHandler, logger *zap.Logger) (*FinalityGadgetCustom, error) {
	// Create babylon client
	bbnConfig := bbncfg.DefaultBabylonConfig()
	bbnConfig.RPCAddr = cfg.BBNRPCAddress
	bbnConfig.ChainID = cfg.BBNChainID
	babylonClient, err := bbnclient.New(
		&bbnConfig,
		logger,
	)
	bbnClient := fgbbnclient.NewBabylonClient(babylonClient.QueryClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create Babylon client: %w", err)
	}

	// Create bitcoin client
	btcConfig := btcclient.DefaultBTCConfig()
	btcConfig.RPCHost = cfg.BitcoinRPCHost
	if cfg.BitcoinRPCUser != "" && cfg.BitcoinRPCPass != "" {
		btcConfig.RPCUser = cfg.BitcoinRPCUser
		btcConfig.RPCPass = cfg.BitcoinRPCPass
	}
	if cfg.BitcoinDisableTLS {
		btcConfig.DisableTLS = true
	}
	var btcClient finalitygadget.IBitcoinClient
	switch cfg.BitcoinRPCHost {
	case "mock-btc-client":
		btcClient, err = mocks.NewMockBitcoinClient(btcConfig, logger)
	default:
		btcClient, err = btcclient.NewBitcoinClient(btcConfig, logger)
	}
	if err != nil {
		return nil, err
	}

	// Create cosmwasm client
	cwClient := cwclient.NewCosmWasmClient(babylonClient.QueryClient.RPCClient, cfg.FGContractAddress)

	// Create finality gadget
	return &FinalityGadgetCustom{
		btcClient:    btcClient,
		bbnClient:    bbnClient,
		cwClient:     cwClient,
		db:           db,
		pollInterval: cfg.PollInterval,
		logger:       logger,
	}, nil
}

//////////////////////////////
// METHODS
//////////////////////////////

/* QueryIsBlockBabylonFinalized checks if the given L2 block is finalized by the Babylon finality gadget
 *
 * - if the finality gadget is not enabled, always return true
 * - else, check if the given L2 block is finalized
 * - return true if finalized, false if not finalized, and error if any
 *
 * - to check if the block is finalized, we need to:
 *   - get the consumer chain id
 *   - get all the FPs pubkey for the consumer chain
 *   - convert the L2 block timestamp to BTC height
 *   - get all FPs voting power at this BTC height
 *   - calculate total voting power
 *   - get all FPs that voted this L2 block with the same height and hash
 *   - calculate voted voting power
 *   - check if the voted voting power is more than 2/3 of the total voting power
 */
func (fg *FinalityGadgetCustom) QueryIsBlockBabylonFinalized(block *types.Block) (bool, error) {
	// check if the finality gadget is enabled
	// if not, always return true to pass through op derivation pipeline
	isEnabled, err := fg.cwClient.QueryIsEnabled()
	if err != nil {
		return false, err
	}
	if !isEnabled {
		return true, nil
	}

	// trim prefix 0x for the L2 block hash
	block.BlockHash = strings.TrimPrefix(block.BlockHash, "0x")

	// get all FPs pubkey for the consumer chain
	allFpPks, err := fg.queryAllFpBtcPubKeys()
	if err != nil {
		return false, err
	}

	// convert the L2 timestamp to BTC height
	btcblockHeight, err := fg.btcClient.GetBlockHeightByTimestamp(block.BlockTimestamp)
	if err != nil {
		return false, err
	}

	// check whether the btc staking is actived
	earliestDelHeight, err := fg.bbnClient.QueryEarliestActiveDelBtcHeight(allFpPks)
	if err != nil {
		return false, err
	}
	if btcblockHeight < earliestDelHeight {
		return false, types.ErrBtcStakingNotActivated
	}

	// get all FPs voting power at this BTC height
	allFpPower, err := fg.bbnClient.QueryMultiFpPower(allFpPks, btcblockHeight)
	if err != nil {
		return false, err
	}

	// calculate total voting power
	var totalPower uint64 = 0
	for _, power := range allFpPower {
		totalPower += power
	}

	// no FP has voting power for the consumer chain
	if totalPower == 0 {
		return false, types.ErrNoFpHasVotingPower
	}

	// get all FPs that voted this (L2 block height, L2 block hash) combination
	votedFpPks, err := fg.cwClient.QueryListOfVotedFinalityProviders(block)
	if err != nil {
		return false, err
	}
	if votedFpPks == nil {
		return false, nil
	}
	// calculate voted voting power
	var votedPower uint64 = 0
	for _, key := range votedFpPks {
		if power, exists := allFpPower[key]; exists {
			votedPower += power
		}
	}

	// quorom < 2/3
	if votedPower*3 < totalPower*2 {
		return false, nil
	}
	return true, nil
}

// QueryBtcStakingActivatedTimestamp retrieves BTC staking activation timestamp from the database
// returns math.MaxUint64, error if any error occurs
func (fg *FinalityGadgetCustom) QueryBtcStakingActivatedTimestamp() (uint64, error) {
	// First, try to get the timestamp from the database
	timestamp, err := fg.db.GetActivatedTimestamp()
	if err != nil {
		// If error is not found, try to query it from the bbnClient
		if errors.Is(err, types.ErrActivatedTimestampNotFound) {
			fg.logger.Debug("activation timestamp hasn't been set yet, querying from bbnClient...")
			return fg.queryBtcStakingActivationTimestamp()
		}
		fg.logger.Error("Failed to get activated timestamp from database", zap.Error(err))
		return math.MaxUint64, err
	}
	fg.logger.Debug("BTC staking activated timestamp found in database", zap.Uint64("timestamp", timestamp))
	return timestamp, nil
}

// Query the BTC staking activation timestamp from bbnClient
// returns math.MaxUint64, ErrBtcStakingNotActivated if the BTC staking is not activated
func (fg *FinalityGadgetCustom) queryBtcStakingActivationTimestamp() (uint64, error) {
	allFpPks, err := fg.queryAllFpBtcPubKeys()
	if err != nil {
		return math.MaxUint64, err
	}
	fg.logger.Debug("All consumer FP public keys", zap.Strings("allFpPks", allFpPks))

	earliestDelHeight, err := fg.bbnClient.QueryEarliestActiveDelBtcHeight(allFpPks)
	if err != nil {
		return math.MaxUint64, err
	}
	if earliestDelHeight == math.MaxUint64 {
		return math.MaxUint64, types.ErrBtcStakingNotActivated
	}
	fg.logger.Debug("Earliest active delegation height", zap.Uint64("height", earliestDelHeight))

	btcBlockTimestamp, err := fg.btcClient.GetBlockTimestampByHeight(earliestDelHeight)
	if err != nil {
		return math.MaxUint64, err
	}
	fg.logger.Debug("BTC staking activated at", zap.Uint64("timestamp", btcBlockTimestamp))

	return btcBlockTimestamp, nil
}

func (fg *FinalityGadgetCustom) queryAllFpBtcPubKeys() ([]string, error) {
	// get the consumer chain id
	consumerId, err := fg.cwClient.QueryConsumerId()
	if err != nil {
		return nil, err
	}

	// get all the FPs pubkey for the consumer chain
	allFpPks, err := fg.bbnClient.QueryAllFpBtcPubKeys(consumerId)
	if err != nil {
		return nil, err
	}
	return allFpPks, nil
}

// periodically check and update the BTC staking activation timestamp
// Exit the goroutine once we've successfully saved the timestamp
func (fg *FinalityGadgetCustom) MonitorBtcStakingActivation(ctx context.Context) {
	ticker := time.NewTicker(fg.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timestamp, err := fg.queryBtcStakingActivationTimestamp()
			if err != nil {
				if errors.Is(err, types.ErrBtcStakingNotActivated) {
					fg.logger.Debug("BTC staking not yet activated, waiting...")
					continue
				}
				fg.logger.Error("Failed to query BTC staking activation timestamp", zap.Error(err))
				continue
			}

			err = fg.db.SaveActivatedTimestamp(timestamp)
			if err != nil {
				fg.logger.Error("Failed to save activated timestamp to database", zap.Error(err))
				continue
			}
			fg.logger.Debug("Saved BTC staking activated timestamp to database", zap.Uint64("timestamp", timestamp))
			return
		}
	}
}
