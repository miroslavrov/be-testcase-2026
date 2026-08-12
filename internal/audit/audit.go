package audit

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Transition пишет строку в журнал переходов состояний
func Transition(ctx context.Context, tx pgx.Tx, orgID, entityType, entityID, from, to, actorType, actorID string) error {
	var fromVal, actor *string
	if from != "" {
		fromVal = &from
	}
	if actorID != "" {
		actor = &actorID
	}
	_, err := tx.Exec(ctx, `
		insert into state_transitions (org_id, entity_type, entity_id, from_status, to_status, actor_type, actor_id)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		orgID, entityType, entityID, fromVal, to, actorType, actor)
	return err
}
