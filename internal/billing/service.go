package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound       = errors.New("invoice not found")
	ErrNoSubscription = errors.New("no active subscription")
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type Invoice struct {
	ID          string    `json:"id"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	TotalUSD    float64   `json:"total_usd"`
	LineCount   int       `json:"line_count"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

const invoiceCols = `id, period_start, period_end, total_usd, line_count, status, created_at`

// Generate строит инвойс по usage_records за период.
// гонку двух генераторов решает уникальный индекс: проигравший просто получает уже созданный инвойс
func (s *Service) Generate(ctx context.Context, orgID string, periodStart, periodEnd time.Time) (Invoice, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invoice{}, false, err
	}
	defer tx.Rollback(ctx)

	var (
		total float64
		count int
	)
	if err := tx.QueryRow(ctx, `
		select coalesce(sum(cost_usd), 0), count(*)
		from usage_records
		where org_id = $1 and recorded_at >= $2 and recorded_at < $3`,
		orgID, periodStart, periodEnd).Scan(&total, &count); err != nil {
		return Invoice{}, false, err
	}

	var inv Invoice
	err = tx.QueryRow(ctx, `
		insert into invoices (org_id, period_start, period_end, total_usd, line_count)
		values ($1, $2, $3, $4, $5)
		on conflict (org_id, period_start) do nothing
		returning `+invoiceCols,
		orgID, periodStart, periodEnd, total, count).
		Scan(&inv.ID, &inv.PeriodStart, &inv.PeriodEnd, &inv.TotalUSD, &inv.LineCount, &inv.Status, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// конфликт: инвойс за период уже есть, отдаём его
		err = tx.QueryRow(ctx,
			`select `+invoiceCols+` from invoices where org_id = $1 and period_start = $2`,
			orgID, periodStart).
			Scan(&inv.ID, &inv.PeriodStart, &inv.PeriodEnd, &inv.TotalUSD, &inv.LineCount, &inv.Status, &inv.CreatedAt)
		if err != nil {
			return Invoice{}, false, err
		}
		return inv, false, tx.Commit(ctx)
	}
	if err != nil {
		return Invoice{}, false, err
	}
	return inv, true, tx.Commit(ctx)
}
