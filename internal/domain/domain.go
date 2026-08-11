package domain

// статусы задачи
const (
	TaskSubmitted        = "submitted"
	TaskQueued           = "queued"
	TaskRunning          = "running"
	TaskAwaitingApproval = "awaiting_approval"
	TaskCompleted        = "completed"
	TaskFailed           = "failed"
	TaskCancelled        = "cancelled"
)

// статусы tool call
const (
	CallPending          = "pending"
	CallAwaitingApproval = "awaiting_approval"
	CallExecuting        = "executing"
	CallCompleted        = "completed"
	CallFailed           = "failed"
	CallRejected         = "rejected"
)

// уровни риска инструмента
const (
	RiskRead        = "read"
	RiskWrite       = "write"
	RiskDestructive = "destructive"
)

// статусы, при которых задача ещё занимает бюджет (не завершена)
func ActiveTaskStatuses() []string {
	return []string{TaskSubmitted, TaskQueued, TaskRunning, TaskAwaitingApproval}
}
