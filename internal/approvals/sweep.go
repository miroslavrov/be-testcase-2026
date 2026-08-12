package approvals

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// SweepExpired обрабатывает все просроченные шаги согласования.
// безопасно на нескольких инстансах: каждый запрос берётся под skip locked
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	count := 0
	for {
		done, err := s.sweepOne(ctx)
		if err != nil {
			return count, err
		}
		if !done {
			return count, nil
		}
		count++
	}
}

func (s *Service) sweepOne(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var (
		reqID     string
		orgID     string
		chainID   string
		stepOrder int
		callID    string
		taskID    string
		onTimeout string
	)
	err = tx.QueryRow(ctx, `
		select ar.id, ar.org_id, ar.chain_id, ar.current_step_order, ar.tool_call_id, c.task_id, st.on_timeout
		from approval_requests ar
		join tool_calls c on c.id = ar.tool_call_id
		join approval_chain_steps st on st.chain_id = ar.chain_id and st.step_order = ar.current_step_order
		where ar.status = 'pending' and ar.current_step_deadline < now()
		order by ar.current_step_deadline
		for update of ar skip locked
		limit 1`).Scan(&reqID, &orgID, &chainID, &stepOrder, &callID, &taskID, &onTimeout)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var (
		nextOrder   int
		nextTimeout float64
	)
	hasNext := false
	err = tx.QueryRow(ctx, `
		select step_order, timeout_hours from approval_chain_steps
		where chain_id = $1 and step_order > $2
		order by step_order limit 1`, chainID, stepOrder).Scan(&nextOrder, &nextTimeout)
	if err == nil {
		hasNext = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}

	// эскалация возможна только если есть следующий шаг, иначе отклоняем
	if onTimeout == "escalate" && hasNext {
		if _, err := tx.Exec(ctx, `
			insert into approval_decisions (request_id, step_order, decision)
			values ($1, $2, 'timeout_escalated')`, reqID, stepOrder); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `
			update approval_requests
			set current_step_order = $2, current_step_deadline = now() + (interval '1 hour' * $3)
			where id = $1`, reqID, nextOrder, nextTimeout); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		insert into approval_decisions (request_id, step_order, decision)
		values ($1, $2, 'timeout_rejected')`, reqID, stepOrder); err != nil {
		return false, err
	}
	if err := rejectFlow(ctx, tx, orgID, reqID, callID, taskID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
