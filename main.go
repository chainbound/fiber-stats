package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/chainbound/fiber-stats/sources"
	"github.com/chainbound/shardmap"
	"github.com/ethereum/go-ethereum/common"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	Sources map[string]SourceConfig
}

type SourceConfig struct {
	Endpoint string
	Key      string
}

type Ingester struct {
	sources map[string]MempoolSource
	results map[string]*shardmap.FIFOMap[string, int64]

	// infuraResults *shardmap.FIFOMap[string, int64]
	// fiberResults  *shardmap.FIFOMap[string, int64]
}

type MetricsService struct {
	observations *prometheus.HistogramVec
}

type MempoolSource interface {
	SubscribePendingTransactions(ctx context.Context) (chan common.Hash, error)
}

func NewMetrics() *MetricsService {
	return &MetricsService{
		observations: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "fiber_latency_diff",
			Help:    "Latency between Fiber and other mempool sources",
			Buckets: []float64{0, 5, 10, 25, 50, 100, 250, 500, 1000, 2500},
		}, []string{"winner", "loser"}),
	}
}

func (m *MetricsService) Run(stream chan Collision) {
	go func() {
		count := 0
		for c := range stream {
			count++

			if count >= 10 {
				millis := float64(c.Diff) / 1000
				// If the latency is larger than 500 milliseconds when Infura wins,
				// we consider it a bad measurement and discard it.
				if millis > -2000 {
					fmt.Println("New observation:", c)
					if millis > 0 {
						m.observations.WithLabelValues(c.Winner, c.Loser).Observe(millis)
					} else {
						m.observations.WithLabelValues(c.Winner, c.Loser).Observe(-millis)
					}
					count = 0
				}

			}

		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}

func NewIngester(sources map[string]MempoolSource) *Ingester {
	results := make(map[string]*shardmap.FIFOMap[string, int64], len(sources))

	for name := range sources {
		results[name] = shardmap.NewFIFOMap[string, int64](16384, 2, shardmap.HashString)
	}

	return &Ingester{
		sources: sources,
		results: results,
	}
}

type Collision struct {
	Winner string
	Loser  string
	Diff   int64
	Hash   common.Hash
}

func (i *Ingester) RecordCollisions() chan Collision {
	collisions := make(chan Collision, 128)

	subs := make(map[string]chan common.Hash, len(i.sources))

	for name, source := range i.sources {
		sub, err := source.SubscribePendingTransactions(context.Background())
		if err != nil {
			panic(err)
		}

		subs[name] = sub
	}

	// We only want to compare Fiber to the other sources, not the other sources with
	// each other.
	for name, sub := range subs {
		if name == "fiber" {
			go func(name string, sub chan common.Hash) {
				for hash := range sub {
					// Check if the hash exists in the other sources.
					ts := time.Now().UnixMicro()
					i.results["fiber"].Put(hash.Hex(), ts)
				inner:
					for otherName, otherResults := range i.results {
						if otherName != "fiber" {
							if otherTs, ok := otherResults.Get(hash.Hex()); ok {
								// We have a collision!

								collisions <- Collision{
									Winner: otherName,
									Loser:  "fiber",
									Diff:   otherTs - ts,
									Hash:   hash,
								}

								// After collision, break this loop
								break inner
							}
						}
					}
				}
			}(name, sub)
		} else {
			go func(name string, sub chan common.Hash) {
				for hash := range sub {
					ts := time.Now().UnixMicro()
					i.results[name].Put(hash.Hex(), ts)

					if fiberTs, ok := i.results["fiber"].Get(hash.Hex()); ok {
						// We have a collision!

						collisions <- Collision{
							Winner: "fiber",
							Loser:  name,
							Diff:   ts - fiberTs,
							Hash:   hash,
						}
					}
				}
			}(name, sub)
		}
	}

	return collisions
}

func main() {
	var cfg Config
	_, err := toml.DecodeFile("config.toml", &cfg)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", cfg)
	err = godotenv.Load()
	if err != nil {
		panic(err)
	}

	mempoolSources := make(map[string]MempoolSource)

	for name, source := range cfg.Sources {
		switch name {
		case "fiber":
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			fiber := sources.NewFiberSource(source.Endpoint, source.Key)
			if err := fiber.Connect(ctx); err != nil {
				panic(err)
			}
			mempoolSources[name] = fiber
		case "bloxroute":
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			blxr := sources.NewBloxrouteSource(source.Endpoint, source.Key)
			if err := blxr.Connect(ctx); err != nil {
				panic(err)
			}

			mempoolSources[name] = blxr
		case "web3":
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			w3 := sources.NewWeb3Source(source.Endpoint)
			if err := w3.Connect(ctx); err != nil {
				panic(err)
			}

			mempoolSources[name] = w3
		}
	}

	i := NewIngester(mempoolSources)

	m := NewMetrics()

	m.Run(i.RecordCollisions())
}
