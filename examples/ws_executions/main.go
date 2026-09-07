// Executions is a read-only example: it streams KIS domestic execution
// notices to the console. Configure every value through the environment;
// nothing here belongs in source control.
//
// Required environment:
//
//	KIS_MODE      "mock" (default) or "live"
//	KIS_APP_KEY   the application key
//	KIS_APP_SECRET the application secret
//	KIS_HTS_ID    the HTS ID whose executions are streamed
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mgh3326/go-kis/kis"
	"github.com/mgh3326/go-kis/kis/ws"
)

func main() {
	mode := kis.Mode(envOr("KIS_MODE", string(kis.Mock)))
	host, endpoint, err := target(mode)
	if err != nil {
		log.Fatal(err)
	}
	// Live and mock use different transaction IDs for the same stream.
	execution, err := kis.TransactionID(mode, ws.TRExecutionVTS, ws.TRExecutionLive)
	if err != nil {
		log.Fatal(err)
	}
	htsID := os.Getenv("KIS_HTS_ID")
	if htsID == "" {
		log.Fatal("KIS_HTS_ID is required")
	}

	client, err := kis.NewClient(kis.Config{
		Host:           host,
		AppKey:         os.Getenv("KIS_APP_KEY"),
		AppSecret:      os.Getenv("KIS_APP_SECRET"),
		RequestTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := ws.Dial(ctx, ws.Config{
		Endpoint: endpoint,
		// No Approval provider is given, so the default one issues and caches
		// an approval key through the client above. A successor process would
		// inject its own provider here to reuse the cached key instead.
		Client: client,
		Dialer: ws.NewDialer(),
		OnReconnect: func(info ws.ReconnectInfo) {
			if info.Stopped {
				log.Printf("stream stopped after %d attempts: %v", info.Attempt, info.Err)
				return
			}
			log.Printf("reconnected on attempt %d, %d subscriptions restored", info.Attempt, info.Subscriptions)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	// Close sends an unsubscribe for every active stream before dropping the
	// socket, which is what frees the app key for the next process.
	defer conn.Close()

	if err := conn.Subscribe(ctx, execution, htsID); err != nil {
		log.Fatal(err)
	}

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for event := range conn.Events() {
		if event.Execution == nil {
			continue
		}
		filled := event.Execution
		fmt.Printf("%s %s %s x%s @%s (order %s)\n",
			filled.FilledAt, filled.Symbol, filled.Side, filled.Qty, filled.Price, filled.OrderNo)
	}
	if stats := conn.Stats(); stats.DroppedFrames > 0 {
		log.Printf("%d frames dropped, last: %s (%s)", stats.DroppedFrames, stats.LastDropTR, stats.LastDropReason)
	}
}

func target(mode kis.Mode) (host, endpoint string, err error) {
	switch mode {
	case kis.Live:
		return kis.HostLive, ws.EndpointLive, nil
	case kis.Mock:
		return kis.HostVTS, ws.EndpointVTS, nil
	default:
		return "", "", kis.ErrInvalidMode
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
