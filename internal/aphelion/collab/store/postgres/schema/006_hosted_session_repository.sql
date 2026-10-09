-- Optional host-published repository descriptor (DME file name, environment
-- hash, git branch and commit). All columns are nullable: an absent descriptor
-- means "unknown", and existing sessions stay unknown.
ALTER TABLE collaboration_hosted_sessions
	ADD COLUMN repository_dme_name TEXT
		CHECK (repository_dme_name IS NULL OR (octet_length(repository_dme_name) BETWEEN 1 AND 128 AND repository_dme_name !~ '[[:cntrl:]/\\]')),
	ADD COLUMN repository_environment_hash TEXT
		CHECK (repository_environment_hash IS NULL OR repository_environment_hash ~ '^[0-9a-f]{64}$'),
	ADD COLUMN repository_git_branch TEXT
		CHECK (repository_git_branch IS NULL OR (octet_length(repository_git_branch) BETWEEN 1 AND 200 AND repository_git_branch !~ '[[:cntrl:]]')),
	ADD COLUMN repository_git_commit TEXT
		CHECK (repository_git_commit IS NULL OR repository_git_commit ~ '^[0-9a-f]{40}$'),
	ADD CONSTRAINT collaboration_hosted_sessions_repository_complete
		CHECK ((repository_dme_name IS NULL) = (repository_environment_hash IS NULL)
			AND (repository_dme_name IS NOT NULL OR (repository_git_branch IS NULL AND repository_git_commit IS NULL)));