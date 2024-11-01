package service

import (
	"encoding/json"
	"fmt"
	"github.com/babylonlabs-io/finality-gadget/types"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/storage/db"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/storage/finalitygadget"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/cast"
	"net/http"
)

type FinalitySDKHandler struct {
	DB             *db.BBoltHandler
	FinalityGadget *finalitygadget.FinalityGadgetCustom
	AgentUrl       string
	client         *http.Client
}

func NewFinalitySDKHandler(db *db.BBoltHandler, agentUrl string, fg *finalitygadget.FinalityGadgetCustom) *FinalitySDKHandler {
	return &FinalitySDKHandler{
		DB:             db,
		FinalityGadget: fg,
		AgentUrl:       agentUrl,
	}
}

func (h *FinalitySDKHandler) Committer(height uint64, hash string, timestamp uint64) error {
	block := &types.Block{
		BlockHeight:    height,
		BlockHash:      hash,
		BlockTimestamp: timestamp,
	}
	res, err := verifyReqBlock(h.AgentUrl, height, hash, timestamp)
	if err != nil {
		return err
	}
	if !res {
		return fmt.Errorf("verify block failed")
	}

	return h.DB.InsertBlock(block)
}

func verifyReqBlock(agentUrl string, height uint64, hash string, timestamp uint64) (bool, error) {
	client := resty.New()
	resp, err := client.R().Get(agentUrl + "/v1/api/verify-block?height=" + cast.ToString(height) + "&hash=" + hash + "&timestamp=" + cast.ToString(timestamp))
	if err != nil {
		return false, err
	}
	if resp.StatusCode() != http.StatusOK {
		return false, nil
	}
	var res VerifyBlockRsp
	err = json.Unmarshal(resp.Body(), &res)
	if err != nil {
		return false, err
	}
	return res.Result, nil
}

func (h *FinalitySDKHandler) Query(height uint64) (bool, error) {
	block, err := h.DB.GetBlockByHeight(height)
	if err != nil {
		return false, err
	}
	res, err := h.FinalityGadget.QueryIsBlockBabylonFinalized(block)
	if err != nil {
		return false, err
	}
	return res, nil
}

func (h *FinalitySDKHandler) LatestBlock() (*types.Block, error) {
	return h.DB.QueryLatestFinalizedBlock()
}

func (h *FinalitySDKHandler) CommitterData(ctx *gin.Context) {
	var req CommitterRequest
	err := ctx.ShouldBindJSON(&req)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = h.Committer(req.Height, req.Hash, req.Timestamp)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"height": req.Height})
}

func (h *FinalitySDKHandler) QueryData(ctx *gin.Context) {
	height := ctx.Param("height")
	res, err := h.Query(cast.ToUint64(height))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *FinalitySDKHandler) LatestBlockData(ctx *gin.Context) {
	block, err := h.LatestBlock()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": block})
}
