package service

import "github.com/babylonlabs-io/finality-gadget/types"

type FinalitySDK interface {
	Committer(height uint64, hash string, timestamp uint64) error

	Query(height uint64) (bool, error)

	LatestBlock() (*types.Block, error)
}
