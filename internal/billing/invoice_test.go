package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateInvoiceConcurrent(t *testing.T) {
	pool := newTestPool(t)
	orgID := setupBillingFixtures(t, pool)
	svc := NewService(pool)

	periodStart := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	const n = 2
	invs := make([]Invoice, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			invs[i], _, errs[i] = svc.Generate(context.Background(), orgID, periodStart, periodEnd)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoErrorf(t, errs[i], "generation %d failed", i)
	}
	assert.Equal(t, invs[0].ID, invs[1].ID, "оба вызова должны вернуть один и тот же инвойс")
	assert.Equal(t, invs[0].TotalUSD, invs[1].TotalUSD)

	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`select count(*) from invoices where org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 1, count, "в базе ровно один инвойс за период")
}

// --- helpers ---

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://agenthub:agenthub@localhost:5432/agenthub?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("no test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("test database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func setupBillingFixtures(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	sfx := hex.EncodeToString(b)

	var orgID string
	require.NoError(t, pool.QueryRow(ctx,
		`insert into organizations (name) values ($1) returning id`, "org-"+sfx).Scan(&orgID))

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from invoices where org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `delete from organizations where id = $1`, orgID)
	})
	return orgID
}
