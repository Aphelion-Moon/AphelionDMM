package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

const hostedSessionSelect = `SELECT hosted_session.session_id, hosted_session.document_id, hosted_session.created_at, hosted_session.visibility, hosted_session.title, hosted_session.map_label, hosted_session.environment_label, COALESCE(hosted_owner.display_name, '')`

const hostedSessionFrom = `FROM collaboration_hosted_sessions AS hosted_session
	JOIN collaboration_documents AS hosted_document ON hosted_document.document_id = hosted_session.document_id
	LEFT JOIN LATERAL (
		SELECT display_name FROM collaboration_hosted_members
		WHERE session_id = hosted_session.session_id AND role = 'owner'
		ORDER BY actor_id COLLATE "C" LIMIT 1
	) AS hosted_owner ON TRUE`

func (store *Store) CreateHostedSession(ctx context.Context, session collabstore.HostedSession, owner collabstore.HostedMember) error {
	metadata, err := collabstore.NormalizeHostedSessionMetadata(hostedSessionMetadata(session))
	if err != nil {
		return err
	}
	applyHostedSessionMetadata(&session, metadata)
	if err := validateHostedSession(session, owner); err != nil {
		return err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin hosted session create: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err := transaction.Exec(ctx, `INSERT INTO collaboration_hosted_sessions(session_id, document_id, created_at, visibility, title, map_label, environment_label) VALUES($1, $2, $3, $4, $5, $6, $7)`, session.SessionID, session.DocumentID, session.CreatedAt, session.Visibility, session.Title, session.MapLabel, session.EnvironmentLabel); err != nil {
		if postgresCode(err) == "23505" {
			return collabstore.ErrHostedSessionExists
		}
		return fmt.Errorf("insert hosted session: %w", err)
	}
	if err := insertHostedMember(ctx, transaction, owner); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit hosted session create: %w", err)
	}
	return nil
}

func (store *Store) EndHostedSession(ctx context.Context, sessionID string) error {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	// Memberships and invitations cascade; the parent document is retained.
	_, err := store.pool.Exec(ctx, `DELETE FROM collaboration_hosted_sessions WHERE session_id = $1`, sessionID)
	return err
}

