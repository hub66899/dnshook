package shutdown

import (
	"context"
	"github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"
)

type callback func(ctx context.Context) error

type syncTodo struct {
	apply    callback
	priority int
}

var (
	asyncStops []callback
	syncStops  []syncTodo
	mu         sync.RWMutex
	timeout    = time.Second * 10
)

func Wait() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	mu.RLock()
	defer mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stopSync(ctx)
	stopAsync(ctx)
}

func OnShutdown(cb callback, args ...int) {
	mu.Lock()
	defer mu.Unlock()
	priority := 0
	if len(args) > 0 {
		priority = args[0]
	}
	if priority == 0 {
		asyncStops = append(asyncStops, cb)
	} else {
		syncStops = append(syncStops, syncTodo{
			apply:    cb,
			priority: priority,
		})
	}
}

func SetTimeout(d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	timeout = d
}

func stopSync(ctx context.Context) {
	sort.Slice(syncStops, func(i, j int) bool {
		return syncStops[i].priority < syncStops[j].priority
	})
	for _, s := range syncStops {
		if err := s.apply(ctx); err != nil {
			logrus.WithError(err).Error("shutdown error")
		}
	}
}

func stopAsync(ctx context.Context) {
	wg := sync.WaitGroup{}
	for _, s := range asyncStops {
		wg.Add(1)
		go func(s callback) {
			defer wg.Done()
			if err := s(ctx); err != nil {
				logrus.WithError(err).Error("shutdown error")
			}
		}(s)
	}
	wg.Wait()
}
