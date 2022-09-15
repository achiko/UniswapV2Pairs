package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
	"uniswap-go/Uniswap/UniswapV2/UniswapV2Factory"
	"uniswap-go/Uniswap/UniswapV2/UniswapV2Pair"

	ERC20 "uniswap-go/Uniswap/Tokens"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
)

type (
	pairFlat struct {
		PairAddress    string
		Token0Address  string
		Token1Address  string
		Token0Decimals uint8
		Token1Decimals uint8
		Token0Symbol   string
		Token1Symbol   string
	}

	Token struct {
		Address  common.Address
		Symbol   string
		Decimals uint8
	}

	Pair struct {
		Address common.Address
		Token0  Token
		Token1  Token
	}

	PairsResult struct {
		ErrorName  string
		PairsCount int
	}
)

var (
	wg  = sync.WaitGroup{}
	rdb *redis.Client
)

const (
	USDC_WETH_PAIR = "0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc"
)

func main() {
	ctx := context.Background()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	ethWSClient, err := ethclient.DialContext(ctx, os.Getenv("RPC_WS"))
	if err != nil {
		log.Fatal(err)
	}

	usdcWethPairC, err := NewUniV2PairContract(USDC_WETH_PAIR, ethWSClient)
	if err != nil {
		log.Fatal(err)
	}

	// factory, err := UniswapV2Factory.NewUniswapV2Factory(common.HexToAddress(dex.factoryAddress), client)

	syncEventChan := make(chan *UniswapV2Pair.UniswapV2PairSync)

	go StartSubscribeUniV2PairSyncEvent(ctx, usdcWethPairC, syncEventChan)

	// NOTE: Blocking here
	if err := HandleAllEventChan(ctx, syncEventChan); err != nil {
		log.Fatal(err)
	}
}

func NewUniV2PairContract(pairAddress string, client *ethclient.Client) (*UniswapV2Pair.UniswapV2Pair, error) {
	paiAddress := common.HexToAddress(pairAddress)

	c, err := UniswapV2Pair.NewUniswapV2Pair(paiAddress, client)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func StartSubscribeUniV2PairSyncEvent(ctx context.Context, c *UniswapV2Pair.UniswapV2Pair, eventChan chan<- *UniswapV2Pair.UniswapV2PairSync) {
	var start *uint64 = nil
	opt := &bind.WatchOpts{Context: ctx, Start: start}

	sub, err := c.WatchSync(opt, eventChan)
	if err != nil {
		log.Fatal(err)
	}
	defer sub.Unsubscribe()

	if err := <-sub.Err(); err != nil {
		fmt.Printf("[ERROR]: StartSubscribeUniV2PairSyncEvent: %s", err)
	}
}

// HACK: You can use generics for multiple event channels.
func HandleAllEventChan(ctx context.Context, uniV2PairSyncChan <-chan *UniswapV2Pair.UniswapV2PairSync) error {
	for {
		select {
		case e := <-uniV2PairSyncChan:
			fmt.Printf("[%s] EVENT: Received UniswapV2PairSyncEvent: reserve0: %s: reserve1: %s\n", time.Now().Format(time.RFC3339), e.Reserve0, e.Reserve1)
			// Do something

		// NOTE: You can handle every event like below.
		// case e := <-uniV2PairSwapChan:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func pairFetcher() {
	startTime := time.Now()

	fmt.Println("RPC: ", os.Getenv("RPC_HTTP"))
	client, err := ethclient.Dial(os.Getenv("RPC_HTTP"))

	if err != nil {
		log.Fatal(err)
	}
	_ = client

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
