package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/audit"
	"github.com/miroslavrov/be-testcase-2026/internal/domain"
)

type stepState int

const (
	stepRun stepState = iota
	stepDone
	stepSkip
	stepAbandoned
)

type currentCall struct {
	ID        string
	ToolName  string
	Estimated float64
	Mock      ToolMock
}

// runExecution гонит задачу по tool call'ам до конца
func (w *Worker) runExecution(ctx context.Context, c *claimed) {
	for {
		call, state, err := w.nextCall(ctx, c)
		if err != nil {
			slog.Error("next call failed", "task", c.TaskID, "err", err)
			return
		}
		switch state {
		case stepDone:
			w.finish(ctx, c, true, "")
			return
		case stepSkip:
			continue
		case stepAbandoned:
			// задачу отменили пока мы работали, слот освободили без нас
			return
		}

		res := RunMockTool(ctx, call.Mock)
		if ctx.Err() != nil {
			// нас гасят: ничего не дописываем, лиз протухнет и задачу подберут
			return
		}

		abandoned, err := w.recordCall(ctx, c, call, res)
		if err != nil {
			slog.Error("record call failed", "task", c.TaskID, "err", err)
			return
		}
		if abandoned {
			return
		}
		if !res.OK {
			w.finish(ctx, c, false, "tool call failed: "+call.ToolName)
			return
		}
	}
}

// nextCall продлевает лиз и берёт следующий вызов под исполнение
func (w *Worker) nextCall(ctx context.Context, c *claimed) (currentCall, stepState, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return currentCall{}, stepRun, err
	}
	defer tx.Rollback(ctx)

	var (
		execStatus string
		idx        int
	)
	err = tx.QueryRow(ctx,
		`select status, current_call_index from task_executions where id = $1 for update`,
		c.ExecutionID).Scan(&execStatus, &idx)
	if err != nil {
		return currentCall{}, stepRun, err
	}
	if execStatus != domain.TaskRunning {
		return currentCall{}, stepAbandoned, nil
	}

	if _, err := tx.Exec(ctx,
		`update task_executions set lease_expires_at = now() + interval '2 minutes', updated_at = now() where id = $1`,
		c.ExecutionID); err != nil {
		return currentCall{}, stepRun, err
	}

	var (
		call       currentCall
		callStatus string
	)
	err = tx.QueryRow(ctx, `
		select c.id, c.status, c.estimated_cost_usd, t.name, t.mock_min_ms, t.mock_max_ms, t.mock_failure_rate
		from tool_calls c
		join tool_definitions t on t.id = c.tool_id
		where c.task_id = $1 and c.order_index = $2`,
		c.TaskID, idx).Scan(&call.ID, &callStatus, &call.Estimated, &call.ToolName,
		&call.Mock.MinMs, &call.Mock.MaxMs, &call.Mock.FailureRate)
	if errors.Is(err, pgx.ErrNoRows) {
		// вызовы кончились — задача готова
		return currentCall{}, stepDone, tx.Commit(ctx)
	}
	if err != nil {
		return currentCall{}, stepRun, err
	}

	if callStatus == domain.CallCompleted {
		// уже сделан (например после падения воркера на самом финише) — просто шагаем дальше
		if _, err := tx.Exec(ctx,
			`update task_executions set current_call_index = $2 where id = $1`,
			c.ExecutionID, idx+1); err != nil {
			return currentCall{}, stepRun, err
		}
		return currentCall{}, stepSkip, tx.Commit(ctx)
	}

	// TODO: тут встанет проверка согласования, пока всё идёт как auto_approved

	if _, err := tx.Exec(ctx,
		`update tool_calls set status = 'executing', started_at = coalesce(started_at, now()) where id = $1`,
		call.ID); err != nil {
		return currentCall{}, stepRun, err
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "tool_call", call.ID, callStatus, domain.CallExecuting, "worker", ""); err != nil {
		return currentCall{}, stepRun, err
	}

	return call, stepRun, tx.Commit(ctx)
}

