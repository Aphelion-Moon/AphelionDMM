ALTER TABLE documents
	ADD COLUMN transaction_version INTEGER NOT NULL DEFAULT 1
	CHECK (transaction_version IN (1, 2));

ALTER TABLE operations
	ADD COLUMN storage_version INTEGER NOT NULL DEFAULT 1
	CHECK (storage_version IN (1, 2));
ALTER TABLE operations ADD COLUMN body_digest TEXT CHECK (body_digest IS NULL OR length(body_digest) = 64);
ALTER TABLE operations ADD COLUMN body_bytes INTEGER CHECK (body_bytes IS NULL OR body_bytes > 0);
ALTER TABLE operations ADD COLUMN change_count INTEGER CHECK (change_count IS NULL OR change_count > 0);

CREATE TABLE IF NOT EXISTS transaction_chunks (
	document_id TEXT NOT NULL,
	operation_id TEXT NOT NULL,
	chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
	data BLOB NOT NULL CHECK (length(data) > 0),
	chunk_digest TEXT NOT NULL CHECK (length(chunk_digest) = 64),
	PRIMARY KEY (document_id, operation_id, chunk_index),
	FOREIGN KEY (document_id, operation_id) REFERENCES operations(document_id, operation_id) ON DELETE CASCADE
);

PRAGMA user_version = 4;
