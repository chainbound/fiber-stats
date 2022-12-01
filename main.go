package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	fiber "github.com/chainbound/fiber-go"
	"github.com/chainbound/shardmap"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Ingester struct {
	client      *gethclient.Client
	fiberClient *fiber.Client

	infuraResults *shardmap.FIFOMap[string, int64]
	fiberResults  *shardmap.FIFOMap[string, int64]
}

type MetricsService struct {
	observations *prometheus.HistogramVec
}

func NewMetrics() *MetricsService {
	return &MetricsService{
		observations: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "fiber_infura_latency",
			Help:    "Latency between fiber and infura.",
			Buckets: []float64{0, 5, 10, 25, 50, 100, 250, 500, 1000, 2500},
		}, []string{"winner"}),
	}
}

func (m *MetricsService) Run(stream chan int64) {
	go func() {
		count := 0
		for o := range stream {
			count++

			if count >= 10 {
				millis := float64(o) / 1000
				// If the latency is larger than 500 milliseconds when Infura wins,
				// we consider it a bad measurement and discard it.
				if millis > -500 {
					fmt.Println("New observation:", millis)
					if millis > 0 {
						m.observations.WithLabelValues("fiber").Observe(millis)
					} else {
						m.observations.WithLabelValues("infura").Observe(-millis)
					}
					count = 0
				}

			}

		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}

func NewIngester(ethclient *gethclient.Client, fiberClient *fiber.Client) *Ingester {
	return &Ingester{
		client:        ethclient,
		fiberClient:   fiberClient,
		infuraResults: shardmap.NewFIFOMap[string, int64](16384, 2, shardmap.HashString),
		fiberResults:  shardmap.NewFIFOMap[string, int64](16384, 2, shardmap.HashString),
	}
}

func (i *Ingester) RecordCollisions() chan int64 {
	collisions := make(chan int64, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sink := make(chan common.Hash)
	_, err := i.client.SubscribePendingTransactions(ctx, sink)
	if err != nil {
		panic(err)
	}

	fiberSub := make(chan *fiber.Transaction)
	go func() {
		if err := i.fiberClient.SubscribeNewTxs(nil, fiberSub); err != nil {
			panic(err)
		}
	}()

	go func() {
		for {
			select {
			case tx := <-fiberSub:
				ts := time.Now().UnixMicro()
				hash := tx.Hash.Hex()
				if ts2, ok := i.infuraResults.Get(hash); ok {
					collisions <- ts2 - ts
				} else {
					if ok := i.fiberResults.Has(hash); !ok {
						i.fiberResults.Put(hash, ts)
					}
				}

			case hash := <-sink:
				str := hash.Hex()
				ts := time.Now().UnixMicro()
				if ts2, ok := i.fiberResults.Get(str); ok {
					collisions <- ts - ts2
				} else {
					if ok := i.infuraResults.Has(str); !ok {
						i.infuraResults.Put(str, ts)
					}
				}
			}
		}
	}()

	return collisions
}

func main() {
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}

	infuraUrl := os.Getenv("INFURA_API")
	fiberUrl := os.Getenv("FIBER_API")
	fiberAPI := os.Getenv("FIBER_API_KEY")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := rpc.DialContext(ctx, infuraUrl)
	if err != nil {
		panic(err)
	}

	client := gethclient.New(c)
	fiberClient := fiber.NewClient(fiberUrl, fiberAPI)

	if err := fiberClient.Connect(ctx); err != nil {
		panic(err)
	}

	i := NewIngester(client, fiberClient)

	m := NewMetrics()

	m.Run(i.RecordCollisions())
}
