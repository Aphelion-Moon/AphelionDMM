ALTER TABLE collaboration_hosted_sessions
	ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
		CHECK (visibility IN ('private', 'community')),
	ADD COLUMN title TEXT NOT NULL DEFAULT ''
		CHECK (octet_length(title) <= 128 AND title !~ '[[:cntrl:]]'),
	ADD COLUMN map_label TEXT NOT NULL DEFAULT ''
		CHECK (octet_length(map_label) <= 128 AND map_label !~ '[[:cntrl:]]'),
	ADD COLUMN environment_label TEXT NOT NULL DEFAULT ''
		CHECK (octet_length(environment_label) <= 128 AND environment_label !~ '[[:cntrl:]]');

CREATE INDEX collaboration_hosted_members_actor_sessions_idx
	ON collaboration_hosted_members(actor_id, session_id)
	WHERE NOT disabled;
