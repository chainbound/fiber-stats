package sources

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type Web3Source struct {
	endpoint string
	client   *gethclient.Client
}

func NewWeb3Source(endpoint string) *Web3Source {
	return &Web3Source{
		endpoint: endpoint,
		// client: gethclient.NewClient(endpoint),
	}
}

func (w *Web3Source) Connect(ctx context.Context) error {
	c, err := rpc.DialContext(ctx, w.endpoint)
	if err != nil {
		return err
	}

	w.client = gethclient.New(c)
	return nil
}

func (w *Web3Source) SubscribePendingTransactions(ctx context.Context) (chan common.Hash, error) {
	sink := make(chan common.Hash)
	if _, err := w.client.SubscribePendingTransactions(ctx, sink); err != nil {
		return nil, err
	}

	return sink, nil
}
