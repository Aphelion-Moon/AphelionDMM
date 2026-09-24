package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

// UpgradeTransactions copies a validated V3 schema into a distinct V4 schema
// on the same PostgreSQL database. The V3 source schema remains intact; it is
// a retained source, not an off-host disaster backup.
func UpgradeTransactions(ctx context.Context, sourceConfig, destinationConfig Config) (err error) {
	if strings.TrimSpace(sourceConfig.DSN) == "" || strings.TrimSpace(destinationConfig.DSN) == "" || sourceConfig.DSN != destinationConfig.DSN {
		return fmt.Errorf("PostgreSQL transaction upgrade requires the same non-empty DSN for source and destination")
	}
	if !postgresIdentifierPattern.MatchString(sourceConfig.Schema) || !postgresIdentifierPattern.MatchString(destinationConfig.Schema) || sourceConfig.Schema == destinationConfig.Schema {
		return fmt.Errorf("PostgreSQL transaction upgrade requires distinct explicit source and destination schemas")
	}
	source, err := Open(ctx, sourceConfig)
	if err != nil {
		return fmt.Errorf("open PostgreSQL transaction upgrade source: %w", err)
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	if source.schemaVersion != 3 {
		return fmt.Errorf("PostgreSQL transaction upgrade source schema is %d; require V3", source.schemaVersion)
	}
	sourceTx, err := source.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin consistent PostgreSQL source snapshot: %w", err)
	}
	defer func() { _ = sourceTx.Rollback(context.Background()) }()
	documentRows, err := sourceTx.Query(ctx, `SELECT document_id FROM collaboration_documents ORDER BY document_id`)
	if err != nil {
		return fmt.Errorf("list PostgreSQL source documents: %w", err)
	}
	var documentIDs []string
	for documentRows.Next() {
		var documentID string
		if err := documentRows.Scan(&documentID); err != nil {
			documentRows.Close()
			return err
		}
		documentIDs = append(documentIDs, documentID)
	}
	rowsErr := documentRows.Err()
	documentRows.Close()
	if rowsErr != nil {
		return fmt.Errorf("read PostgreSQL source documents: %w", rowsErr)
	}
	sourceStates := make(map[string]engine.RecoveryState, len(documentIDs))
	for _, documentID := range documentIDs {
		state, err := loadRecovery(ctx, sourceTx, model.DocumentID(documentID), false, 3)
		if err != nil {
			return fmt.Errorf("validate PostgreSQL source document %q: %w", documentID, err)
		}
		if _, err := state.Restore(); err != nil {
			return fmt.Errorf("validate PostgreSQL source document %q history: %w", documentID, err)
		}
		sourceStates[documentID] = state
	}

	admin, err := pgx.Connect(ctx, destinationConfig.DSN)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL transaction upgrade target: %w", err)
	}
	defer func() { err = errors.Join(err, admin.Close(context.Background())) }()
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)`, destinationConfig.Schema).Scan(&exists); err != nil {
		return fmt.Errorf("check PostgreSQL target schema: %w", err)
	}
	if exists {
		return fmt.Errorf("PostgreSQL transaction upgrade destination schema already exists")
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{destinationConfig.Schema}.Sanitize()); err != nil {
		return fmt.Errorf("create staged PostgreSQL target schema: %w", err)
	}
	created, complete := true, false
	defer func() {
		if created && !complete {
			_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{destinationConfig.Schema}.Sanitize()+` CASCADE`)
		}
	}()
	target, err := Open(ctx, destinationConfig)
	if err != nil {
		return fmt.Errorf("create staged PostgreSQL V4 store: %w", err)
	}
	targetClosed := false
	defer func() {
		if !targetClosed {
			err = errors.Join(err, target.Close())
		}
	}()
	if target.schemaVersion != 4 {
		return fmt.Errorf("staged PostgreSQL target schema is %d; want V4", target.schemaVersion)
	}
	targetTx, err := target.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin staged PostgreSQL copy: %w", err)
	}
	defer func() { _ = targetTx.Rollback(context.Background()) }()
	if err := copyPostgresV3(ctx, sourceTx, targetTx); err != nil {
		return err
	}
	for _, documentID := range documentIDs {
		state, err := loadRecovery(ctx, targetTx, model.DocumentID(documentID), false, 4)
		if err != nil {
			return fmt.Errorf("validate staged PostgreSQL document %q: %w", documentID, err)
		}
		if _, err := state.Restore(); err != nil {
			return fmt.Errorf("validate staged PostgreSQL document %q history: %w", documentID, err)
		}
		if !reflect.DeepEqual(state, sourceStates[documentID]) {
			return fmt.Errorf("staged PostgreSQL document %q differs from validated V3 source", documentID)
		}
		version, err := readTransactionVersion(ctx, targetTx, 4, model.DocumentID(documentID), false)
		if err != nil || version != collabstore.LegacyTransactionVersion {
			return fmt.Errorf("staged PostgreSQL document %q transaction version = %d, %v; want V1", documentID, version, err)
		}
	}
	if err := targetTx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staged PostgreSQL V4 copy: %w", err)
	}
	if err := sourceTx.Commit(ctx); err != nil {
		return fmt.Errorf("finish consistent PostgreSQL source snapshot: %w", err)
	}
	if err := target.Close(); err != nil {
		return fmt.Errorf("close staged PostgreSQL V4 store: %w", err)
	}
	targetClosed = true
	complete = true
	return nil
}

func copyPostgresV3(ctx context.Context, source queryer, target pgx.Tx) error {
	documents, err := source.Query(ctx, `SELECT document_id, snapshot, snapshot_revision, snapshot_hash, current_revision, current_hash FROM collaboration_documents ORDER BY document_id`)
	if err != nil {
		return err
	}
	for documents.Next() {
		var documentID string
		var snapshot []byte
		var snapshotRevision, currentRevision int64
		var snapshotHash, currentHash string
		if err := documents.Scan(&documentID, &snapshot, &snapshotRevision, &snapshotHash, &currentRevision, &currentHash); err != nil {
			documents.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_documents(document_id, snapshot, snapshot_revision, snapshot_hash, current_revision, current_hash, transaction_version) VALUES($1, $2, $3, $4, $5, $6, $7)`, documentID, snapshot, snapshotRevision, strings.TrimSpace(snapshotHash), currentRevision, strings.TrimSpace(currentHash), collabstore.LegacyTransactionVersion); err != nil {
			documents.Close()
			return fmt.Errorf("copy PostgreSQL document: %w", err)
		}
	}
	documentsErr := documents.Err()
	documents.Close()
	if err := documentsErr; err != nil {
		return fmt.Errorf("read PostgreSQL source documents: %w", err)
	}
	hostedSessions, err := source.Query(ctx, `SELECT session_id, document_id, created_at FROM collaboration_hosted_sessions ORDER BY session_id`)
	if err != nil {
		return err
	}
	for hostedSessions.Next() {
		var sessionID, documentID string
		var createdAt any
		if err := hostedSessions.Scan(&sessionID, &documentID, &createdAt); err != nil {
			hostedSessions.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_hosted_sessions(session_id, document_id, created_at) VALUES($1, $2, $3)`, sessionID, documentID, createdAt); err != nil {
			hostedSessions.Close()
			return fmt.Errorf("copy PostgreSQL hosted session: %w", err)
		}
	}
	hostedSessionsErr := hostedSessions.Err()
	hostedSessions.Close()
	if err := hostedSessionsErr; err != nil {
		return fmt.Errorf("read PostgreSQL hosted sessions: %w", err)
	}
	hostedMembers, err := source.Query(ctx, `SELECT session_id, issuer, subject, actor_id, display_name, role, disabled FROM collaboration_hosted_members ORDER BY session_id, issuer, subject`)
	if err != nil {
		return err
	}
	for hostedMembers.Next() {
		var sessionID, issuer, subject, actorID, displayName, role string
		var disabled bool
		if err := hostedMembers.Scan(&sessionID, &issuer, &subject, &actorID, &displayName, &role, &disabled); err != nil {
			hostedMembers.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_hosted_members(session_id, issuer, subject, actor_id, display_name, role, disabled) VALUES($1, $2, $3, $4, $5, $6, $7)`, sessionID, issuer, subject, actorID, displayName, role, disabled); err != nil {
			hostedMembers.Close()
			return fmt.Errorf("copy PostgreSQL hosted member: %w", err)
		}
	}
	hostedMembersErr := hostedMembers.Err()
	hostedMembers.Close()
	if err := hostedMembersErr; err != nil {
		return fmt.Errorf("read PostgreSQL hosted members: %w", err)
	}
	hostedInvitations, err := source.Query(ctx, `SELECT token_hash, session_id, role, created_by_actor_id, expires_at, redeemed_at FROM collaboration_hosted_invitations ORDER BY session_id, created_by_actor_id, expires_at`)
	if err != nil {
		return err
	}
	for hostedInvitations.Next() {
		var tokenHash []byte
		var sessionID, role, createdByActorID string
		var expiresAt any
		var redeemedAt any
		if err := hostedInvitations.Scan(&tokenHash, &sessionID, &role, &createdByActorID, &expiresAt, &redeemedAt); err != nil {
			hostedInvitations.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_hosted_invitations(token_hash, session_id, role, created_by_actor_id, expires_at, redeemed_at) VALUES($1, $2, $3, $4, $5, $6)`, tokenHash, sessionID, role, createdByActorID, expiresAt, redeemedAt); err != nil {
			hostedInvitations.Close()
			return fmt.Errorf("copy PostgreSQL hosted invitation: %w", err)
		}
	}
	hostedInvitationsErr := hostedInvitations.Err()
	hostedInvitations.Close()
	if err := hostedInvitationsErr; err != nil {
		return fmt.Errorf("read PostgreSQL hosted invitations: %w", err)
	}
	operations, err := source.Query(ctx, `SELECT document_id, operation_id, revision, accepted, map_hash FROM collaboration_operations ORDER BY document_id, revision`)
	if err != nil {
		return err
	}
	for operations.Next() {
		var documentID, operationID, mapHash string
		var revision int64
		var accepted []byte
		if err := operations.Scan(&documentID, &operationID, &revision, &accepted, &mapHash); err != nil {
			operations.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_operations(document_id, operation_id, revision, accepted, map_hash, storage_version) VALUES($1, $2, $3, $4, $5, $6)`, documentID, operationID, revision, accepted, strings.TrimSpace(mapHash), collabstore.LegacyTransactionVersion); err != nil {
			operations.Close()
			return fmt.Errorf("copy PostgreSQL operation: %w", err)
		}
	}
	operationsErr := operations.Err()
	operations.Close()
	if err := operationsErr; err != nil {
		return fmt.Errorf("read PostgreSQL source operations: %w", err)
	}
	hashes, err := source.Query(ctx, `SELECT document_id, revision, map_hash FROM collaboration_revision_hashes ORDER BY document_id, revision`)
	if err != nil {
		return err
	}
	for hashes.Next() {
		var documentID, mapHash string
		var revision int64
		if err := hashes.Scan(&documentID, &revision, &mapHash); err != nil {
			hashes.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_revision_hashes(document_id, revision, map_hash) VALUES($1, $2, $3)`, documentID, revision, strings.TrimSpace(mapHash)); err != nil {
			hashes.Close()
			return fmt.Errorf("copy PostgreSQL revision hash: %w", err)
		}
	}
	hashesErr := hashes.Err()
	hashes.Close()
	if err := hashesErr; err != nil {
		return fmt.Errorf("read PostgreSQL source revision hashes: %w", err)
	}
	checkpoints, err := source.Query(ctx, `SELECT checkpoint_id, document_id, session_id, idempotency_key, revision, map_hash, requested_by, created_at, status, artifact_hash, verifier, verifier_version, diagnostic_code, completed_at FROM collaboration_export_checkpoints ORDER BY document_id, checkpoint_id`)
	if err != nil {
		return err
	}
	for checkpoints.Next() {
		var checkpointID, documentID, sessionID, idempotencyKey, mapHash, requestedBy, status string
		var revision int64
		var createdAt, completedAt any
		var artifactHash, verifier, verifierVersion, diagnosticCode *string
		if err := checkpoints.Scan(&checkpointID, &documentID, &sessionID, &idempotencyKey, &revision, &mapHash, &requestedBy, &createdAt, &status, &artifactHash, &verifier, &verifierVersion, &diagnosticCode, &completedAt); err != nil {
			checkpoints.Close()
			return err
		}
		if _, err := target.Exec(ctx, `INSERT INTO collaboration_export_checkpoints(checkpoint_id, document_id, session_id, idempotency_key, revision, map_hash, requested_by, created_at, status, artifact_hash, verifier, verifier_version, diagnostic_code, completed_at) VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, checkpointID, documentID, sessionID, idempotencyKey, revision, strings.TrimSpace(mapHash), requestedBy, createdAt, status, artifactHash, verifier, verifierVersion, diagnosticCode, completedAt); err != nil {
			checkpoints.Close()
			return fmt.Errorf("copy PostgreSQL export checkpoint: %w", err)
		}
	}
	checkpointsErr := checkpoints.Err()
	checkpoints.Close()
	if err := checkpointsErr; err != nil {
		return fmt.Errorf("read PostgreSQL source export checkpoints: %w", err)
	}
	return nil
}
