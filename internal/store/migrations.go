package store

// migrations are applied in order; PRAGMA user_version records progress.
// Never edit an applied migration; append a new one instead.
var migrations = []string{
	`
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL
);

CREATE TABLE users (
	id             TEXT PRIMARY KEY,
	email          TEXT NOT NULL UNIQUE,
	name           TEXT NOT NULL DEFAULT '',
	password_hash  TEXT NOT NULL,
	email_verified INTEGER NOT NULL DEFAULT 0,
	is_admin       INTEGER NOT NULL DEFAULT 0,
	disabled       INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL,
	updated_at     INTEGER NOT NULL
);

CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	auth_time  INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE clients (
	id                        TEXT PRIMARY KEY,
	name                      TEXT NOT NULL,
	type                      TEXT NOT NULL CHECK (type IN ('web', 'spa', 'native')),
	secret_hash               TEXT NOT NULL DEFAULT '',
	redirect_uris             TEXT NOT NULL DEFAULT '[]',
	post_logout_redirect_uris TEXT NOT NULL DEFAULT '[]',
	allow_refresh             INTEGER NOT NULL DEFAULT 1,
	created_at                INTEGER NOT NULL,
	updated_at                INTEGER NOT NULL
);

CREATE TABLE auth_requests (
	id               TEXT PRIMARY KEY,
	client_id        TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	redirect_uri     TEXT NOT NULL,
	scopes           TEXT NOT NULL,
	state            TEXT NOT NULL DEFAULT '',
	nonce            TEXT NOT NULL DEFAULT '',
	response_type    TEXT NOT NULL,
	response_mode    TEXT NOT NULL DEFAULT '',
	code_challenge   TEXT NOT NULL,
	challenge_method TEXT NOT NULL,
	prompt           TEXT NOT NULL DEFAULT '[]',
	max_age          INTEGER,
	login_hint       TEXT NOT NULL DEFAULT '',
	hint_subject     TEXT NOT NULL DEFAULT '',
	status           TEXT NOT NULL DEFAULT 'pending'
	                 CHECK (status IN ('pending', 'approved', 'denied', 'redeemed')),
	user_id          TEXT REFERENCES users(id) ON DELETE CASCADE,
	auth_time        INTEGER,
	amr              TEXT NOT NULL DEFAULT '[]',
	code_hash        TEXT UNIQUE,
	created_at       INTEGER NOT NULL,
	expires_at       INTEGER NOT NULL
);

CREATE TABLE access_tokens (
	id         TEXT PRIMARY KEY,
	family_id  TEXT NOT NULL,
	client_id  TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	scopes     TEXT NOT NULL,
	audience   TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	revoked_at INTEGER
);
CREATE INDEX access_tokens_family ON access_tokens(family_id);
CREATE INDEX access_tokens_user ON access_tokens(user_id);

CREATE TABLE refresh_tokens (
	id         TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	family_id  TEXT NOT NULL,
	client_id  TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	scopes     TEXT NOT NULL,
	audience   TEXT NOT NULL,
	amr        TEXT NOT NULL DEFAULT '[]',
	auth_time  INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	revoked_at INTEGER
);
CREATE INDEX refresh_tokens_family ON refresh_tokens(family_id);
CREATE INDEX refresh_tokens_user ON refresh_tokens(user_id);

CREATE TABLE signing_keys (
	id          TEXT PRIMARY KEY,
	algorithm   TEXT NOT NULL,
	private_pem TEXT NOT NULL,
	created_at  INTEGER NOT NULL,
	retired_at  INTEGER
);
`,
	// Phase 2: device list, last sign-in, per-app access, audit log.
	`
ALTER TABLE sessions ADD COLUMN id TEXT NOT NULL DEFAULT '';
UPDATE sessions SET id = lower(hex(randomblob(16)));
CREATE UNIQUE INDEX sessions_id ON sessions(id);
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';

ALTER TABLE users ADD COLUMN last_login_at INTEGER;

ALTER TABLE clients ADD COLUMN access TEXT NOT NULL DEFAULT 'all'
	CHECK (access IN ('all', 'admins', 'selected'));

CREATE TABLE client_users (
	client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	PRIMARY KEY (client_id, user_id)
);
CREATE INDEX client_users_user ON client_users(user_id);

CREATE TABLE audit_events (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	at           INTEGER NOT NULL,
	type         TEXT NOT NULL,
	actor_id     TEXT NOT NULL DEFAULT '',
	actor_email  TEXT NOT NULL DEFAULT '',
	subject_type TEXT NOT NULL DEFAULT '',
	subject_id   TEXT NOT NULL DEFAULT '',
	subject_name TEXT NOT NULL DEFAULT '',
	ip           TEXT NOT NULL DEFAULT '',
	detail       TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_events_at ON audit_events(at);
CREATE INDEX audit_events_type ON audit_events(type, id);
CREATE INDEX audit_events_actor ON audit_events(actor_id, id);
CREATE INDEX audit_events_subject ON audit_events(subject_id, id);
`,
	// Phase 3: which XAuth users may read which xmail mailboxes. The
	// mailboxes themselves live in xmail.
	`
CREATE TABLE mailbox_grants (
	mailbox_address TEXT NOT NULL,
	user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	assigned_by     TEXT NOT NULL,
	assigned_at     INTEGER NOT NULL,
	PRIMARY KEY (mailbox_address, user_id)
);
CREATE INDEX mailbox_grants_user ON mailbox_grants(user_id);
`,
	// Phase 4: shared and personal TOTP entries. The secret is AES-256-GCM
	// ciphertext; the key lives in settings under "totp_key".
	`
CREATE TABLE totp_entries (
	id               TEXT PRIMARY KEY,
	owner_user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
	name             TEXT NOT NULL,
	issuer           TEXT NOT NULL DEFAULT '',
	account_name     TEXT NOT NULL DEFAULT '',
	secret_encrypted BLOB NOT NULL,
	digits           INTEGER NOT NULL DEFAULT 6,
	period           INTEGER NOT NULL DEFAULT 30,
	algorithm        TEXT NOT NULL DEFAULT 'SHA1',
	note             TEXT NOT NULL DEFAULT '',
	created_by       TEXT NOT NULL,
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL
);
CREATE INDEX totp_entries_owner ON totp_entries(owner_user_id);

CREATE TABLE totp_grants (
	entry_id    TEXT NOT NULL REFERENCES totp_entries(id) ON DELETE CASCADE,
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	assigned_by TEXT NOT NULL,
	assigned_at INTEGER NOT NULL,
	PRIMARY KEY (entry_id, user_id)
);
CREATE INDEX totp_grants_user ON totp_grants(user_id);
	`,
	// Phase 5: one-time email verification and password reset links.
	// Only the hash of the token is stored.
	`
CREATE TABLE email_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	purpose    TEXT NOT NULL CHECK (purpose IN ('verify', 'reset')),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
);
CREATE INDEX email_tokens_user ON email_tokens(user_id, purpose);
	`,
	// Phase 5: WebAuthn passkeys. The credential record is JSON; the
	// challenge row is deleted as soon as the ceremony finishes.
	`
CREATE TABLE webauthn_credentials (
	id           TEXT PRIMARY KEY,
	user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL DEFAULT '',
	credential   TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER
);
CREATE INDEX webauthn_credentials_user ON webauthn_credentials(user_id);

CREATE TABLE webauthn_challenges (
	id         TEXT PRIMARY KEY,
	session    TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
`,
}
