package tasks

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

func (s *Service) Cancel(ctx context.Context, orgID, userID, taskID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`select status from tasks where org_id = $1 and id = $2 for update`,
		orgID, taskID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if isTerminal(status) {
		return ErrNotCancellable
	}

	// если задача уже на слоте — снимаем исполнение и освобождаем слот
	if err := releaseExecution(ctx, tx, orgID, taskID, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`update tasks set status = $2, updated_at = now() where id = $1`,
		taskID, domain.TaskCancelled); err != nil {
		return err
	}
	if err := recordTransition(ctx, tx, orgID, "task", taskID, status, domain.TaskCancelled, "user", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func releaseExecution(ctx context.Context, tx pgx.Tx, orgID, taskID, userID string) error {
	var (
		execID string
		slotID string
	)
	err := tx.QueryRow(ctx, `
		select id, slot_id from task_executions
		where task_id = $1 and status in ('running', 'awaiting_approval')
		for update`, taskID).Scan(&execID, &slotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`update task_executions set status = $2, finished_at = now(), updated_at = now() where id = $1`,
		execID, domain.TaskCancelled); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`update agent_slots set status = 'available', updated_at = now() where id = $1`,
		slotID); err != nil {
		return err
	}
	return recordTransition(ctx, tx, orgID, "execution", execID, domain.TaskRunning, domain.TaskCancelled, "user", userID)
}

func isTerminal(status string) bool {
	return slices.Contains([]string{domain.TaskCompleted, domain.TaskFailed, domain.TaskCancelled}, status)
}
