package finalitygadget

import "github.com/babylonlabs-io/finality-gadget/types"

type IFinalityGadgetCustom interface {
	QueryIsBlockBabylonFinalized(block *types.Block) (bool, error)
}
