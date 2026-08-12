package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/miroslavrov/be-testcase-2026/internal/approvals"
	"github.com/miroslavrov/be-testcase-2026/internal/tasks"
	"github.com/miroslavrov/be-testcase-2026/internal/worker"
)

// сценарий из тз от начала до конца:
// read авто -> send_email ждёт -> approve -> deploy ждёт -> reject -> failed
// порог ставим 0.10 чтобы send_email реально попадал под согласование
func TestScenarioFromSpec(t *testing.T) {
	pool := newTestPool(t)
	f := setupScenario(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// два воркера как в проде, дерутся за работу честно
	for i := 0; i < 2; i++ {
		go worker.New(pool, "itest").Run(ctx, 50*time.Millisecond)
	}

	tasksSvc := tasks.NewService(pool)
	apprSvc := approvals.NewService(pool)

	task, err := tasksSvc.Create(ctx, f.orgID, f.memberID, "", tasks.CreateInput{
		Title:            "scenario",
		RequiredSlotType: "standard",
		Priority:         3,
		ToolCalls: []tasks.ToolCallInput{
			{ToolName: f.readTool, EstimatedCostUSD: cost(0.01)},
			{ToolName: f.emailTool, EstimatedCostUSD: cost(0.50)},
			{ToolName: f.deployTool, EstimatedCostUSD: cost(5.00)},
		},
	})
	require.NoError(t, err)

	// шаги 5-6: read прошёл сам, send_email встал на согласование
	waitFor(t, pool, func() bool {
		return callStatus(t, pool, task.ID, 0) == "completed" &&
			callStatus(t, pool, task.ID, 1) == "awaiting_approval"
	}, "read_db должен выполниться сам, send_email встать на согласование")
	assert.Equal(t, "awaiting_approval", taskStatus(t, pool, task.ID))

	// шаг 7: согласуем send_email — задача должна поехать дальше
	req1 := pendingRequest(t, pool, f.orgID)
	require.NoError(t, apprSvc.Approve(ctx, f.orgID, req1, f.approverID, "approver", ""))
	waitFor(t, pool, func() bool {
		return callStatus(t, pool, task.ID, 1) == "completed" &&
			callStatus(t, pool, task.ID, 2) == "awaiting_approval"
	}, "после approve send_email должен выполниться, deploy встать на согласование")

	// шаги 8-9: deploy ждёт, отклоняем — задача падает
	req2 := pendingRequest(t, pool, f.orgID)
	require.NotEqual(t, req1, req2)
	require.NoError(t, apprSvc.Reject(ctx, f.orgID, req2, f.approverID, "approver", "слишком дорого"))
	waitFor(t, pool, func() bool {
		return taskStatus(t, pool, task.ID) == "failed"
	}, "после reject задача должна перейти в failed")
	assert.Equal(t, "rejected", callStatus(t, pool, task.ID, 2))

	// шаг 10: usage только по двум завершённым вызовам
	var usage int
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from usage_records where task_id = $1`, task.ID).Scan(&usage))
	assert.Equal(t, 2, usage)

	// слот вернулся в пул
	var busy int
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from agent_slots where org_id = $1 and status = 'busy'`, f.orgID).Scan(&busy))
	assert.Equal(t, 0, busy)

	// шаг 11: журнал отражает полный путь задачи
	assert.Equal(t,
		[]string{"submitted", "queued", "running", "awaiting_approval", "running", "awaiting_approval", "failed"},
		taskPath(t, pool, task.ID))
}

func cost(v float64) *float64 { return &v }