// recordCall фиксирует результат вызова и двигает индекс
func (w *Worker) recordCall(ctx context.Context, c *claimed, call currentCall, res ToolResult) (abandoned bool, err error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var (
		execStatus string
		idx        int
	)
	err = tx.QueryRow(ctx,
		`select status, current_call_index from task_executions where id = $1 for update`,
		c.ExecutionID).Scan(&execStatus, &idx)
	if err != nil {
		return false, err
	}
	if execStatus != domain.TaskRunning {
		return true, nil
	}

	body, _ := json.Marshal(res)

	if res.OK {
		actual := round2(call.Estimated * (0.9 + rand.Float64()*0.2))
		if _, err := tx.Exec(ctx, `
			update tool_calls set status = 'completed', actual_cost_usd = $2, result = $3, finished_at = now()
			where id = $1`,
			call.ID, actual, body); err != nil {
			return false, err
		}
		// конфликт по tool_call_id молча гасим: если call перезапускался, второго списания не будет
		if _, err := tx.Exec(ctx, `
			insert into usage_records (org_id, task_id, tool_call_id, cost_usd)
			values ($1, $2, $3, $4)
			on conflict (tool_call_id) do nothing`,
			c.OrgID, c.TaskID, call.ID, actual); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx,
			`update task_executions set current_call_index = $2 where id = $1`,
			c.ExecutionID, idx+1); err != nil {
			return false, err
		}
		if err := audit.Transition(ctx, tx, c.OrgID, "tool_call", call.ID, domain.CallExecuting, domain.CallCompleted, "worker", ""); err != nil {
			return false, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			update tool_calls set status = 'failed', result = $2, finished_at = now()
			where id = $1`,
			call.ID, body); err != nil {
			return false, err
		}
		if err := audit.Transition(ctx, tx, c.OrgID, "tool_call", call.ID, domain.CallExecuting, domain.CallFailed, "worker", ""); err != nil {
			return false, err
		}
	}

	return false, tx.Commit(ctx)
}

// finish закрывает исполнение, освобождает слот и ставит финальный статус задаче
func (w *Worker) finish(ctx context.Context, c *claimed, success bool, reason string) {
	execStatus, taskStatus := domain.TaskCompleted, domain.TaskCompleted
	if !success {
		execStatus, taskStatus = domain.TaskFailed, domain.TaskFailed
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		slog.Error("finish begin failed", "task", c.TaskID, "err", err)
		return
	}
	defer tx.Rollback(ctx)

	// только если исполнение ещё наше: отмена могла закрыть его раньше
	var slotID string
	err = tx.QueryRow(ctx, `
		update task_executions set status = $2, finished_at = now(), updated_at = now()
		where id = $1 and status = 'running'
		returning slot_id`,
		c.ExecutionID, execStatus).Scan(&slotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Error("finish exec failed", "task", c.TaskID, "err", err)
		return
	}

	if err := FreeSlot(ctx, tx, slotID); err != nil {
		slog.Error("free slot failed", "task", c.TaskID, "err", err)
		return
	}
	if _, err := tx.Exec(ctx,
		`update tasks set status = $2, failure_reason = nullif($3, ''), updated_at = now() where id = $1`,
		c.TaskID, taskStatus, reason); err != nil {
		slog.Error("finish task failed", "task", c.TaskID, "err", err)
		return
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "execution", c.ExecutionID, domain.TaskRunning, execStatus, "worker", ""); err != nil {
		slog.Error("finish audit failed", "task", c.TaskID, "err", err)
		return
	}
	if err := audit.Transition(ctx, tx, c.OrgID, "task", c.TaskID, domain.TaskRunning, taskStatus, "worker", ""); err != nil {
		slog.Error("finish audit failed", "task", c.TaskID, "err", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Error("finish commit failed", "task", c.TaskID, "err", err)
		return
	}
	slog.Info("task finished", "task", c.TaskID, "status", taskStatus, "reason", reason)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
