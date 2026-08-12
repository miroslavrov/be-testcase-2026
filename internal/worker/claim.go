package worker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/audit"
	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

type claimed struct {
	ExecutionID string
	TaskID      string
	OrgID       string
	SlotID      string
}

// claimResumable берёт исполнение, которое числится running, но лиз протух.
// сюда попадают и упавшие воркеры (crash recovery), и возобновлённые после approve
func (w *Worker) claimResumable(ctx context.Context) (*claimed, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var c claimed
	err = tx.QueryRow(ctx, `
		select id, task_id, org_id, slot_id
		from task_executions
		where status = 'running' and lease_expires_at < now()
		order by lease_expires_at
		for update skip locked
		limit 1`).Scan(&c.ExecutionID, &c.TaskID, &c.OrgID, &c.SlotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`update task_executions set lease_expires_at = now() + interval '2 minutes', updated_at = now() where id = $1`,
		c.ExecutionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &c, nil
}

// claimQueued берёт одну задачу из очереди и садит её на слот.
// nil без ошибки — брать нечего
func (w *Worker) claimQueued(ctx context.Context) (*claimed, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var (
		c        claimed
		slotType string
	)
	// exists по слотам, чтобы задача без свободного слота не блокировала очередь другим оргам
	err = tx.QueryRow(ctx, `
		select t.id, t.org_id, t.required_slot_type
		from tasks t
		where t.status = 'queued'
		  and exists (
			select 1 from agent_slots s
			where s.org_id = t.org_id and s.slot_type = t.required_slot_type and s.status = 'available'
		  )
		order by t.priority desc, t.created_at
		for update of t skip locked
		limit 1`).Scan(&c.TaskID, &c.OrgID, &slotType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	slotID, err := ReserveSlot(ctx, tx, c.OrgID, slotType)
	if errors.Is(err, ErrNoFreeSlot) || errors.Is(err, ErrSlotLimit) {
		// слот увели прямо из под носа, задача останется в очереди
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.SlotID = slotID

	err = tx.QueryRow(ctx, `
		insert into task_executions (task_id, org_id, slot_id, status, lease_expires_at)
		values ($1, $2, $3, 'running', now() + interval '2 minutes')
		returning id`,
		c.TaskID, c.OrgID, c.SlotID).Scan(&c.ExecutionID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`update tasks set status = 'running', updated_at = now() where id = $1`, c.TaskID); err != nil {
		return nil, err
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "task", c.TaskID, domain.TaskQueued, domain.TaskRunning, "worker", ""); err != nil {
		return nil, err
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "execution", c.ExecutionID, "", domain.TaskRunning, "worker", ""); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &c, nil
}