func (store *Store) ListHostedSessions(ctx context.Context) ([]collabstore.HostedSession, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return nil, collabstore.ErrStoreClosed
	}
	rows, err := store.pool.Query(ctx, hostedSessionSelect+" "+hostedSessionFrom+` ORDER BY hosted_session.session_id COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("list hosted sessions: %w", err)
	}
	defer rows.Close()
	var sessions []collabstore.HostedSession
	for rows.Next() {
		session, err := scanHostedSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan hosted session: %w", err)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (store *Store) ListCommunityHostedSessions(ctx context.Context, actorID model.ActorID, activeSessionIDs []string, request collabstore.HostedSessionPageRequest) (collabstore.HostedSessionPage, error) {
	page := collabstore.HostedSessionPage{}
	if err := actorID.Validate(); err != nil {
		return page, err
	}
	request, err := normalizeHostedSessionPageRequest(request)
	if err != nil {
		return page, err
	}
	activeSessionIDs, err = normalizeHostedActiveIDs(activeSessionIDs)
	if err != nil {
		return page, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return page, collabstore.ErrStoreClosed
	}
	if len(activeSessionIDs) == 0 {
		return page, nil
	}
	query := hostedSessionSelect + " " + hostedSessionFrom + `
	WHERE hosted_session.visibility = 'community'
	  AND hosted_session.session_id = ANY($1::text[])
	  AND ($2 = '' OR hosted_session.session_id COLLATE "C" > $2 COLLATE "C")
	  AND NOT EXISTS (
		SELECT 1 FROM collaboration_hosted_members AS requester
		WHERE requester.session_id = hosted_session.session_id AND requester.actor_id = $3 AND requester.disabled
	  )
	ORDER BY hosted_session.session_id COLLATE "C"
	LIMIT $4`
	rows, err := store.pool.Query(ctx, query, activeSessionIDs, request.Cursor, actorID, request.Limit+1)
	if err != nil {
		return page, fmt.Errorf("list active community hosted sessions: %w", err)
	}
	return readHostedSessionPage(rows, request.Limit)
}

func (store *Store) ListMyHostedSessions(ctx context.Context, actorID model.ActorID, request collabstore.HostedSessionPageRequest) (collabstore.HostedSessionPage, error) {
	page := collabstore.HostedSessionPage{}
	if err := actorID.Validate(); err != nil {
		return page, err
	}
	request, err := normalizeHostedSessionPageRequest(request)
	if err != nil {
		return page, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return page, collabstore.ErrStoreClosed
	}
	query := hostedSessionSelect + " " + hostedSessionFrom + `
	JOIN collaboration_hosted_members AS requester
	  ON requester.session_id = hosted_session.session_id AND requester.actor_id = $1 AND NOT requester.disabled
	WHERE ($2 = '' OR hosted_session.session_id COLLATE "C" > $2 COLLATE "C")
	ORDER BY hosted_session.session_id COLLATE "C"
	LIMIT $3`
	rows, err := store.pool.Query(ctx, query, actorID, request.Cursor, request.Limit+1)
	if err != nil {
		return page, fmt.Errorf("list member hosted sessions: %w", err)
	}
	return readHostedSessionPage(rows, request.Limit)
}

func (store *Store) UpdateHostedSessionMetadata(ctx context.Context, sessionID string, ownerActorID model.ActorID, metadata collabstore.HostedSessionMetadata) (collabstore.HostedSession, error) {
	if sessionID == "" || len(sessionID) > 128 {
		return collabstore.HostedSession{}, fmt.Errorf("hosted session ID is invalid")
	}
	if err := ownerActorID.Validate(); err != nil {
		return collabstore.HostedSession{}, err
	}
	metadata, err := collabstore.NormalizeHostedSessionMetadata(metadata)
	if err != nil {
		return collabstore.HostedSession{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.HostedSession{}, collabstore.ErrStoreClosed
	}
	var lastErr error
	for attempt := 0; attempt < maxTransactionAttempts; attempt++ {
		session, err := store.updateHostedSessionMetadataOnce(ctx, sessionID, ownerActorID, metadata)
		if err == nil || !IsRetryable(err) {
			return session, err
		}
		lastErr = err
		if ctx.Err() != nil {
			return collabstore.HostedSession{}, ctx.Err()
		}
	}
	return collabstore.HostedSession{}, fmt.Errorf("update hosted session metadata after %d attempts: %w", maxTransactionAttempts, lastErr)
}

func (store *Store) updateHostedSessionMetadataOnce(ctx context.Context, sessionID string, ownerActorID model.ActorID, metadata collabstore.HostedSessionMetadata) (collabstore.HostedSession, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return collabstore.HostedSession{}, fmt.Errorf("begin hosted metadata update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	var lockedSessionID string
	if err := transaction.QueryRow(ctx, `SELECT session_id FROM collaboration_hosted_sessions WHERE session_id = $1 FOR UPDATE`, sessionID).Scan(&lockedSessionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return collabstore.HostedSession{}, collabstore.ErrHostedSessionMissing
		}
		return collabstore.HostedSession{}, fmt.Errorf("lock hosted session for metadata update: %w", err)
	}
	var role collabstore.HostedRole
	var disabled bool
	if err := transaction.QueryRow(ctx, `SELECT role, disabled FROM collaboration_hosted_members WHERE session_id = $1 AND actor_id = $2 FOR UPDATE`, sessionID, ownerActorID).Scan(&role, &disabled); err != nil || role != collabstore.HostedRoleOwner || disabled {
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			return collabstore.HostedSession{}, collabstore.ErrHostedSessionMissing
		}
		return collabstore.HostedSession{}, fmt.Errorf("verify hosted session owner: %w", err)
	}
	if _, err := transaction.Exec(ctx, `UPDATE collaboration_hosted_sessions SET visibility = $2, title = $3, map_label = $4, environment_label = $5 WHERE session_id = $1`, sessionID, metadata.Visibility, metadata.Title, metadata.MapLabel, metadata.EnvironmentLabel); err != nil {
		return collabstore.HostedSession{}, fmt.Errorf("update hosted session metadata: %w", err)
	}
	session, err := scanHostedSession(transaction.QueryRow(ctx, hostedSessionSelect+" "+hostedSessionFrom+` WHERE hosted_session.session_id = $1`, sessionID))
	if err != nil {
		return collabstore.HostedSession{}, fmt.Errorf("read updated hosted session: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return collabstore.HostedSession{}, fmt.Errorf("commit hosted metadata update: %w", err)
	}
	return session, nil
}

func (store *Store) JoinHostedSession(ctx context.Context, sessionID string, identity collabstore.HostedIdentity) (collabstore.HostedMember, bool, error) {
	identity, err := normalizeHostedIdentity(identity)
	if err != nil {
		return collabstore.HostedMember{}, false, err
	}
	if sessionID == "" || len(sessionID) > 128 {
		return collabstore.HostedMember{}, false, fmt.Errorf("hosted session ID is invalid")
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.HostedMember{}, false, collabstore.ErrStoreClosed
	}
	var lastErr error
	for attempt := 0; attempt < maxTransactionAttempts; attempt++ {
		member, created, err := store.joinHostedSessionOnce(ctx, sessionID, identity)
		if err == nil || (!IsRetryable(err) && !errors.Is(err, collabstore.ErrHostedMemberExists)) {
			return member, created, err
		}
		lastErr = err
		if ctx.Err() != nil {
			return collabstore.HostedMember{}, false, ctx.Err()
		}
	}
	return collabstore.HostedMember{}, false, fmt.Errorf("join hosted session after %d attempts: %w", maxTransactionAttempts, lastErr)
}

func (store *Store) joinHostedSessionOnce(ctx context.Context, sessionID string, identity collabstore.HostedIdentity) (collabstore.HostedMember, bool, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return collabstore.HostedMember{}, false, fmt.Errorf("begin hosted session admission: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	var visibility collabstore.HostedVisibility
	if err := transaction.QueryRow(ctx, `SELECT visibility FROM collaboration_hosted_sessions WHERE session_id = $1 FOR UPDATE`, sessionID).Scan(&visibility); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return collabstore.HostedMember{}, false, collabstore.ErrHostedSessionMissing
		}
		return collabstore.HostedMember{}, false, fmt.Errorf("lock hosted session for admission: %w", err)
	}
	member, found, err := hostedMemberForIdentity(ctx, transaction, sessionID, identity)
	if err != nil {
		return collabstore.HostedMember{}, false, err
	}
	if found {
		if member.Disabled {
			return collabstore.HostedMember{}, false, collabstore.ErrHostedMemberDisabled
		}
		if err := transaction.Commit(ctx); err != nil {
			return collabstore.HostedMember{}, false, fmt.Errorf("commit hosted member reconnect: %w", err)
		}
		return member, false, nil
	}
	if visibility != collabstore.HostedVisibilityCommunity {
		return collabstore.HostedMember{}, false, collabstore.ErrHostedSessionNotCommunity
	}
	member = collabstore.HostedMember{SessionID: sessionID, Issuer: identity.Issuer, Subject: identity.Subject, ActorID: identity.ActorID, DisplayName: identity.DisplayName, Role: collabstore.HostedRoleEditor}
	if err := insertHostedMember(ctx, transaction, member); err != nil {
		return collabstore.HostedMember{}, false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return collabstore.HostedMember{}, false, fmt.Errorf("commit hosted community admission: %w", err)
	}
	return member, true, nil
}

func hostedMemberForIdentity(ctx context.Context, transaction pgx.Tx, sessionID string, identity collabstore.HostedIdentity) (collabstore.HostedMember, bool, error) {
	member := collabstore.HostedMember{SessionID: sessionID}
	err := transaction.QueryRow(ctx, `SELECT issuer, subject, actor_id, display_name, role, disabled FROM collaboration_hosted_members WHERE session_id = $1 AND actor_id = $2 FOR UPDATE`, sessionID, identity.ActorID).Scan(&member.Issuer, &member.Subject, &member.ActorID, &member.DisplayName, &member.Role, &member.Disabled)
	if err == nil {
		if member.Issuer != identity.Issuer || member.Subject != identity.Subject {
			return collabstore.HostedMember{}, false, collabstore.ErrHostedIdentityMismatch
		}
		return member, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return collabstore.HostedMember{}, false, fmt.Errorf("resolve hosted actor membership: %w", err)
	}
	member = collabstore.HostedMember{SessionID: sessionID}
	err = transaction.QueryRow(ctx, `SELECT issuer, subject, actor_id, display_name, role, disabled FROM collaboration_hosted_members WHERE session_id = $1 AND issuer = $2 AND subject = $3 FOR UPDATE`, sessionID, identity.Issuer, identity.Subject).Scan(&member.Issuer, &member.Subject, &member.ActorID, &member.DisplayName, &member.Role, &member.Disabled)
	if err == nil {
		return collabstore.HostedMember{}, false, collabstore.ErrHostedIdentityMismatch
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return collabstore.HostedMember{}, false, nil
	}
	return collabstore.HostedMember{}, false, fmt.Errorf("resolve hosted provider membership: %w", err)
}

func (store *Store) ListHostedMembers(ctx context.Context, sessionID string) ([]collabstore.HostedMember, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return nil, collabstore.ErrStoreClosed
	}
	rows, err := store.pool.Query(ctx, `SELECT issuer, subject, actor_id, display_name, role, disabled FROM collaboration_hosted_members WHERE session_id = $1 ORDER BY actor_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list hosted members: %w", err)
	}
	defer rows.Close()
	var members []collabstore.HostedMember
	for rows.Next() {
		member := collabstore.HostedMember{SessionID: sessionID}
		if err := rows.Scan(&member.Issuer, &member.Subject, &member.ActorID, &member.DisplayName, &member.Role, &member.Disabled); err != nil {
			return nil, fmt.Errorf("scan hosted member: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (store *Store) ResolveHostedMember(ctx context.Context, sessionID, issuer, subject string) (collabstore.HostedMember, bool, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.HostedMember{}, false, collabstore.ErrStoreClosed
	}
	member := collabstore.HostedMember{SessionID: sessionID, Issuer: issuer, Subject: subject}
	err := store.pool.QueryRow(ctx, `SELECT actor_id, display_name, role, disabled FROM collaboration_hosted_members WHERE session_id = $1 AND issuer = $2 AND subject = $3`, sessionID, issuer, subject).Scan(&member.ActorID, &member.DisplayName, &member.Role, &member.Disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return collabstore.HostedMember{}, false, nil
	}
	if err != nil {
		return collabstore.HostedMember{}, false, fmt.Errorf("resolve hosted member: %w", err)
	}
	return member, true, nil
}

func (store *Store) UpdateHostedMemberDisplayName(ctx context.Context, sessionID string, actorID model.ActorID, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if sessionID == "" || displayName == "" || len(displayName) > 128 {
		return fmt.Errorf("hosted member display name is invalid")
	}
	if err := actorID.Validate(); err != nil {
		return err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	result, err := store.pool.Exec(ctx, `UPDATE collaboration_hosted_members SET display_name = $3 WHERE session_id = $1 AND actor_id = $2`, sessionID, actorID, displayName)
	if err != nil {
		return fmt.Errorf("update hosted member display name: %w", err)
	}
	if result.RowsAffected() != 1 {
		return collabstore.ErrHostedSessionMissing
	}
	return nil
}

func (store *Store) CreateHostedInvitation(ctx context.Context, invitation collabstore.HostedInvitation) error {
	if invitation.SessionID == "" || len(invitation.SessionID) > 128 || (invitation.Role != collabstore.HostedRoleViewer && invitation.Role != collabstore.HostedRoleEditor) || invitation.ExpiresAt.IsZero() {
		return fmt.Errorf("hosted invitation is invalid")
	}
	if err := invitation.CreatedByActorID.Validate(); err != nil {
		return err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.ErrStoreClosed
	}
	_, err := store.pool.Exec(ctx, `INSERT INTO collaboration_hosted_invitations(token_hash, session_id, role, created_by_actor_id, expires_at) VALUES($1, $2, $3, $4, $5)`, invitation.TokenHash[:], invitation.SessionID, invitation.Role, invitation.CreatedByActorID, invitation.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert hosted invitation: %w", err)
	}
	return nil
}

func (store *Store) RedeemHostedInvitation(ctx context.Context, expectedSessionID string, tokenHash [sha256.Size]byte, identity collabstore.HostedIdentity, now time.Time) (collabstore.HostedMember, error) {
	if identity.Issuer == "" || identity.Subject == "" || identity.DisplayName == "" || len(identity.DisplayName) > 128 {
		return collabstore.HostedMember{}, collabstore.ErrHostedInvitationInvalid
	}
	if err := identity.ActorID.Validate(); err != nil {
		return collabstore.HostedMember{}, err
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return collabstore.HostedMember{}, collabstore.ErrStoreClosed
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return collabstore.HostedMember{}, fmt.Errorf("begin hosted invitation redeem: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	var sessionID string
	var role collabstore.HostedRole
	var expiresAt time.Time
	var redeemedAt *time.Time
	if err := transaction.QueryRow(ctx, `SELECT session_id, role, expires_at, redeemed_at FROM collaboration_hosted_invitations WHERE token_hash = $1 AND session_id = $2 FOR UPDATE`, tokenHash[:], expectedSessionID).Scan(&sessionID, &role, &expiresAt, &redeemedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return collabstore.HostedMember{}, collabstore.ErrHostedInvitationInvalid
		}
		return collabstore.HostedMember{}, fmt.Errorf("load hosted invitation: %w", err)
	}
	if redeemedAt != nil || !now.Before(expiresAt) {
		return collabstore.HostedMember{}, collabstore.ErrHostedInvitationInvalid
	}
	member := collabstore.HostedMember{SessionID: sessionID, Issuer: identity.Issuer, Subject: identity.Subject, ActorID: identity.ActorID, DisplayName: identity.DisplayName, Role: role}
	if err := insertHostedMember(ctx, transaction, member); err != nil {
		return collabstore.HostedMember{}, err
	}
	if _, err := transaction.Exec(ctx, `UPDATE collaboration_hosted_invitations SET redeemed_at = $2 WHERE token_hash = $1`, tokenHash[:], now); err != nil {
		return collabstore.HostedMember{}, fmt.Errorf("redeem hosted invitation: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return collabstore.HostedMember{}, fmt.Errorf("commit hosted invitation redeem: %w", err)
	}
	return member, nil
}

type hostedSessionScanner interface {
	Scan(...any) error
}

func scanHostedSession(scanner hostedSessionScanner) (collabstore.HostedSession, error) {
	var session collabstore.HostedSession
	err := scanner.Scan(&session.SessionID, &session.DocumentID, &session.CreatedAt, &session.Visibility, &session.Title, &session.MapLabel, &session.EnvironmentLabel, &session.OwnerDisplayName)
	return session, err
}

func readHostedSessionPage(rows pgx.Rows, limit int) (collabstore.HostedSessionPage, error) {
	defer rows.Close()
	page := collabstore.HostedSessionPage{}
	for rows.Next() {
		session, err := scanHostedSession(rows)
		if err != nil {
			return collabstore.HostedSessionPage{}, fmt.Errorf("scan hosted session page: %w", err)
		}
		page.Sessions = append(page.Sessions, session)
	}
	if err := rows.Err(); err != nil {
		return collabstore.HostedSessionPage{}, err
	}
	if len(page.Sessions) > limit {
		page.NextCursor = page.Sessions[limit-1].SessionID
		page.Sessions = page.Sessions[:limit]
	}
	return page, nil
}

func normalizeHostedSessionPageRequest(request collabstore.HostedSessionPageRequest) (collabstore.HostedSessionPageRequest, error) {
	if request.Limit == 0 {
		request.Limit = collabstore.HostedPageDefaultLimit
	}
	if request.Limit < 1 || request.Limit > collabstore.HostedPageMaxLimit {
		return collabstore.HostedSessionPageRequest{}, fmt.Errorf("hosted session page limit must be between 1 and %d", collabstore.HostedPageMaxLimit)
	}
	if !utf8.ValidString(request.Cursor) || len(request.Cursor) > 128 {
		return collabstore.HostedSessionPageRequest{}, fmt.Errorf("hosted session page cursor is invalid")
	}
	for _, character := range request.Cursor {
		if unicode.IsControl(character) {
			return collabstore.HostedSessionPageRequest{}, fmt.Errorf("hosted session page cursor is invalid")
		}
	}
	return request, nil
}

func normalizeHostedActiveIDs(sessionIDs []string) ([]string, error) {
	if len(sessionIDs) > collabstore.HostedActiveIDMaxCount {
		return nil, fmt.Errorf("active hosted session ID count exceeds %d", collabstore.HostedActiveIDMaxCount)
	}
	activeIDs := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if sessionID == "" || len(sessionID) > 128 || !utf8.ValidString(sessionID) {
			return nil, fmt.Errorf("active hosted session ID is invalid")
		}
		for _, character := range sessionID {
			if unicode.IsControl(character) {
				return nil, fmt.Errorf("active hosted session ID is invalid")
			}
		}
		if _, exists := seen[sessionID]; exists {
			continue
		}
		seen[sessionID] = struct{}{}
		activeIDs = append(activeIDs, sessionID)
	}
	return activeIDs, nil
}

func normalizeHostedIdentity(identity collabstore.HostedIdentity) (collabstore.HostedIdentity, error) {
	identity.Issuer = strings.TrimSpace(identity.Issuer)
	identity.Subject = strings.TrimSpace(identity.Subject)
	identity.DisplayName = strings.TrimSpace(identity.DisplayName)
	if identity.Issuer == "" || identity.Subject == "" || identity.DisplayName == "" || !utf8.ValidString(identity.Issuer) || !utf8.ValidString(identity.Subject) || !utf8.ValidString(identity.DisplayName) || len(identity.DisplayName) > collabstore.HostedMetadataTextMaxBytes {
		return collabstore.HostedIdentity{}, fmt.Errorf("hosted identity is invalid")
	}
	for _, value := range []string{identity.Issuer, identity.Subject, identity.DisplayName} {
		for _, character := range value {
			if unicode.IsControl(character) {
				return collabstore.HostedIdentity{}, fmt.Errorf("hosted identity is invalid")
			}
		}
	}
	if err := identity.ActorID.Validate(); err != nil {
		return collabstore.HostedIdentity{}, err
	}
	return identity, nil
}

func hostedSessionMetadata(session collabstore.HostedSession) collabstore.HostedSessionMetadata {
	return collabstore.HostedSessionMetadata{Visibility: session.Visibility, Title: session.Title, MapLabel: session.MapLabel, EnvironmentLabel: session.EnvironmentLabel}
}

func applyHostedSessionMetadata(session *collabstore.HostedSession, metadata collabstore.HostedSessionMetadata) {
	session.Visibility = metadata.Visibility
	session.Title = metadata.Title
	session.MapLabel = metadata.MapLabel
	session.EnvironmentLabel = metadata.EnvironmentLabel
}

func validateHostedSession(session collabstore.HostedSession, owner collabstore.HostedMember) error {
	if session.SessionID == "" || len(session.SessionID) > 128 || session.CreatedAt.IsZero() || owner.SessionID != session.SessionID || owner.Role != collabstore.HostedRoleOwner || owner.Disabled {
		return fmt.Errorf("hosted session or owner is invalid")
	}
	if err := session.DocumentID.Validate(); err != nil {
		return err
	}
	if _, err := collabstore.NormalizeHostedSessionMetadata(hostedSessionMetadata(session)); err != nil {
		return err
	}
	return validateHostedMember(owner)
}

func validateHostedMember(member collabstore.HostedMember) error {
	if member.SessionID == "" || member.Issuer == "" || member.Subject == "" || member.DisplayName == "" || len(member.DisplayName) > 128 || !collabstore.ValidHostedRole(member.Role) {
		return fmt.Errorf("hosted member is invalid")
	}
	return member.ActorID.Validate()
}

func insertHostedMember(ctx context.Context, transaction pgx.Tx, member collabstore.HostedMember) error {
	if err := validateHostedMember(member); err != nil {
		return err
	}
	_, err := transaction.Exec(ctx, `INSERT INTO collaboration_hosted_members(session_id, issuer, subject, actor_id, display_name, role, disabled) VALUES($1, $2, $3, $4, $5, $6, $7)`, member.SessionID, member.Issuer, member.Subject, member.ActorID, member.DisplayName, member.Role, member.Disabled)
	if postgresCode(err) == "23505" {
		return collabstore.ErrHostedMemberExists
	}
	if err != nil {
		return fmt.Errorf("insert hosted member: %w", err)
	}
	return nil
}

func postgresCode(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}
	return ""
}

var _ collabstore.HostedRegistry = (*Store)(nil)
var _ collabstore.HostedSessionBrowserStore = (*Store)(nil)
