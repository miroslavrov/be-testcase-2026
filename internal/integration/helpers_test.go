package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type fixtures struct {
	orgID      string
	memberID   string
	approverID string
	readTool   string
	emailTool  string
	deployTool string
}

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
	t.Cleanup(pool.Close)
	return pool
}

func setupScenario(t *testing.T, pool *pgxpool.Pool) fixtures {
	t.Helper()
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	sfx := hex.EncodeToString(b)

	var f fixtures

	require.NoError(t, pool.QueryRow(ctx,
		`insert into organizations (name) values ($1) returning id`, "org-"+sfx).Scan(&f.orgID))

	var planID string
	require.NoError(t, pool.QueryRow(ctx, `
		insert into plans (name, max_concurrent_slots, monthly_budget_usd, auto_approve_threshold_usd)
		values ($1, 10, 1000, 0.10) returning id`, "plan-"+sfx).Scan(&planID))

	_, err := pool.Exec(ctx, `
		insert into subscriptions (org_id, plan_id, status, current_period_start, current_period_end)
		values ($1, $2, 'active', date_trunc('month', now()), date_trunc('month', now()) + interval '1 month')`,
		f.orgID, planID)
	require.NoError(t, err)

	require.NoError(t, pool.QueryRow(ctx, `
		insert into users (org_id, email, password_hash, role)
		values ($1, $2, 'x', 'member') returning id`, f.orgID, "member-"+sfx+"@test").Scan(&f.memberID))
	require.NoError(t, pool.QueryRow(ctx, `
		insert into users (org_id, email, password_hash, role)
		values ($1, $2, 'x', 'approver') returning id`, f.orgID, "approver-"+sfx+"@test").Scan(&f.approverID))

	for i := 0; i < 2; i++ {
		_, err := pool.Exec(ctx,
			`insert into agent_slots (org_id, slot_type) values ($1, 'standard')`, f.orgID)
		require.NoError(t, err)
	}

	f.readTool = "read_db_" + sfx
	f.emailTool = "send_email_" + sfx
	f.deployTool = "deploy_" + sfx
	for _, tool := range []struct {
		name, risk string
		costUSD    float64
	}{
		{f.readTool, "read", 0.01},
		{f.emailTool, "write", 0.50},
		{f.deployTool, "destructive", 5.00},
	} {
		// фейл рейт нулевой, тест должен быть детерминированным
		_, err := pool.Exec(ctx, `
			insert into tool_definitions (name, risk_level, base_cost_usd, mock_min_ms, mock_max_ms, mock_failure_rate)
			values ($1, $2, $3, 5, 20, 0)`, tool.name, tool.risk, tool.costUSD)
		require.NoError(t, err)
	}

	for _, risk := range []string{"write", "destructive"} {
		var chainID string
		require.NoError(t, pool.QueryRow(ctx,
			`insert into approval_chains (org_id, risk_level) values ($1, $2) returning id`,
			f.orgID, risk).Scan(&chainID))
		_, err := pool.Exec(ctx, `
			insert into approval_chain_steps (chain_id, step_order, approver_role, timeout_hours, on_timeout)
			values ($1, 1, 'approver', 0.02, 'reject')`, chainID)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, q := range []string{
			`delete from notifications where org_id = $1`,
			`delete from usage_records where org_id = $1`,
			`delete from state_transitions where org_id = $1`,
			`delete from approval_decisions where request_id in (select id from approval_requests where org_id = $1)`,
			`delete from approval_requests where org_id = $1`,
			`delete from task_executions where org_id = $1`,
			`delete from tool_calls where org_id = $1`,
			`delete from tasks where org_id = $1`,
			`delete from approval_chains where org_id = $1`,
			`delete from agent_slots where org_id = $1`,
			`delete from subscriptions where org_id = $1`,
			`delete from users where org_id = $1`,
			`delete from organizations where id = $1`,
		} {
			_, _ = pool.Exec(ctx, q, f.orgID)
		}
		_, _ = pool.Exec(ctx, `delete from plans where id = $1`, planID)
		for _, name := range []string{f.readTool, f.emailTool, f.deployTool} {
			_, _ = pool.Exec(ctx, `delete from tool_definitions where name = $1`, name)
		}
	})
	return f
}

func waitFor(t *testing.T, pool *pgxpool.Pool, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", msg)
}

func taskStatus(t *testing.T, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var s string
	require.NoError(t, pool.QueryRow(context.Background(),
		`select status from tasks where id = $1`, taskID).Scan(&s))
	return s
}

func callStatus(t *testing.T, pool *pgxpool.Pool, taskID string, idx int) string {
	t.Helper()
	var s string
	require.NoError(t, pool.QueryRow(context.Background(),
		`select status from tool_calls where task_id = $1 and order_index = $2`, taskID, idx).Scan(&s))
	return s
}

func pendingRequest(t *testing.T, pool *pgxpool.Pool, orgID string) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(),
		`select id from approval_requests where org_id = $1 and status = 'pending'`, orgID).Scan(&id))
	return id
}

func taskPath(t *testing.T, pool *pgxpool.Pool, taskID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`select to_status from state_transitions where entity_type = 'task' and entity_id = $1 order by id`, taskID)
	require.NoError(t, err)
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	return out
}
