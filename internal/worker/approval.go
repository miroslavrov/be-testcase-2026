package worker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/audit"
	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

// needsApproval повторяет правила из тз: read всегда авто, дешевле порога авто, иначе согласование
func needsApproval(riskLevel string, cost, threshold float64) bool {
	if riskLevel == domain.RiskRead {
		return false
	}
	return cost >= threshold
}

// raiseApproval заводит запрос на согласование и паркует задачу на первом шаге цепочки.
// raised=false — цепочки под этот риск нет, значит исполняем без согласования
func raiseApproval(ctx context.Context, tx pgx.Tx, c *claimed, callID, riskLevel, fromCallStatus string) (bool, error) {
	var (
		chainID   string
		stepOrder int
		timeoutH  float64
	)
	err := tx.QueryRow(ctx, `
		select ch.id, st.step_order, st.timeout_hours
		from approval_chains ch
		join approval_chain_steps st on st.chain_id = ch.id
		where ch.org_id = $1 and ch.risk_level = $2
		order by st.step_order
		limit 1`, c.OrgID, riskLevel).Scan(&chainID, &stepOrder, &timeoutH)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx, `
		insert into approval_requests (org_id, tool_call_id, chain_id, current_step_order, current_step_deadline, status)
		values ($1, $2, $3, $4, now() + (interval '1 hour' * $5), 'pending')`,
		c.OrgID, callID, chainID, stepOrder, timeoutH); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`update tool_calls set status = 'awaiting_approval' where id = $1`, callID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`update tasks set status = 'awaiting_approval', updated_at = now() where id = $1`, c.TaskID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`update task_executions set status = 'awaiting_approval', updated_at = now() where id = $1`, c.ExecutionID); err != nil {
		return false, err
	}

	if err := audit.Transition(ctx, tx, c.OrgID, "tool_call", callID, fromCallStatus, domain.CallAwaitingApproval, "worker", ""); err != nil {
		return false, err
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "task", c.TaskID, domain.TaskRunning, domain.TaskAwaitingApproval, "worker", ""); err != nil {
		return false, err
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "execution", c.ExecutionID, domain.TaskRunning, domain.TaskAwaitingApproval, "worker", ""); err != nil {
		return false, err
	}
	return true, nil
}

func approvalResolved(ctx context.Context, tx pgx.Tx, callID string) (approved, resolved bool, err error) {
	var status string
	err = tx.QueryRow(ctx,
		`select status from approval_requests where tool_call_id = $1`, callID).Scan(&status)
	if err != nil {
		return false, false, err
	}
	return status == "approved", status != "pending", nil
}
