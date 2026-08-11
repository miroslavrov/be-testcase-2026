package worker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrNoFreeSlot     = errors.New("no free slot")
	ErrSlotLimit      = errors.New("plan slot limit reached")
	ErrNoSubscription = errors.New("no active subscription")
)

// ReserveSlot захватывает свободный слот нужного типа внутри переданной транзакции.
// два воркера не могут взять один слот: skip locked просто пропускает строку,
// которую уже держит другая транзакция, вместо того чтобы ждать её
func ReserveSlot(ctx context.Context, tx pgx.Tx, orgID, slotType string) (string, error) {
	// лок подписки сериализует проверку лимита плана в рамках орги
	var maxSlots int
	err := tx.QueryRow(ctx, `
		select p.max_concurrent_slots
		from subscriptions s
		join plans p on p.id = s.plan_id
		where s.org_id = $1 and s.status = 'active'
		for update of s`, orgID).Scan(&maxSlots)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSubscription
	}
	if err != nil {
		return "", err
	}

	var busy int
	if err := tx.QueryRow(ctx,
		`select count(*) from agent_slots where org_id = $1 and status = 'busy'`,
		orgID).Scan(&busy); err != nil {
		return "", err
	}
	if busy >= maxSlots {
		return "", ErrSlotLimit
	}

	var slotID string
	err = tx.QueryRow(ctx, `
		select id from agent_slots
		where org_id = $1 and slot_type = $2 and status = 'available'
		order by id
		for update skip locked
		limit 1`, orgID, slotType).Scan(&slotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoFreeSlot
	}
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx,
		`update agent_slots set status = 'busy', updated_at = now() where id = $1`,
		slotID); err != nil {
		return "", err
	}
	return slotID, nil
}

// FreeSlot возвращает слот в пул
func FreeSlot(ctx context.Context, tx pgx.Tx, slotID string) error {
	_, err := tx.Exec(ctx,
		`update agent_slots set status = 'available', updated_at = now() where id = $1`,
		slotID)
	return err
}
