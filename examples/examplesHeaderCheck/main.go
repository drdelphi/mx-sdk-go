package main

import (
	"context"
	"time"

	logger "github.com/ElrondNetwork/elrond-go-logger"
	"github.com/ElrondNetwork/elrond-sdk-erdgo/blockchain"
	"github.com/ElrondNetwork/elrond-sdk-erdgo/core"
	"github.com/ElrondNetwork/elrond-sdk-erdgo/examples"
	"github.com/ElrondNetwork/elrond-sdk-erdgo/headerCheck"
)

var log = logger.GetOrCreate("elrond-sdk-erdgo/examples/examplesHeaderCheck")

func main() {
	args := blockchain.ArgsElrondProxy{
		ProxyURL:            examples.TestnetGateway,
		Client:              nil,
		SameScState:         false,
		ShouldBeSynced:      false,
		FinalityCheck:       false,
		CacheExpirationTime: time.Minute,
		EntityType:          core.Proxy,
	}
	ep, err := blockchain.NewElrondProxy(args)
	if err != nil {
		log.Error("error creating proxy", "error", err)
		return
	}

	headerVerifier, err := headerCheck.NewHeaderCheckHandler(ep)
	if err != nil {
		log.Error("error creating header check handler", "error", err)
		return
	}

	// set header headerHash and shard ID
	headerHash := "e0b29ef07f76b75ea9608eed37c588440113724077f57cda3bac84ea0de378ab"
	shardID := uint32(2)

	ok, err := headerVerifier.VerifyHeaderSignatureByHash(context.Background(), shardID, headerHash)
	if err != nil {
		log.Error("error verifying header signature", "error", err)
		return
	}
	if !ok {
		log.Info("header signature does not match")
		return
	}

	log.Info("header signature matches")
}
