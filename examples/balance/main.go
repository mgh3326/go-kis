// Balance is a read-only example. Configure values outside source control.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mgh3326/go-kis/kis"
	"github.com/mgh3326/go-kis/kis/domestic"
)

func main() {
	client, err := kis.NewClient(kis.Config{Host: kis.HostVTS, AppKey: os.Getenv("KIS_APP_KEY"), AppSecret: os.Getenv("KIS_APP_SECRET"), RequestTimeout: 10 * time.Second, TokenProvider: kis.TokenProviderFunc(func(context.Context) (string, error) { return os.Getenv("KIS_ACCESS_TOKEN"), nil })})
	if err != nil {
		panic(err)
	}
	result, err := domestic.Balance(context.Background(), client, kis.Mock, domestic.BalanceRequest{CANO: os.Getenv("KIS_CANO"), ACNT_PRDT_CD: "01"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("positions: %d\n", len(result.Output1))
}
