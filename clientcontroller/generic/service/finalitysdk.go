package service

import (
	"encoding/json"
	"github.com/babylonlabs-io/finality-gadget/types"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/generic"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/generic/finalitygadget"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/cast"
	"net/http"
)

type FinalitySDKHandler struct {
	FinalityGadget *finalitygadget.FinalityGadgetCustom
	ServiceRPC     string
}

func NewFinalitySDKHandler(servicerpc string, fg *finalitygadget.FinalityGadgetCustom) *FinalitySDKHandler {
	return &FinalitySDKHandler{
		FinalityGadget: fg,
		ServiceRPC:     servicerpc,
	}
}

func (h *FinalitySDKHandler) Query(namespace string, height uint64) (bool, error) {
	client := resty.New()
	blockresp, err := client.R().Get(h.ServiceRPC + "/v1/api/get-block?namespace=" + namespace + "&height=" + cast.ToString(height))
	if err != nil {
		return false, err
	}
	var l2Block generic.BlockInfo
	err = json.Unmarshal(blockresp.Body(), &l2Block)
	if err != nil {
		return false, err
	}

	block := &types.Block{
		BlockHeight:    l2Block.Height,
		BlockHash:      l2Block.Hash,
		BlockTimestamp: l2Block.Timestamp,
	}
	res, err := h.FinalityGadget.QueryIsBlockBabylonFinalized(block)
	if err != nil {
		return false, err
	}
	return res, nil
}

func (h *FinalitySDKHandler) QueryData(ctx *gin.Context) {
	height := ctx.Param("height")
	namespace := ctx.Param("namespace")
	res, err := h.Query(namespace, cast.ToUint64(height))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": res})
}
