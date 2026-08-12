package worker

import (
	"context"
	"math/rand/v2"
	"time"
)

type ToolMock struct {
	MinMs       int
	MaxMs       int
	FailureRate float64
}

type ToolResult struct {
	OK         bool   `json:"ok"`
	DurationMs int    `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// RunMockTool имитирует инструмент: спит случайное время и иногда падает
func RunMockTool(ctx context.Context, m ToolMock) ToolResult {
	dur := m.MinMs
	if m.MaxMs > m.MinMs {
		dur += rand.IntN(m.MaxMs - m.MinMs + 1)
	}

	select {
	case <-ctx.Done():
		return ToolResult{OK: false, Error: "cancelled"}
	case <-time.After(time.Duration(dur) * time.Millisecond):
	}

	if rand.Float64() < m.FailureRate {
		return ToolResult{OK: false, DurationMs: dur, Error: "mock tool failure"}
	}
	return ToolResult{OK: true, DurationMs: dur}
}
