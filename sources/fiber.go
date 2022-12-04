package sources

import (
	"context"

	fiber "github.com/chainbound/fiber-go"
	"github.com/ethereum/go-ethereum/common"
)

type FiberSource struct {
	client *fiber.Client
}

func NewFiberSource(api, key string) *FiberSource {
	return &FiberSource{
		client: fiber.NewClient(api, key),
	}
}

func (f *FiberSource) Connect(ctx context.Context) error {
	return f.client.Connect(ctx)
}

func (f *FiberSource) SubscribePendingTransactions(ctx context.Context) (chan common.Hash, error) {
	txCh := make(chan *fiber.Transaction)

	go func() {
		if err := f.client.SubscribeNewTxs(nil, txCh); err != nil {
			panic(err)
		}
	}()

	hashCh := make(chan common.Hash)
	go func() {
		for tx := range txCh {
			hashCh <- tx.Hash
		}
	}()

	return hashCh, nil
}
