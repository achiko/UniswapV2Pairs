package main

/*
 id | exchange_name |              factory_address               |               router_address               | block_number |                 web_address
----+---------------+--------------------------------------------+--------------------------------------------+--------------+----------------------------------------------
  1 | uniswapv2     | 0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f | 0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D |     10000835 | https://app.uniswap.org/#/swap?chain=mainnet
  2 | sushiswap     | 0xC0AEe478e3658e2610c5F7A4A2E1777cE9e4f2Ac | 0xd9e1cE17f2641f24aE83637ab66a2cca9C378B9F |     10794229 | https://sushi.com/

*/

type Dex struct {
	dexName        string
	factoryAddress string
	routerAddress  string
	blockNumber    int
	webAddress     string
}

func getDex(index int) Dex {
	return Dex{dexName: "Uniswapv2", factoryAddress: "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f", routerAddress: "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D", blockNumber: 10000835}

}
