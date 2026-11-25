-- Sprint 9: launch readiness and operations visibility.
--
-- worker_heartbeats: each worker process upserts its row every 30 seconds,
-- so staff (and the readiness page) can see a worker that has stopped.
--
-- launch_signoffs: the record of business and engineering sign-offs for each
-- launch gate. Append-only: a decision is withdrawn by a later "revoked" row,
-- never by editing history. A configuration record is never a sign-off; the
-- launch control centre needs both.

-- +goose Up

CREATE TABLE worker_heartbeats (
    worker     text PRIMARY KEY CHECK (char_length(worker) BETWEEN 1 AND 100),
    version    text NOT NULL DEFAULT '' CHECK (char_length(version) <= 100),
    started_at timestamptz NOT NULL,
    beat_at    timestamptz NOT NULL
);

CREATE TABLE launch_signoffs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gate        text NOT NULL CHECK (gate ~ '^[a-z][a-z_]{1,59}$'),
    decision    text NOT NULL CHECK (decision IN ('approved', 'not_applicable', 'revoked')),
    -- Where the evidence or approval lives: a document, ticket or meeting.
    reference   text NOT NULL CHECK (char_length(reference) BETWEEN 3 AND 300),
    note        text NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    -- No user foreign key, as with configuration rows: the record outlives accounts.
    decided_by  uuid NOT NULL,
    decided_by_name text NOT NULL DEFAULT '',
    decided_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX launch_signoffs_gate_idx ON launch_signoffs (gate, id DESC);

CREATE TRIGGER launch_signoffs_immutable BEFORE UPDATE OR DELETE ON launch_signoffs
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

INSERT INTO permissions (key, description) VALUES
    ('launch.read',    'View the launch control centre and system status.'),
    ('launch.signoff', 'Record or revoke a launch gate sign-off.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('super_admin', ARRAY['launch.read', 'launch.signoff']),
    ('admin', ARRAY['launch.read']),
    ('operations', ARRAY['launch.read']),
    ('finance', ARRAY['launch.read'])
) AS g(r, perms), unnest(perms) AS p
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('launch.read', 'launch.signoff');
DELETE FROM permissions WHERE key IN ('launch.read', 'launch.signoff');
DROP TABLE launch_signoffs;
DROP TABLE worker_heartbeats;
