package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type SubscriptionView struct {
	PlanName                string    `json:"plan_name"`
	Status                  string    `json:"status"`
	MaxConcurrentSlots      int       `json:"max_concurrent_slots"`
	MonthlyBudgetUSD        float64   `json:"monthly_budget_usd"`
	AutoApproveThresholdUSD float64   `json:"auto_approve_threshold_usd"`
	PeriodStart             time.Time `json:"current_period_start"`
	PeriodEnd               time.Time `json:"current_period_end"`
	BudgetUsedUSD           float64   `json:"budget_used_usd"`
	BudgetReservedUSD       float64   `json:"budget_reserved_usd"`
	BudgetRemainingUSD      float64   `json:"budget_remaining_usd"`
}

func (s *Service) Subscription(ctx context.Context, orgID string) (SubscriptionView, error) {
	var v SubscriptionView
	err := s.pool.QueryRow(ctx, `
		select p.name, sub.status, p.max_concurrent_slots, p.monthly_budget_usd, p.auto_approve_threshold_usd,
		       sub.current_period_start, sub.current_period_end
		from subscriptions sub
		join plans p on p.id = sub.plan_id
		where sub.org_id = $1 and sub.status = 'active'`,
		orgID).Scan(&v.PlanName, &v.Status, &v.MaxConcurrentSlots, &v.MonthlyBudgetUSD,
		&v.AutoApproveThresholdUSD, &v.PeriodStart, &v.PeriodEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionView{}, ErrNoSubscription
	}
	if err != nil {
		return SubscriptionView{}, err
	}

	if err := s.pool.QueryRow(ctx, `
		select coalesce(sum(cost_usd), 0) from usage_records
		where org_id = $1 and recorded_at >= $2 and recorded_at < $3`,
		orgID, v.PeriodStart, v.PeriodEnd).Scan(&v.BudgetUsedUSD); err != nil {
		return SubscriptionView{}, err
	}
	if err := s.pool.QueryRow(ctx, `
		select coalesce(sum(estimated_cost_usd), 0) from tasks
		where org_id = $1 and status in ('submitted', 'queued', 'running', 'awaiting_approval')`,
		orgID).Scan(&v.BudgetReservedUSD); err != nil {
		return SubscriptionView{}, err
	}
	v.BudgetRemainingUSD = v.MonthlyBudgetUSD - v.BudgetUsedUSD - v.BudgetReservedUSD
	return v, nil
}

func (s *Service) ListInvoices(ctx context.Context, orgID string) ([]Invoice, error) {
	rows, err := s.pool.Query(ctx,
		`select `+invoiceCols+` from invoices where org_id = $1 order by period_start desc`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Invoice, 0)
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(&inv.ID, &inv.PeriodStart, &inv.PeriodEnd, &inv.TotalUSD,
			&inv.LineCount, &inv.Status, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Service) GetInvoice(ctx context.Context, orgID, id string) (Invoice, error) {
	var inv Invoice
	err := s.pool.QueryRow(ctx,
		`select `+invoiceCols+` from invoices where org_id = $1 and id = $2`, orgID, id).
		Scan(&inv.ID, &inv.PeriodStart, &inv.PeriodEnd, &inv.TotalUSD,
			&inv.LineCount, &inv.Status, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrNotFound
	}
	return inv, err
}
