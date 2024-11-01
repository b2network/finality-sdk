package generic

import (
	"context"
	sdkErr "cosmossdk.io/errors"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	wasmdparams "github.com/CosmWasm/wasmd/app/params"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	bbnapp "github.com/babylonlabs-io/babylon/app"
	bbnclient "github.com/babylonlabs-io/babylon/client/client"
	bbntypes "github.com/babylonlabs-io/babylon/types"
	btcstakingtypes "github.com/babylonlabs-io/babylon/x/btcstaking/types"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/api"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/generic/finalitygadget"
	cwclient "github.com/babylonlabs-io/finality-provider/cosmwasmclient/client"
	cwconfig "github.com/babylonlabs-io/finality-provider/cosmwasmclient/config"
	fpcfg "github.com/babylonlabs-io/finality-provider/finality-provider/config"
	"github.com/babylonlabs-io/finality-provider/types"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	cmtcrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquerytypes "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/relayer/v2/relayer/provider"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/cast"
	"go.uber.org/zap"
	"math"
)

const (
	BabylonChainName = "Babylon"
)

var _ api.ConsumerController = &GenericConsumerController{}

type GenericConsumerController struct {
	Cfg        *fpcfg.ConsumerGenericConfig
	CwClient   *cwclient.Client
	bbnClient  *bbnclient.Client
	namespace  string
	serviceRpc string
	fg         *finalitygadget.FinalityGadgetCustom
	logger     *zap.Logger
}

func NewGenericConsumerController(cfg *fpcfg.ConsumerGenericConfig, fg *finalitygadget.FinalityGadgetCustom, logger *zap.Logger) (*GenericConsumerController, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config for storage consumer controller")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cwConfig := cfg.ToCosmwasmConfig()

	cwClient, err := NewCwClient(&cwConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create CW client: %w", err)
	}

	bbnConfig := cfg.ToBBNConfig()
	babylonConfig := fpcfg.BBNConfigToBabylonConfig(&bbnConfig)

	if err := babylonConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config for Babylon client: %w", err)
	}

	bc, err := bbnclient.New(
		&babylonConfig,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Babylon client: %w", err)
	}

	return &GenericConsumerController{
		Cfg:        cfg,
		CwClient:   cwClient,
		bbnClient:  bc,
		serviceRpc: cfg.ServiceRPC,
		namespace:  cfg.Namespace,
		fg:         fg,
		logger:     logger,
	}, nil
}

func NewCwClient(cwConfig *cwconfig.CosmwasmConfig, logger *zap.Logger) (*cwclient.Client, error) {
	if err := cwConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config for OP consumer controller: %w", err)
	}

	bbnEncodingCfg := bbnapp.GetEncodingConfig()
	cwEncodingCfg := wasmdparams.EncodingConfig{
		InterfaceRegistry: bbnEncodingCfg.InterfaceRegistry,
		Codec:             bbnEncodingCfg.Codec,
		TxConfig:          bbnEncodingCfg.TxConfig,
		Amino:             bbnEncodingCfg.Amino,
	}

	cwClient, err := cwclient.New(
		cwConfig,
		BabylonChainName,
		cwEncodingCfg,
		logger,
	)

	return cwClient, err
}

func (cc *GenericConsumerController) ReliablySendMsg(msg sdk.Msg, expectedErrs []*sdkErr.Error, unrecoverableErrs []*sdkErr.Error) (*provider.RelayerTxResponse, error) {
	return cc.reliablySendMsgs([]sdk.Msg{msg}, expectedErrs, unrecoverableErrs)
}

func (cc *GenericConsumerController) reliablySendMsgs(msgs []sdk.Msg, expectedErrs []*sdkErr.Error, unrecoverableErrs []*sdkErr.Error) (*provider.RelayerTxResponse, error) {
	return cc.CwClient.ReliablySendMsgs(
		context.Background(),
		msgs,
		expectedErrs,
		unrecoverableErrs,
	)
}

