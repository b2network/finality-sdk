package service

type FinalitySDK interface {
	Query(namespace string, height uint64) (bool, error)
}
