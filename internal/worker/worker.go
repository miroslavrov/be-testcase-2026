package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	pool *pgxpool.Pool
	name string
}

func New(pool *pgxpool.Pool, name string) *Worker {
	return &Worker{pool: pool, name: name}
}

// Run крутит цикл: есть работа — делаем, нет — спим и пробуем снова
func (w *Worker) Run(ctx context.Context, poll time.Duration) {
	for {
		if ctx.Err() != nil {
			return
		}

		c, err := w.claimQueued(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("claim failed", "worker", w.name, "err", err)
		}
		if c != nil {
			slog.Info("task claimed", "worker", w.name, "task", c.TaskID, "slot", c.SlotID)
			w.runExecution(ctx, c)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(poll):
		}
	}
}
