package main

import (
	"context"
	"fmt"
	_ "github.com/ethereum/go-ethereum"
	_ "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	_ "github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	_ "github.com/ethereum/go-ethereum/common"
	_ "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-redis/redis/v8"
	"log"
	"sync"
	"time"
	ERC20 "uniswap-go/Uniswap/Tokens"
	"uniswap-go/Uniswap/UniswapV2/UniswapV2Factory"
	"uniswap-go/Uniswap/UniswapV2/UniswapV2Pair"
)

type pairFlat struct {
	PairAddress    string
	Token0Address  string
	Token1Address  string
	Token0Decimals uint8
	Token1Decimals uint8
	Token0Symbol   string
	Token1Symbol   string
}

type Token struct {
	Address  common.Address
	Symbol   string
	Decimals uint8
}

type Pair struct {
	Address common.Address
	Token0  Token
	Token1  Token
}

type PairsResult struct {
	ErrorName  string
	PairsCount int
}

var wg = sync.WaitGroup{}

func main() {

	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	_ = rdb

	client, err := ethclient.Dial(getENV("RPC_WS"))
	if err != nil {
		log.Fatal(err)
	}
	_ = client

	// factory, err := UniswapV2Factory.NewUniswapV2Factory(common.HexToAddress(dex.factoryAddress), client)

	paiAddress := common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc")
	pairContract, err := UniswapV2Pair.NewUniswapV2Pair(paiAddress, client)
	watchOpts := &bind.WatchOpts{Context: context.Background()}

	channel := make(chan *UniswapV2Pair.UniswapV2PairSync)

	go func() {
		sub, err := pairContract.WatchSync(watchOpts, channel)
		if err != nil {
			fmt.Println(err)
		}
		defer sub.Unsubscribe()
	}()

	// Receive events from the channel
	event := <-channel

	fmt.Println("Event :", time.Now(), event.Reserve0, event.Reserve1)

}

func pairFetcher() {
	startTime := time.Now()

	fmt.Println("RPC: ", getENV("RPC_HTTP"))
	client, err := ethclient.Dial(getENV("RPC_HTTP"))

	if err != nil {
		log.Fatal(err)
	}
	_ = client

	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	_ = rdb

	dex := getDex(0)
	_ = dex

	currentBlock, _ := client.BlockNumber(context.Background())
	fmt.Println("Latest Block Number : ", currentBlock)

	var startBlock = uint64(dex.blockNumber)
	const pageSize = uint64(100_000)

	pageCount := (currentBlock - 1000000 - startBlock) / pageSize

	if pageCount == 0 {
		pageCount = 1
	}

	fmt.Println("pagesCount : ", pageCount)

	for p := 0; p < int(pageCount); p++ {
		wg.Add(1)

		_p := uint64(p)
		fmt.Println(p, "Start Block= ", startBlock+_p*pageSize, "EndBlock=", startBlock+pageSize*(_p+1))

		_startBlock := startBlock + pageSize*_p
		_endBlock := startBlock + pageSize*(_p+1)

		//pairChannel := make(chan PairsResult, 100)
		//_ = pairChannel

		go fetchPairs(dex, _startBlock, &_endBlock, client, rdb)
	}

	wg.Wait()

	fmt.Println("Total Time Elapsed : ", time.Since(startTime))
}

func fetchPairs(dex Dex, startBlock uint64, endBlock *uint64, client *ethclient.Client, rdb *redis.Client) {

	startTime := time.Now()

	fmt.Println("Factory Address of", dex.dexName, dex.factoryAddress)
	factory, err := UniswapV2Factory.NewUniswapV2Factory(common.HexToAddress(dex.factoryAddress), client)

	if err != nil {
		log.Fatal(err)
	}

	filterOpts := &bind.FilterOpts{Context: context.Background(), Start: startBlock, End: endBlock}

	itr, err := factory.FilterPairCreated(filterOpts, nil, nil)

	if err != nil {
		fmt.Println("Err : ", err)
	}

	fmt.Println("Fetch Time ", time.Since(startTime))

	pairs := []Pair{}

	for itr.Next() {
		event := itr.Event
		ch := make(chan [2]Token, 300) // why 300 avoid blocking ?
		go packTokens(event.Token0, event.Token1, client, ch)
		result := <-ch
		pair := &Pair{Address: event.Pair, Token0: result[0], Token1: result[1]}
		go dbSavePair(pair, rdb)
	}

	fmt.Println("Total Pairs fetched : ", len(pairs))
	fmt.Println("EndTime ", time.Since(startTime))

	wg.Done()
}

func dbSavePair(p *Pair, rdb *redis.Client) {

	// Make flat data for redis
	data := map[string]interface{}{
		"PairAddress":    p.Address.String(),
		"Token0Address":  p.Token0.Address.String(),
		"Token1Address":  p.Token1.Address.String(),
		"Token0Decimals": p.Token0.Decimals,
		"Token1Decimals": p.Token1.Decimals,
		"Token0Symbol":   p.Token0.Symbol,
		"Token1Symbol":   p.Token1.Symbol,
	}

	err := rdb.HSet(context.Background(), p.Address.String(), data).Err()

	if err != nil {
		panic(err)
	}
}

func packTokens(token0Address common.Address, token1Address common.Address, client *ethclient.Client, ch chan [2]Token) {
	var tokens [2]Token
	cht0 := make(chan Token)
	cht1 := make(chan Token)
	go getTokenInfo(token0Address, client, cht0)
	go getTokenInfo(token1Address, client, cht1)
	tokens[0] = <-cht0
	tokens[1] = <-cht1
	ch <- tokens
}

func getTokenInfo(tokenAddress common.Address, client *ethclient.Client, ch chan Token) {
	callOpts := &bind.CallOpts{Pending: false, Context: context.Background()}
	tokenInstance, err := ERC20.NewERC20(tokenAddress, client)
	if err != nil {
		log.Fatal(err)
	}

	tokenDecimals, err := tokenInstance.Decimals(callOpts)
	if err != nil {
		log.Println("Decimals : ", err)
	}

	tokenSymbol, err := tokenInstance.Symbol(callOpts)
	if err != nil {
		//log.Println("-----------------------------")
		//log.Println("Symbol", err)
		//log.Println(tokenAddress.String())
		//log.Println("-----------------------------")
		tokenSymbol = "NA"
	}

	ch <- Token{Address: tokenAddress, Symbol: tokenSymbol, Decimals: tokenDecimals}
}
