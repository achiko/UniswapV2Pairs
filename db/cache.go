package db

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
)

type accountData struct {
	Account string  `redis:"Account"`
	Health  float64 `redis:"Health"`
}

func main() {

	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	_ = rdb

	ctx := context.Background()
	_ = ctx

	rdb.FlushDB(ctx)

	data := map[string]interface{}{"Account": "0xaaa00003939393", "Health": 33.45}

	err := rdb.HSet(ctx, "map1", data).Err()
	if err != nil {
		panic(err)
	}

	// Get the map. The same approach works for HmGet().
	res := rdb.HGetAll(ctx, "map1")
	if res.Err() != nil {
		panic(err)
	}

	fmt.Println(res)

	// Scan the results into the struct.
	var d accountData
	if err := res.Scan(&d); err != nil {
		panic(err)
	}

	fmt.Println(d)
}