func (cc GenericConsumerController) CommitPubRandList(fpPk *btcec.PublicKey, startHeight uint64, numPubRand uint64, commitment []byte, sig *schnorr.Signature) (*types.TxResponse, error) {
	msg := CommitPublicRandomnessMsg{
		CommitPublicRandomness: CommitPublicRandomnessMsgParams{
			FpPubkeyHex: bbntypes.NewBIP340PubKeyFromBTCPK(fpPk).MarshalHex(),
			StartHeight: startHeight,
			NumPubRand:  numPubRand,
			Commitment:  commitment,
			Signature:   sig.Serialize(),
		},
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	execMsg := &wasmtypes.MsgExecuteContract{
		Sender:   cc.CwClient.MustGetAddr(),
		Contract: cc.Cfg.FinalityGadgetAddress,
		Msg:      payload,
	}

	res, err := cc.ReliablySendMsg(execMsg, nil, nil)
	if err != nil {
		return nil, err
	}
	return &types.TxResponse{TxHash: res.TxHash}, nil
}

func ConvertProof(cmtProof cmtcrypto.Proof) Proof {
	return Proof{
		Total:    uint64(cmtProof.Total),
		Index:    uint64(cmtProof.Index),
		LeafHash: cmtProof.LeafHash,
		Aunts:    cmtProof.Aunts,
	}
}

func (cc GenericConsumerController) SubmitFinalitySig(fpPk *btcec.PublicKey, block *types.BlockInfo, pubRand *btcec.FieldVal, proof []byte, sig *btcec.ModNScalar) (*types.TxResponse, error) {
	cmtProof := cmtcrypto.Proof{}
	if err := cmtProof.Unmarshal(proof); err != nil {
		return nil, err
	}
	msg := SubmitFinalitySignatureMsg{
		SubmitFinalitySignature: SubmitFinalitySignatureMsgParams{
			FpPubkeyHex: bbntypes.NewBIP340PubKeyFromBTCPK(fpPk).MarshalHex(),
			Height:      block.Height,
			PubRand:     bbntypes.NewSchnorrPubRandFromFieldVal(pubRand).MustMarshal(),
			Proof:       ConvertProof(cmtProof),
			BlockHash:   block.Hash,
			Signature:   bbntypes.NewSchnorrEOTSSigFromModNScalar(sig).MustMarshal(),
		},
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	fmt.Printf("submit sig msg:" + cast.ToString(block.Height))
	execMsg := &wasmtypes.MsgExecuteContract{
		Sender:   cc.CwClient.MustGetAddr(),
		Contract: cc.Cfg.FinalityGadgetAddress,
		Msg:      payload,
	}

	res, err := cc.ReliablySendMsg(execMsg, nil, nil)
	if err != nil {
		return nil, err
	}
	cc.logger.Debug(
		"Successfully submitted finality signature",
		zap.Uint64("height", block.Height),
		zap.String("block_hash", hex.EncodeToString(block.Hash)),
	)
	return &types.TxResponse{TxHash: res.TxHash}, nil
}

func (cc GenericConsumerController) SubmitBatchFinalitySigs(fpPk *btcec.PublicKey, blocks []*types.BlockInfo, pubRandList []*btcec.FieldVal, proofList [][]byte, sigs []*btcec.ModNScalar) (*types.TxResponse, error) {
	if len(blocks) != len(sigs) {
		return nil, fmt.Errorf("the number of blocks %v should match the number of finality signatures %v", len(blocks), len(sigs))
	}
	msgs := make([]sdk.Msg, 0, len(blocks))
	for i, block := range blocks {
		cmtProof := cmtcrypto.Proof{}
		if err := cmtProof.Unmarshal(proofList[i]); err != nil {
			return nil, err
		}

		msg := SubmitFinalitySignatureMsg{
			SubmitFinalitySignature: SubmitFinalitySignatureMsgParams{
				FpPubkeyHex: bbntypes.NewBIP340PubKeyFromBTCPK(fpPk).MarshalHex(),
				Height:      block.Height,
				PubRand:     bbntypes.NewSchnorrPubRandFromFieldVal(pubRandList[i]).MustMarshal(),
				Proof:       ConvertProof(cmtProof),
				BlockHash:   block.Hash,
				Signature:   bbntypes.NewSchnorrEOTSSigFromModNScalar(sigs[i]).MustMarshal(),
			},
		}
		fmt.Printf("submit batch sig msg:" + cast.ToString(block.Height))
		payload, err := json.Marshal(msg)
		if err != nil {
			return nil, err
		}
		execMsg := &wasmtypes.MsgExecuteContract{
			Sender:   cc.CwClient.MustGetAddr(),
			Contract: cc.Cfg.FinalityGadgetAddress,
			Msg:      payload,
		}
		msgs = append(msgs, execMsg)
	}

	res, err := cc.reliablySendMsgs(msgs, nil, nil)
	if err != nil {
		return nil, err
	}
	cc.logger.Debug(
		"Successfully submitted finality signatures in a batch",
		zap.Uint64("start_height", blocks[0].Height),
		zap.Uint64("end_height", blocks[len(blocks)-1].Height),
	)
	return &types.TxResponse{TxHash: res.TxHash}, nil
}

func (cc GenericConsumerController) QueryFinalityProviderHasPower(fpPk *btcec.PublicKey, blockHeight uint64) (bool, error) {
	fpBtcPkHex := bbntypes.NewBIP340PubKeyFromBTCPK(fpPk).MarshalHex()
	var nextKey []byte

	btcStakingParams, err := cc.bbnClient.QueryClient.BTCStakingParams()
	if err != nil {
		return false, err
	}
	for {
		resp, err := cc.bbnClient.QueryClient.FinalityProviderDelegations(fpBtcPkHex, &sdkquerytypes.PageRequest{Key: nextKey, Limit: 100})
		if err != nil {
			return false, err
		}

		for _, btcDels := range resp.BtcDelegatorDelegations {
			for _, btcDel := range btcDels.Dels {
				active, err := cc.isDelegationActive(btcStakingParams, btcDel)
				if err != nil {
					continue
				}
				if active {
					return true, nil
				}
			}
		}

		if resp.Pagination == nil || resp.Pagination.NextKey == nil {
			break
		}
		nextKey = resp.Pagination.NextKey
	}

	return false, nil
}

func (cc GenericConsumerController) QueryLatestFinalizedBlock() (*types.BlockInfo, error) {
	client := resty.New()
	resp, err := client.R().Get(cc.serviceRpc + "/v1/api/finalized-block?namespace=" + cc.namespace)
	if err != nil {
		return nil, err
	}
	var latestBlock LatestBlockResponse
	err = json.Unmarshal(resp.Body(), &latestBlock)
	if err != nil {
		return nil, err
	}
	if latestBlock.Error != "" {
		return nil, errors.New(latestBlock.Error)
	}

	hashByte, err := hex.DecodeString(latestBlock.Data.Hash[2:])
	if err != nil {
		return nil, err
	}

	return &types.BlockInfo{
		Height: latestBlock.Data.Height,
		Hash:   hashByte,
	}, nil

}

func (cc GenericConsumerController) QueryLastPublicRandCommit(fpPk *btcec.PublicKey) (*types.PubRandCommit, error) {
	fpPubKey := bbntypes.NewBIP340PubKeyFromBTCPK(fpPk)
	queryMsg := &QueryMsg{
		LastPubRandCommit: &PubRandCommit{
			BtcPkHex: fpPubKey.MarshalHex(),
		},
	}

	jsonData, err := json.Marshal(queryMsg)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling to JSON: %w", err)
	}

	stateResp, err := cc.CwClient.QuerySmartContractState(cc.Cfg.FinalityGadgetAddress, string(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to query smart contract state: %w", err)
	}
	if len(stateResp.Data) == 0 {
		return nil, nil
	}

	var resp *types.PubRandCommit
	err = json.Unmarshal(stateResp.Data, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if resp == nil {
		return nil, nil
	}
	if err := resp.Validate(); err != nil {
		return nil, err
	}

	return resp, nil
}

func (cc GenericConsumerController) QueryBlock(height uint64) (*types.BlockInfo, error) {
	client := resty.New()
	blockResp, err := client.R().Get(cc.serviceRpc + "/v1/api/get-block?namespace=" + cc.namespace + "&height=" + cast.ToString(height))
	if err != nil {
		return nil, err
	}
	var l2BlockRsp *GetBlockResponse
	err = json.Unmarshal(blockResp.Body(), &l2BlockRsp)
	if err != nil {
		return nil, err
	}
	if l2BlockRsp.Error != "" {
		return nil, errors.New(l2BlockRsp.Error)
	}

	hashByte, err := hex.DecodeString(l2BlockRsp.Data.Hash[2:])
	if err != nil {
		return nil, err
	}

	cc.logger.Debug(
		"QueryBlock",
		zap.Uint64("height", height),
		zap.String("block_hash", hex.EncodeToString(hashByte)),
	)

	return &types.BlockInfo{
		Height: l2BlockRsp.Data.Height,
		Hash:   hashByte,
	}, nil
}

func (cc GenericConsumerController) QueryIsBlockFinalized(height uint64) (bool, error) {
	l2Block, err := cc.QueryLatestFinalizedBlock()
	if err != nil {
		return false, err
	}
	if l2Block == nil {
		return false, nil
	}
	if height > l2Block.Height {
		return false, nil
	}
	return true, nil
}

func (cc GenericConsumerController) QueryBlocks(startHeight, endHeight, limit uint64) ([]*types.BlockInfo, error) {
	//TODO  range scans
	return nil, nil
}

func (cc GenericConsumerController) QueryLatestBlockHeight() (uint64, error) {
	block, err := cc.QueryLatestFinalizedBlock()
	if err != nil {
		return 0, err
	}
	return block.Height, nil
}

func (cc GenericConsumerController) QueryActivatedHeight() (uint64, error) {
	activatedTimestamp, err := cc.fg.QueryBtcStakingActivatedTimestamp()
	if err != nil {
		cc.logger.Error("failed to query BTC staking activate timestamp", zap.Error(err))
		return math.MaxUint64, err
	}

	l2BlockNumber, err := cc.GetBlockNumberByTimestamp(context.Background(), activatedTimestamp)
	if err != nil {
		cc.logger.Error("failed to convert L2 block number from the given BTC staking activation timestamp", zap.Error(err))
		return math.MaxUint64, err
	}

	return l2BlockNumber, nil
}

func (cc GenericConsumerController) Close() error {
	return cc.CwClient.Stop()
}

func (cc *GenericConsumerController) isDelegationActive(
	btcStakingParams *btcstakingtypes.QueryParamsResponse,
	btcDel *btcstakingtypes.BTCDelegationResponse,
) (bool, error) {

	covQuorum := btcStakingParams.GetParams().CovenantQuorum
	ud := btcDel.UndelegationResponse

	if len(ud.GetDelegatorUnbondingSigHex()) > 0 {
		return false, nil
	}

	if uint32(len(btcDel.CovenantSigs)) < covQuorum {
		return false, nil
	}
	if len(ud.CovenantUnbondingSigList) < int(covQuorum) {
		return false, nil
	}
	if len(ud.CovenantSlashingSigs) < int(covQuorum) {
		return false, nil
	}

	return true, nil
}

// GetBlockNumberByTimestamp returns the L2 block number for the given BTC staking activation timestamp.
// It uses a binary search to find the block number.
func (cc *GenericConsumerController) GetBlockNumberByTimestamp(ctx context.Context, targetTimestamp uint64) (uint64, error) {
	// Check if the target timestamp is after the latest block
	client := resty.New()
	resp, err := client.R().Get(cc.serviceRpc + "/v1/api/latest-block?namespace=" + cc.namespace)
	if err != nil {
		return math.MaxUint64, err
	}
	var latestBlock *GetBlockResponse
	err = json.Unmarshal(resp.Body(), &latestBlock)
	if err != nil {
		return math.MaxUint64, err
	}
	if latestBlock.Error != "" {
		return math.MaxUint64, errors.New(latestBlock.Error)
	}
	if targetTimestamp > latestBlock.Data.Timestamp {
		return math.MaxUint64, fmt.Errorf("target timestamp %d is after the latest block timestamp %d", targetTimestamp, latestBlock.Data.Timestamp)
	}

	// Check if the target timestamp is before the first block
	respFirstBlock, err := client.R().Get(cc.serviceRpc + "/v1/api/get-block?namespace=" + cc.namespace + "&height=" + cast.ToString(1))
	var firstBlock *GetBlockResponse

	if err != nil {
		return math.MaxUint64, err
	}
	err = json.Unmarshal(respFirstBlock.Body(), &firstBlock)
	if err != nil {
		return math.MaxUint64, err
	}
	if firstBlock.Error != "" {
		return math.MaxUint64, errors.New(firstBlock.Error)
	}

	// let's say block 0 is at t0 and block 1 at t1
	// if t0 < targetTimestamp < t1, the activated height should be block 1
	if targetTimestamp < firstBlock.Data.Timestamp {
		return uint64(1), nil
	}

	// binary search between block 1 and the latest block
	// start from block 1, b/c some L2s such as OP mainnet, block 0 is genesis block with timestamp 0
	lowerBound := uint64(1)
	upperBound := latestBlock.Data.Height

	for lowerBound <= upperBound {
		midBlockNumber := (lowerBound + upperBound) / 2
		respBlock, err := client.R().Get(cc.serviceRpc + "/v1/api/get-block?namespace=" + cc.namespace + "&height=" + cast.ToString(midBlockNumber))
		var block *GetBlockResponse

		if err != nil {
			return math.MaxUint64, err
		}
		err = json.Unmarshal(respBlock.Body(), &block)
		if err != nil {
			return math.MaxUint64, err
		}
		if firstBlock.Error != "" {
			return math.MaxUint64, errors.New(block.Error)
		}

		if block.Data.Timestamp < targetTimestamp {
			lowerBound = midBlockNumber + 1
		} else if block.Data.Timestamp > targetTimestamp {
			upperBound = midBlockNumber - 1
		} else {
			return midBlockNumber, nil
		}
	}

	return lowerBound, nil
}
