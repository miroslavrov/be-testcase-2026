package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/miroslavrov/be-testcase-2026/internal/auth"
	"github.com/miroslavrov/be-testcase-2026/internal/config"
	"github.com/miroslavrov/be-testcase-2026/internal/store"
)

const demoPassword = "password123"

type chainStep struct {
	role      string
	timeoutH  float64
	onTimeout string
}

func main() {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := store.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := run(ctx, pool); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
	slog.Info("seed done", "org", "Acme Corp", "password", demoPassword)
}

func run(ctx context.Context, pool *pgxpool.Pool) error {
	orgID, err := ensureOrg(ctx, pool, "Acme Corp")
	if err != nil {
		return err
	}

	planID, err := ensurePlan(ctx, pool, "startup", 10, 100.00, 1.00)
	if err != nil {
		return err
	}
	if err := ensureSubscription(ctx, pool, orgID, planID); err != nil {
		return err
	}

	for _, u := range []struct{ email, role string }{
		{"owner@acme.test", "owner"},
		{"admin@acme.test", "admin"},
		{"approver@acme.test", "approver"},
		{"member@acme.test", "member"},
	} {
		if err := ensureUser(ctx, pool, orgID, u.email, u.role); err != nil {
			return err
		}
	}

	if err := ensureSlots(ctx, pool, orgID, "standard", 2); err != nil {
		return err
	}
	if err := ensureSlots(ctx, pool, orgID, "fast", 1); err != nil {
		return err
	}

	for _, t := range []struct {
		name, risk   string
		cost         float64
		minMs, maxMs int
		failRate     float64
	}{
		{"read_database", "read", 0.01, 100, 300, 0.01},
		{"generate_report", "read", 0.05, 300, 800, 0.02},
		{"send_email", "write", 0.50, 200, 600, 0.05},
		{"deploy_service", "destructive", 5.00, 1000, 3000, 0.10},
	} {
		if err := ensureTool(ctx, pool, t.name, t.risk, t.cost, t.minMs, t.maxMs, t.failRate); err != nil {
			return err
		}
	}

	if err := ensureChain(ctx, pool, orgID, "write", []chainStep{
		{"approver", 24, "reject"},
	}); err != nil {
		return err
	}
	return ensureChain(ctx, pool, orgID, "destructive", []chainStep{
		{"approver", 12, "escalate"},
		{"owner", 24, "reject"},
	})
}

func ensureOrg(ctx context.Context, pool *pgxpool.Pool, name string) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `select id from organizations where name = $1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	err = pool.QueryRow(ctx, `insert into organizations (name) values ($1) returning id`, name).Scan(&id)
	return id, err
}

func ensurePlan(ctx context.Context, pool *pgxpool.Pool, name string, maxSlots int, budget, threshold float64) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		insert into plans (name, max_concurrent_slots, monthly_budget_usd, auto_approve_threshold_usd)
		values ($1, $2, $3, $4)
		on conflict (name) do update set
			max_concurrent_slots = excluded.max_concurrent_slots,
			monthly_budget_usd = excluded.monthly_budget_usd,
			auto_approve_threshold_usd = excluded.auto_approve_threshold_usd
		returning id`,
		name, maxSlots, budget, threshold).Scan(&id)
	return id, err
}

func ensureSubscription(ctx context.Context, pool *pgxpool.Pool, orgID, planID string) error {
	_, err := pool.Exec(ctx, `
		insert into subscriptions (org_id, plan_id, status, current_period_start, current_period_end)
		values ($1, $2, 'active', date_trunc('month', now()), date_trunc('month', now()) + interval '1 month')
		on conflict (org_id) where status = 'active' do nothing`,
		orgID, planID)
	return err
}

func ensureUser(ctx context.Context, pool *pgxpool.Pool, orgID, email, role string) error {
	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		insert into users (org_id, email, password_hash, role)
		values ($1, $2, $3, $4)
		on conflict (email) do nothing`,
		orgID, email, hash, role)
	return err
}

func ensureSlots(ctx context.Context, pool *pgxpool.Pool, orgID, slotType string, want int) error {
	var have int
	if err := pool.QueryRow(ctx,
		`select count(*) from agent_slots where org_id = $1 and slot_type = $2`,
		orgID, slotType).Scan(&have); err != nil {
		return err
	}
	for i := have; i < want; i++ {
		if _, err := pool.Exec(ctx,
			`insert into agent_slots (org_id, slot_type) values ($1, $2)`,
			orgID, slotType); err != nil {
			return err
		}
	}
	return nil
}

func ensureTool(ctx context.Context, pool *pgxpool.Pool, name, risk string, cost float64, minMs, maxMs int, failRate float64) error {
	_, err := pool.Exec(ctx, `
		insert into tool_definitions (name, risk_level, base_cost_usd, mock_min_ms, mock_max_ms, mock_failure_rate)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (name) do update set
			risk_level = excluded.risk_level,
			base_cost_usd = excluded.base_cost_usd,
			mock_min_ms = excluded.mock_min_ms,
			mock_max_ms = excluded.mock_max_ms,
			mock_failure_rate = excluded.mock_failure_rate`,
		name, risk, cost, minMs, maxMs, failRate)
	return err
}

func ensureChain(ctx context.Context, pool *pgxpool.Pool, orgID, risk string, steps []chainStep) error {
	var chainID string
	err := pool.QueryRow(ctx, `
		insert into approval_chains (org_id, risk_level)
		values ($1, $2)
		on conflict (org_id, risk_level) do update set risk_level = excluded.risk_level
		returning id`,
		orgID, risk).Scan(&chainID)
	if err != nil {
		return err
	}
	for i, st := range steps {
		if _, err := pool.Exec(ctx, `
			insert into approval_chain_steps (chain_id, step_order, approver_role, timeout_hours, on_timeout)
			values ($1, $2, $3, $4, $5)
			on conflict (chain_id, step_order) do update set
				approver_role = excluded.approver_role,
				timeout_hours = excluded.timeout_hours,
				on_timeout = excluded.on_timeout`,
			chainID, i+1, st.role, st.timeoutH, st.onTimeout); err != nil {
			return err
		}
	}
	return nil
}
