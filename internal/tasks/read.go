package tasks

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

var ErrNotFound = errors.New("task not found")

type ListFilter struct {
	Status string
	Limit  int
	Offset int
}

type ToolCallView struct {
	ID               string          `json:"id"`
	ToolName         string          `json:"tool_name"`
	OrderIndex       int             `json:"order_index"`
	Status           string          `json:"status"`
	EstimatedCostUSD float64         `json:"estimated_cost_usd"`
	ActualCostUSD    *float64        `json:"actual_cost_usd"`
	Result           json.RawMessage `json:"result,omitempty"`
}

type TaskDetail struct {
	Task
	ToolCalls       []ToolCallView `json:"tool_calls"`
	CurrentToolCall *ToolCallView  `json:"current_tool_call"`
}

func (s *Service) List(ctx context.Context, orgID string, f ListFilter) ([]Task, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		select id, title, status, required_slot_type, priority, estimated_cost_usd, created_at
		from tasks
		where org_id = $1 and ($2 = '' or status = $2)
		order by created_at desc
		limit $3 offset $4`,
		orgID, f.Status, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Task, 0)
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Status, &t.RequiredSlotType, &t.Priority, &t.EstimatedCostUSD, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, orgID, taskID string) (TaskDetail, error) {
	var d TaskDetail
	err := s.pool.QueryRow(ctx, `
		select id, title, status, required_slot_type, priority, estimated_cost_usd, created_at
		from tasks
		where org_id = $1 and id = $2`,
		orgID, taskID).Scan(&d.ID, &d.Title, &d.Status, &d.RequiredSlotType, &d.Priority, &d.EstimatedCostUSD, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskDetail{}, ErrNotFound
	}
	if err != nil {
		return TaskDetail{}, err
	}

	rows, err := s.pool.Query(ctx, `
		select c.id, t.name, c.order_index, c.status, c.estimated_cost_usd, c.actual_cost_usd, c.result
		from tool_calls c
		join tool_definitions t on t.id = c.tool_id
		where c.task_id = $1
		order by c.order_index`,
		taskID)
	if err != nil {
		return TaskDetail{}, err
	}
	defer rows.Close()

	d.ToolCalls = make([]ToolCallView, 0)
	for rows.Next() {
		var c ToolCallView
		if err := rows.Scan(&c.ID, &c.ToolName, &c.OrderIndex, &c.Status, &c.EstimatedCostUSD, &c.ActualCostUSD, &c.Result); err != nil {
			return TaskDetail{}, err
		}
		d.ToolCalls = append(d.ToolCalls, c)
	}
	if err := rows.Err(); err != nil {
		return TaskDetail{}, err
	}

	d.CurrentToolCall = currentCall(d.ToolCalls)
	return d, nil
}

// текущий вызов — первый ещё не завершённый по порядку
func currentCall(calls []ToolCallView) *ToolCallView {
	for i := range calls {
		switch calls[i].Status {
		case domain.CallPending, domain.CallAwaitingApproval, domain.CallExecuting:
			return &calls[i]
		}
	}
	return nil
}
