package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/miroslavrov/be-testcase-2026/internal/config"
	"github.com/miroslavrov/be-testcase-2026/internal/store"
	"github.com/miroslavrov/be-testcase-2026/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	host, _ := os.Hostname()
	slog.Info("worker started", "host", host, "concurrency", cfg.WorkerConcurrency)

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerConcurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			worker.New(pool, fmt.Sprintf("%s-%d", host, i)).Run(ctx, 2*time.Second)
		}(i)
	}

	<-ctx.Done()
	wg.Wait()
	slog.Info("worker stopped")
}
