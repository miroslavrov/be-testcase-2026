package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

var (
	ErrValidation     = errors.New("validation")
	ErrUnknownTool    = errors.New("unknown tool")
	ErrNoSubscription = errors.New("no active subscription")
	ErrBudgetExceeded = errors.New("budget exceeded")
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type ToolCallInput struct {
	ToolName         string          `json:"tool_name"`
	InputParams      json.RawMessage `json:"input_params"`
	EstimatedCostUSD *float64        `json:"estimated_cost_usd"`
}

type CreateInput struct {
	Title            string          `json:"title"`
	RequiredSlotType string          `json:"required_slot_type"`
	Priority         int             `json:"priority"`
	ToolCalls        []ToolCallInput `json:"tool_calls"`
}

type Task struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	RequiredSlotType string    `json:"required_slot_type"`
	Priority         int       `json:"priority"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Service) Create(ctx context.Context, orgID, userID string, in CreateInput) (Task, error) {
	if err := in.validate(); err != nil {
		return Task{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)

	// лочим строку подписки: два инстанса api не смогут одновременно посчитать бюджет
	// и оба проскочить лимит — второй ждёт коммита первого и видит уже учтённую задачу
	var (
		periodStart, periodEnd time.Time
		budget                 float64
	)
	err = tx.QueryRow(ctx, `
		select s.current_period_start, s.current_period_end, p.monthly_budget_usd
		from subscriptions s
		join plans p on p.id = s.plan_id
		where s.org_id = $1 and s.status = 'active'
		for update of s`, orgID).Scan(&periodStart, &periodEnd, &budget)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNoSubscription
	}
	if err != nil {
		return Task{}, err
	}

	estimate, resolved, err := s.resolveCosts(ctx, tx, in.ToolCalls)
	if err != nil {
		return Task{}, err
	}

	used, committed, err := budgetUsage(ctx, tx, orgID, periodStart, periodEnd)
	if err != nil {
		return Task{}, err
	}
	if used+committed+estimate > budget {
		return Task{}, fmt.Errorf("%w: used %.2f + committed %.2f + task %.2f over budget %.2f",
			ErrBudgetExceeded, used, committed, estimate, budget)
	}

	var task Task
	err = tx.QueryRow(ctx, `
		insert into tasks (org_id, created_by, title, required_slot_type, priority, status, estimated_cost_usd)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, title, status, required_slot_type, priority, estimated_cost_usd, created_at`,
		orgID, userID, in.Title, in.RequiredSlotType, in.Priority, domain.TaskQueued, estimate,
	).Scan(&task.ID, &task.Title, &task.Status, &task.RequiredSlotType, &task.Priority, &task.EstimatedCostUSD, &task.CreatedAt)
	if err != nil {
		return Task{}, err
	}

	for i, c := range resolved {
		if _, err := tx.Exec(ctx, `
			insert into tool_calls (task_id, org_id, tool_id, order_index, input_params, estimated_cost_usd, status)
			values ($1, $2, $3, $4, $5, $6, $7)`,
			task.ID, orgID, c.toolID, i, c.params, c.cost, domain.CallPending); err != nil {
			return Task{}, err
		}
	}

	if err := recordTransition(ctx, tx, orgID, "task", task.ID, "", domain.TaskQueued, "user", userID); err != nil {
		return Task{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (in CreateInput) validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return fmt.Errorf("%w: title is required", ErrValidation)
	}
	if in.RequiredSlotType != "standard" && in.RequiredSlotType != "fast" {
		return fmt.Errorf("%w: required_slot_type must be standard or fast", ErrValidation)
	}
	if in.Priority < 1 || in.Priority > 5 {
		return fmt.Errorf("%w: priority must be between 1 and 5", ErrValidation)
	}
	if len(in.ToolCalls) == 0 {
		return fmt.Errorf("%w: at least one tool call is required", ErrValidation)
	}
	return nil
}

type resolvedCall struct {
	toolID string
	params []byte
	cost   float64
}

func (s *Service) resolveCosts(ctx context.Context, tx pgx.Tx, calls []ToolCallInput) (float64, []resolvedCall, error) {
	total := 0.0
	out := make([]resolvedCall, 0, len(calls))
	for _, c := range calls {
		var (
			toolID   string
			baseCost float64
		)
		err := tx.QueryRow(ctx,
			`select id, base_cost_usd from tool_definitions where name = $1`, c.ToolName).
			Scan(&toolID, &baseCost)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, fmt.Errorf("%w: %s", ErrUnknownTool, c.ToolName)
		}
		if err != nil {
			return 0, nil, err
		}

		cost := baseCost
		if c.EstimatedCostUSD != nil {
			cost = *c.EstimatedCostUSD
		}
		params := []byte(c.InputParams)
		if len(params) == 0 {
			params = []byte("{}")
		}
		total += cost
		out = append(out, resolvedCall{toolID: toolID, params: params, cost: cost})
	}
	return total, out, nil
}

func budgetUsage(ctx context.Context, tx pgx.Tx, orgID string, start, end time.Time) (used, committed float64, err error) {
	if err = tx.QueryRow(ctx,
		`select coalesce(sum(cost_usd), 0) from usage_records
		 where org_id = $1 and recorded_at >= $2 and recorded_at < $3`,
		orgID, start, end).Scan(&used); err != nil {
		return 0, 0, err
	}
	if err = tx.QueryRow(ctx,
		`select coalesce(sum(estimated_cost_usd), 0) from tasks
		 where org_id = $1 and status = any($2)`,
		orgID, domain.ActiveTaskStatuses()).Scan(&committed); err != nil {
		return 0, 0, err
	}
	return used, committed, nil
}

func recordTransition(ctx context.Context, tx pgx.Tx, orgID, entityType, entityID, from, to, actorType, actorID string) error {
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	var fromVal *string
	if from != "" {
		fromVal = &from
	}
	_, err := tx.Exec(ctx, `
		insert into state_transitions (org_id, entity_type, entity_id, from_status, to_status, actor_type, actor_id)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		orgID, entityType, entityID, fromVal, to, actorType, actor)
	return err
}
