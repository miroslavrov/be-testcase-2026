package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReserveSlotConcurrent(t *testing.T) {
	pool := newTestPool(t)
	orgID := setupSlotFixtures(t, pool, 10)

	const workers = 50
	ids := make([]string, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer tx.Rollback(ctx)

			id, err := ReserveSlot(ctx, tx, orgID, "standard")
			if err != nil {
				errs[i] = err
				return
			}
			if err := tx.Commit(ctx); err != nil {
				errs[i] = err
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()

	won := make(map[string]bool)
	for i := 0; i < workers; i++ {
		if errs[i] == nil {
			won[ids[i]] = true
			continue
		}
		require.ErrorIs(t, errs[i], ErrNoFreeSlot, "проигравшие должны получить ровно ErrNoFreeSlot")
	}

	successes := 0
	for i := range errs {
		if errs[i] == nil {
			successes++
		}
	}
	assert.Equal(t, 10, successes, "ровно 10 успешных резерваций")
	assert.Len(t, won, 10, "все выданные слоты разные, дублей нет")

	var busy int
	require.NoError(t, pool.QueryRow(context.Background(),
		`select count(*) from agent_slots where org_id = $1 and status = 'busy'`, orgID).Scan(&busy))
	assert.Equal(t, 10, busy, "в базе ровно 10 занятых слотов")
}

// --- helpers ---

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://agenthub:agenthub@localhost:5432/agenthub?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	// коннектов больше чем горутин, чтобы драка шла в постгресе, а не в очереди пула
	cfg.MaxConns = 60

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("no test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("test database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func setupSlotFixtures(t *testing.T, pool *pgxpool.Pool, slots int) string {
	t.Helper()
	ctx := context.Background()
	sfx := make([]byte, 6)
	_, _ = rand.Read(sfx)
	name := hex.EncodeToString(sfx)

	var orgID string
	require.NoError(t, pool.QueryRow(ctx,
		`insert into organizations (name) values ($1) returning id`, "org-"+name).Scan(&orgID))

	var planID string
	require.NoError(t, pool.QueryRow(ctx, `
		insert into plans (name, max_concurrent_slots, monthly_budget_usd, auto_approve_threshold_usd)
		values ($1, 50, 1000, 1) returning id`, "plan-"+name).Scan(&planID))

	_, err := pool.Exec(ctx, `
		insert into subscriptions (org_id, plan_id, status, current_period_start, current_period_end)
		values ($1, $2, 'active', date_trunc('month', now()), date_trunc('month', now()) + interval '1 month')`,
		orgID, planID)
	require.NoError(t, err)

	for i := 0; i < slots; i++ {
		_, err := pool.Exec(ctx,
			`insert into agent_slots (org_id, slot_type) values ($1, 'standard')`, orgID)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from agent_slots where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from subscriptions where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from organizations where id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from plans where id = $1`, planID)
	})
	return orgID
}
