package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func (store *Store) HostedNotificationSummary(ctx context.Context, activeIDs []string) (collabstore.HostedNotificationSummary, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	var summary collabstore.HostedNotificationSummary
	if store.closed {
		return summary, collabstore.ErrStoreClosed
	}
	if len(activeIDs) > collabstore.HostedActiveIDMaxCount {
		return summary, fmt.Errorf("too many active sessions for notification summary")
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return summary, err
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	err = transaction.QueryRow(ctx, `SELECT count(*) FILTER (WHERE visibility='community'), count(*) FILTER (WHERE visibility='private'), count(*) FILTER (WHERE visibility='private' AND session_id=ANY($1::text[])) FROM collaboration_hosted_sessions`, activeIDs).Scan(&summary.CommunityCount, &summary.PrivateCount, &summary.ActivePrivateCount)
	if err != nil {
		return summary, err
	}
	rows, err := transaction.Query(ctx, `SELECT session_id, title FROM collaboration_hosted_sessions WHERE visibility='community' ORDER BY session_id COLLATE "C" LIMIT 10`)
	if err != nil {
		return summary, err
	}
	for rows.Next() {
		session := collabstore.HostedSession{Visibility: collabstore.HostedVisibilityCommunity}
		if err := rows.Scan(&session.SessionID, &session.Title); err != nil {
			rows.Close()
			return summary, err
		}
		summary.Community = append(summary.Community, session)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return summary, err
	}
	return summary, transaction.Commit(ctx)
}
