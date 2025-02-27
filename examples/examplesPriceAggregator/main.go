package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/multiversx/mx-chain-crypto-go/signing"
	"github.com/multiversx/mx-chain-crypto-go/signing/ed25519"
	logger "github.com/multiversx/mx-chain-logger-go"
	"github.com/multiversx/mx-sdk-go/aggregator"
	"github.com/multiversx/mx-sdk-go/aggregator/fetchers"
	"github.com/multiversx/mx-sdk-go/aggregator/mock"
	"github.com/multiversx/mx-sdk-go/authentication"
	"github.com/multiversx/mx-sdk-go/blockchain"
	"github.com/multiversx/mx-sdk-go/blockchain/cryptoProvider"
	"github.com/multiversx/mx-sdk-go/core"
	"github.com/multiversx/mx-sdk-go/core/polling"
	"github.com/multiversx/mx-sdk-go/examples"
	"github.com/multiversx/mx-sdk-go/interactors"
)

var log = logger.GetOrCreate("mx-sdk-go/examples/examplesPriceAggregator")

const base = "ETH"
const quote = "USD"
const percentDifferenceToNotify = 1 // 0 will notify on each fetch
const decimals = 2

const minResultsNum = 3
const pollInterval = time.Second * 2
const autoSendInterval = time.Second * 10

const networkAddress = "https://testnet-gateway.multiversx.com"

var (
	suite  = ed25519.NewEd25519()
	keyGen = signing.NewKeyGenerator(suite)
)

func main() {
	_ = logger.SetLogLevel("*:DEBUG")

	log.Info("examplesPriceAggregator will fetch the price of a defined pair from a bunch of exchanges, and will" +
		" notify a printer if the price changed")
	log.Info("application started, press CTRL+C to stop the app...")

	err := runApp()
	if err != nil {
		log.Error(err.Error())
	} else {
		log.Info("application gracefully closed")
	}
}

func runApp() error {
	priceFetchers, err := createPriceFetchers()
	if err != nil {
		return err
	}

	argsPriceAggregator := aggregator.ArgsPriceAggregator{
		PriceFetchers: priceFetchers,
		MinResultsNum: minResultsNum,
	}
	aggregatorInstance, err := aggregator.NewPriceAggregator(argsPriceAggregator)
	if err != nil {
		return err
	}

	printNotifee := &mock.PriceNotifeeStub{
		PriceChangedCalled: func(ctx context.Context, args []*aggregator.ArgsPriceChanged) error {
			for _, arg := range args {
				log.Info("Notified about the price changed",
					"pair", fmt.Sprintf("%s-%s", arg.Base, arg.Quote),
					"denominated price", arg.DenominatedPrice,
					"decimals", arg.Decimals,
					"timestamp", arg.Timestamp)
			}

			return nil
		},
	}

	pairs := []*aggregator.ArgsPair{
		{
			Base:                      base,
			Quote:                     quote,
			PercentDifferenceToNotify: percentDifferenceToNotify,
			Decimals:                  decimals,
			Exchanges:                 fetchers.ImplementedFetchers,
		},
	}
	argsPriceNotifier := aggregator.ArgsPriceNotifier{
		Pairs:            pairs,
		Aggregator:       aggregatorInstance,
		Notifee:          printNotifee,
		AutoSendInterval: autoSendInterval,
	}

	priceNotifier, err := aggregator.NewPriceNotifier(argsPriceNotifier)
	if err != nil {
		return err
	}

	addPairsToFetchers(pairs, priceFetchers)

	argsPollingHandler := polling.ArgsPollingHandler{
		Log:              log,
		Name:             "price notifier polling handler",
		PollingInterval:  pollInterval,
		PollingWhenError: pollInterval,
		Executor:         priceNotifier,
	}

	pollingHandler, err := polling.NewPollingHandler(argsPollingHandler)
	if err != nil {
		return err
	}

	defer func() {
		errClose := pollingHandler.Close()
		log.LogIfError(errClose)
	}()

	err = pollingHandler.StartProcessingLoop()
	if err != nil {
		return err
	}

	chStop := make(chan os.Signal)
	signal.Notify(chStop, os.Interrupt)
	<-chStop

	return nil
}

func addPairsToFetchers(pairs []*aggregator.ArgsPair, priceFetchers []aggregator.PriceFetcher) {
	for _, pair := range pairs {
		addPairToFetchers(pair, priceFetchers)
	}
}

func addPairToFetchers(pair *aggregator.ArgsPair, priceFetchers []aggregator.PriceFetcher) {
	for _, fetcher := range priceFetchers {
		name := fetcher.Name()
		_, ok := pair.Exchanges[name]
		if !ok {
			log.Info("Missing fetcher name from known exchanges for pair",
				"fetcher", name, "pair base", pair.Base, "pair quote", pair.Quote)
			continue
		}

		fetcher.AddPair(pair.Base, pair.Quote)
	}
}

func createXExchangeMap() map[string]fetchers.XExchangeTokensPair {
	return map[string]fetchers.XExchangeTokensPair{
		"ETH-USD": {
			// for tests only until we have an ETH id
			// the price will be dropped as it is extreme compared to real price
			Base:  "WEGLD-bd4d79",
			Quote: "USDC-c76f1f",
		},
	}
}

func createPriceFetchers() ([]aggregator.PriceFetcher, error) {
	exchanges := fetchers.ImplementedFetchers
	priceFetchers := make([]aggregator.PriceFetcher, 0, len(exchanges))

	graphqlResponseGetter, err := createGraphqlResponseGetter()
	if err != nil {
		return nil, err
	}

	httpResponseGetter, err := aggregator.NewHttpResponseGetter()
	if err != nil {
		return nil, err
	}

	for exchangeName := range exchanges {
		priceFetcher, errFetch := fetchers.NewPriceFetcher(exchangeName, httpResponseGetter, graphqlResponseGetter, createXExchangeMap())
		if errFetch != nil {
			return nil, errFetch
		}

		priceFetchers = append(priceFetchers, priceFetcher)
	}

	return priceFetchers, nil
}

func createGraphqlResponseGetter() (aggregator.GraphqlGetter, error) {
	authClient, err := createAuthClient()
	if err != nil {
		return nil, err
	}

	return aggregator.NewGraphqlResponseGetter(authClient)
}

func createAuthClient() (authentication.AuthClient, error) {
	w := interactors.NewWallet()
	privateKeyBytes, err := w.LoadPrivateKeyFromPemData([]byte(examples.AlicePemContents))
	if err != nil {
		log.Error("unable to load alice.pem", "error", err)
		return nil, err
	}

	argsProxy := blockchain.ArgsProxy{
		ProxyURL:            networkAddress,
		SameScState:         false,
		ShouldBeSynced:      false,
		FinalityCheck:       false,
		AllowedDeltaToFinal: 1,
		CacheExpirationTime: time.Second,
		EntityType:          core.Proxy,
	}

	proxy, err := blockchain.NewProxy(argsProxy)
	if err != nil {
		return nil, err
	}

	holder, _ := cryptoProvider.NewCryptoComponentsHolder(keyGen, privateKeyBytes)
	args := authentication.ArgsNativeAuthClient{
		Signer:                 cryptoProvider.NewSigner(),
		ExtraInfo:              nil,
		Proxy:                  proxy,
		CryptoComponentsHolder: holder,
		TokenExpiryInSeconds:   60 * 60 * 24,
		Host:                   "oracle",
	}

	authClient, err := authentication.NewNativeAuthClient(args)
	if err != nil {
		return nil, err
	}

	return authClient, nil
}
