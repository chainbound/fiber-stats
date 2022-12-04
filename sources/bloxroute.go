package sources

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

type BloxrouteSource struct {
	url string
	key string
	sub *websocket.Conn
}

type BlxrMsg struct {
	Params struct {
		Result struct {
			TxHash common.Hash
		}
	}
}

func NewBloxrouteSource(url, key string) *BloxrouteSource {
	return &BloxrouteSource{
		url: url,
		key: key,
	}
}

func (b *BloxrouteSource) Connect(ctx context.Context) error {
	var err error
	dialer := websocket.DefaultDialer

	b.sub, _, err = dialer.Dial(b.url, http.Header{"Authorization": []string{b.key}})
	if err != nil {
		return err
	}

	return nil
}

func (b *BloxrouteSource) SubscribePendingTransactions(ctx context.Context) (chan common.Hash, error) {
	hashCh := make(chan common.Hash)
	subReq := `{"id": 1, "method": "subscribe", "params": ["newTxs", {"include": ["tx_hash", "tx_contents"]}]}`

	err := b.sub.WriteMessage(websocket.TextMessage, []byte(subReq))
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			var decoded BlxrMsg
			_, msg, err := b.sub.ReadMessage()
			if err != nil {
				panic(err)
			}

			json.Unmarshal(msg, &decoded)
			hashCh <- decoded.Params.Result.TxHash
		}
	}()
	return hashCh, nil
}
