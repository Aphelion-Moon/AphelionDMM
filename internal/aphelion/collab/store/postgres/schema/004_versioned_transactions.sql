ALTER TABLE collaboration_documents
	ADD COLUMN transaction_version SMALLINT NOT NULL DEFAULT 1
	CHECK (transaction_version IN (1, 2));

ALTER TABLE collaboration_operations
	ADD COLUMN storage_version SMALLINT NOT NULL DEFAULT 1
	CHECK (storage_version IN (1, 2));
ALTER TABLE collaboration_operations ADD COLUMN body_digest TEXT CHECK (body_digest IS NULL OR length(body_digest) = 64);
ALTER TABLE collaboration_operations ADD COLUMN body_bytes BIGINT CHECK (body_bytes IS NULL OR body_bytes > 0);
ALTER TABLE collaboration_operations ADD COLUMN change_count BIGINT CHECK (change_count IS NULL OR change_count > 0);

CREATE TABLE collaboration_transaction_chunks (
	operation_id TEXT NOT NULL REFERENCES collaboration_operations(operation_id) ON DELETE CASCADE,
	chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
	data BYTEA NOT NULL CHECK (octet_length(data) > 0),
	chunk_digest TEXT NOT NULL CHECK (length(chunk_digest) = 64),
	PRIMARY KEY (operation_id, chunk_index)
);
