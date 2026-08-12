package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunMockToolAlwaysFails(t *testing.T) {
	res := RunMockTool(context.Background(), ToolMock{MinMs: 1, MaxMs: 2, FailureRate: 1})
	assert.False(t, res.OK)
	assert.Equal(t, "mock tool failure", res.Error)
}

func TestRunMockToolNeverFails(t *testing.T) {
	res := RunMockTool(context.Background(), ToolMock{MinMs: 1, MaxMs: 2, FailureRate: 0})
	assert.True(t, res.OK)
	assert.GreaterOrEqual(t, res.DurationMs, 1)
	assert.LessOrEqual(t, res.DurationMs, 2)
}

func TestRunMockToolRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	res := RunMockTool(ctx, ToolMock{MinMs: 5000, MaxMs: 5000, FailureRate: 0})
	assert.False(t, res.OK)
	assert.Equal(t, "cancelled", res.Error)
	assert.Less(t, time.Since(start), time.Second)
}
