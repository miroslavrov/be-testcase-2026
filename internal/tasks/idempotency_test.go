package tasks

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

func TestCreateIdempotentConcurrent(t *testing.T) {
	pool := newTestPool(t)
	org, user, tool := setupFixtures(t, pool)
	svc := NewService(pool)

	cost := 0.01
	in := CreateInput{
		Title:            "concurrent idem",
		RequiredSlotType: "standard",
		Priority:         3,
		ToolCalls:        []ToolCallInput{{ToolName: tool, EstimatedCostUSD: &cost}},
	}
	key := "idem-" + suffix()

	const n = 3
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, err := svc.Create(context.Background(), org, user, key, in)
			ids[i] = task.ID
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "request %d failed", i)
	}
	for i := 1; i < n; i++ {
		assert.Equal(t, ids[0], ids[i], "all responses must carry the same task id")
	}

	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`select count(*) from tasks where org_id = $1`, org).Scan(&count))
	assert.Equal(t, 1, count, "exactly one task must exist in the db")
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
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("no test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("test database not reachable: %v", err)
	}
	// закрываем через cleanup, а не defer: иначе пул умрёт раньше, чем отработает
	// очистка фикстур, и она тихо не сработает
	t.Cleanup(pool.Close)
	return pool
}

// заводит изолированную оргу с активной подпиской и одним инструментом,
// имена с суффиксом чтобы тесты не мешали друг другу и сидам
func setupFixtures(t *testing.T, pool *pgxpool.Pool) (orgID, userID, toolName string) {
	t.Helper()
	ctx := context.Background()
	sfx := suffix()

	require.NoError(t, pool.QueryRow(ctx,
		`insert into organizations (name) values ($1) returning id`, "org-"+sfx).Scan(&orgID))

	var planID string
	require.NoError(t, pool.QueryRow(ctx, `
		insert into plans (name, max_concurrent_slots, monthly_budget_usd, auto_approve_threshold_usd)
		values ($1, 10, 1000, 1) returning id`, "plan-"+sfx).Scan(&planID))

	_, err := pool.Exec(ctx, `
		insert into subscriptions (org_id, plan_id, status, current_period_start, current_period_end)
		values ($1, $2, 'active', date_trunc('month', now()), date_trunc('month', now()) + interval '1 month')`,
		orgID, planID)
	require.NoError(t, err)

	require.NoError(t, pool.QueryRow(ctx, `
		insert into users (org_id, email, password_hash, role)
		values ($1, $2, 'x', 'member') returning id`, orgID, "u-"+sfx+"@test").Scan(&userID))

	toolName = "tool_" + sfx
	_, err = pool.Exec(ctx, `
		insert into tool_definitions (name, risk_level, base_cost_usd)
		values ($1, 'read', 0.01)`, toolName)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from state_transitions where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from idempotency_keys where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from tool_calls where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from tasks where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from tool_definitions where name = $1`, toolName)
		_, _ = pool.Exec(ctx, `delete from subscriptions where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from users where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from plans where id = $1`, planID)
		_, _ = pool.Exec(ctx, `delete from organizations where id = $1`, orgID)
	})
	return orgID, userID, toolName
}

func suffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
