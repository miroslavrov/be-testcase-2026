package approvals

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/audit"
	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

var (
	ErrNotActionable = errors.New("approval already resolved")
	ErrWrongRole     = errors.New("role cannot act on this step")
)

func (s *Service) Approve(ctx context.Context, orgID, id, approverID, approverRole, comment string) error {
	return s.decide(ctx, orgID, id, approverID, approverRole, comment, true)
}

func (s *Service) Reject(ctx context.Context, orgID, id, approverID, approverRole, comment string) error {
	return s.decide(ctx, orgID, id, approverID, approverRole, comment, false)
}

func (s *Service) decide(ctx context.Context, orgID, id, approverID, approverRole, comment string, approve bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		status    string
		chainID   string
		stepOrder int
		callID    string
		taskID    string
		stepRole  string
	)
	err = tx.QueryRow(ctx, `
		select ar.status, ar.chain_id, ar.current_step_order, ar.tool_call_id, c.task_id, st.approver_role
		from approval_requests ar
		join tool_calls c on c.id = ar.tool_call_id
		join approval_chain_steps st on st.chain_id = ar.chain_id and st.step_order = ar.current_step_order
		where ar.org_id = $1 and ar.id = $2
		for update of ar`, orgID, id).Scan(&status, &chainID, &stepOrder, &callID, &taskID, &stepRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrNotActionable
	}
	if approverRole != stepRole {
		return ErrWrongRole
	}

	decision := "rejected"
	if approve {
		decision = "approved"
	}
	if _, err := tx.Exec(ctx, `
		insert into approval_decisions (request_id, step_order, approver_id, decision, comment)
		values ($1, $2, $3, $4, nullif($5, ''))`,
		id, stepOrder, approverID, decision, comment); err != nil {
		return err
	}

	if !approve {
		if err := rejectFlow(ctx, tx, orgID, id, callID, taskID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// есть ли следующий шаг цепочки
	var (
		nextOrder   int
		nextTimeout float64
	)
	err = tx.QueryRow(ctx, `
		select step_order, timeout_hours from approval_chain_steps
		where chain_id = $1 and step_order > $2
		order by step_order limit 1`, chainID, stepOrder).Scan(&nextOrder, &nextTimeout)
	if err == nil {
		// переходим на следующий шаг, задача так и ждёт
		_, err = tx.Exec(ctx, `
			update approval_requests
			set current_step_order = $2, current_step_deadline = now() + (interval '1 hour' * $3)
			where id = $1`, id, nextOrder, nextTimeout)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// последний шаг согласован — возобновляем исполнение
	if err := resumeFlow(ctx, tx, orgID, id, taskID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rejectFlow(ctx context.Context, tx pgx.Tx, orgID, reqID, callID, taskID string) error {
	if _, err := tx.Exec(ctx,
		`update approval_requests set status = 'rejected', resolved_at = now() where id = $1`, reqID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`update tool_calls set status = 'rejected', finished_at = now() where id = $1`, callID); err != nil {
		return err
	}
	if err := audit.Transition(ctx, tx, orgID, "tool_call", callID, domain.CallAwaitingApproval, domain.CallRejected, "approver", ""); err != nil {
		return err
	}

	// снимаем исполнение и освобождаем слот
	var execID, slotID string
	err := tx.QueryRow(ctx,
		`select id, slot_id from task_executions where task_id = $1 and status = 'awaiting_approval' for update`,
		taskID).Scan(&execID, &slotID)
	if err == nil {
		if _, err := tx.Exec(ctx,
			`update task_executions set status = 'failed', finished_at = now(), updated_at = now() where id = $1`, execID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`update agent_slots set status = 'available', updated_at = now() where id = $1`, slotID); err != nil {
			return err
		}
		if err := audit.Transition(ctx, tx, orgID, "execution", execID, domain.TaskAwaitingApproval, domain.TaskFailed, "approver", ""); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	if _, err := tx.Exec(ctx, `
		update tasks set status = 'failed', failure_reason = 'approval rejected', updated_at = now()
		where id = $1 and status = 'awaiting_approval'`, taskID); err != nil {
		return err
	}
	return audit.Transition(ctx, tx, orgID, "task", taskID, domain.TaskAwaitingApproval, domain.TaskFailed, "approver", "")
}

func resumeFlow(ctx context.Context, tx pgx.Tx, orgID, reqID, taskID string) error {
	if _, err := tx.Exec(ctx,
		`update approval_requests set status = 'approved', resolved_at = now() where id = $1`, reqID); err != nil {
		return err
	}

	// лиз в now() — исполнение сразу становится подбираемым, его продолжит любой воркер
	var execID string
	err := tx.QueryRow(ctx, `
		update task_executions set status = 'running', lease_expires_at = now(), updated_at = now()
		where task_id = $1 and status = 'awaiting_approval'
		returning id`, taskID).Scan(&execID)
	if errors.Is(err, pgx.ErrNoRows) {
		// исполнения нет (задачу успели отменить) — просто закрываем запрос
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		update tasks set status = 'running', updated_at = now()
		where id = $1 and status = 'awaiting_approval'`, taskID); err != nil {
		return err
	}
	if err := audit.Transition(ctx, tx, orgID, "execution", execID, domain.TaskAwaitingApproval, domain.TaskRunning, "approver", ""); err != nil {
		return err
	}
	return audit.Transition(ctx, tx, orgID, "task", taskID, domain.TaskAwaitingApproval, domain.TaskRunning, "approver", "")
}
