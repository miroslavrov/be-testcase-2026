package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("approval not found")

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type Summary struct {
	ID               string    `json:"id"`
	ToolCallID       string    `json:"tool_call_id"`
	TaskID           string    `json:"task_id"`
	ToolName         string    `json:"tool_name"`
	RiskLevel        string    `json:"risk_level"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	CurrentStepOrder int       `json:"current_step_order"`
	CurrentStepRole  string    `json:"current_step_role"`
	Deadline         time.Time `json:"current_step_deadline"`
	CreatedAt        time.Time `json:"created_at"`
}

type Decision struct {
	StepOrder int       `json:"step_order"`
	Decision  string    `json:"decision"`
	Comment   *string   `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

type Detail struct {
	Summary
	InputParams json.RawMessage `json:"input_params"`
	Decisions   []Decision      `json:"decisions"`
}

const summaryCols = `
	ar.id, ar.tool_call_id, c.task_id, t.name, t.risk_level, c.estimated_cost_usd,
	ar.current_step_order, st.approver_role, ar.current_step_deadline, ar.created_at`

func (s *Service) List(ctx context.Context, orgID string) ([]Summary, error) {
	rows, err := s.pool.Query(ctx, `
		select `+summaryCols+`
		from approval_requests ar
		join tool_calls c on c.id = ar.tool_call_id
		join tool_definitions t on t.id = c.tool_id
		join approval_chain_steps st on st.chain_id = ar.chain_id and st.step_order = ar.current_step_order
		where ar.org_id = $1 and ar.status = 'pending'
		order by ar.created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Summary, 0)
	for rows.Next() {
		var a Summary
		if err := rows.Scan(&a.ID, &a.ToolCallID, &a.TaskID, &a.ToolName, &a.RiskLevel,
			&a.EstimatedCostUSD, &a.CurrentStepOrder, &a.CurrentStepRole, &a.Deadline, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Detail, error) {
	var d Detail
	err := s.pool.QueryRow(ctx, `
		select `+summaryCols+`, c.input_params
		from approval_requests ar
		join tool_calls c on c.id = ar.tool_call_id
		join tool_definitions t on t.id = c.tool_id
		join approval_chain_steps st on st.chain_id = ar.chain_id and st.step_order = ar.current_step_order
		where ar.org_id = $1 and ar.id = $2`,
		orgID, id).Scan(&d.ID, &d.ToolCallID, &d.TaskID, &d.ToolName, &d.RiskLevel,
		&d.EstimatedCostUSD, &d.CurrentStepOrder, &d.CurrentStepRole, &d.Deadline, &d.CreatedAt, &d.InputParams)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}

	rows, err := s.pool.Query(ctx, `
		select step_order, decision, comment, created_at
		from approval_decisions
		where request_id = $1
		order by created_at`, id)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()

	d.Decisions = make([]Decision, 0)
	for rows.Next() {
		var dec Decision
		if err := rows.Scan(&dec.StepOrder, &dec.Decision, &dec.Comment, &dec.CreatedAt); err != nil {
			return Detail{}, err
		}
		d.Decisions = append(d.Decisions, dec)
	}
	return d, rows.Err()
}
