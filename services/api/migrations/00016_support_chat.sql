-- Sprint 9.1: Mise Chat and the help centre.
--
-- Conversations belong to a signed-in customer or to a guest (name + email,
-- an opaque token held in an httpOnly cookie on the app host). Messages are
-- immutable and numbered per conversation (seq), so a client that reconnects
-- asks for "everything after seq N". Internal notes live in their own table:
-- no customer-facing query reads it. Typing indicators are never stored.
-- Attachments are not created: no approved private object storage exists.
--
-- Business hours, availability and response-expectation text ship empty:
-- the business sets them; nothing here promises a reply time.

-- +goose Up

CREATE TABLE support_chat_settings (
    id                    boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled               boolean NOT NULL DEFAULT true,
    timezone              text NOT NULL DEFAULT 'America/Port_of_Spain',
    -- [{ "day": 0-6 (Sunday = 0), "open": "HH:MM", "close": "HH:MM" }]; empty = no hours set.
    business_hours        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(business_hours) = 'array'),
    online_message        text NOT NULL DEFAULT '' CHECK (char_length(online_message) <= 500),
    offline_message       text NOT NULL DEFAULT '' CHECK (char_length(offline_message) <= 500),
    response_expectation  text NOT NULL DEFAULT '' CHECK (char_length(response_expectation) <= 300),
    -- At most one "you have a reply" email per conversation per window.
    email_cooldown_minutes integer NOT NULL DEFAULT 30 CHECK (email_cooldown_minutes BETWEEN 5 AND 1440),
    updated_by            uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at            timestamptz NOT NULL DEFAULT now()
);
INSERT INTO support_chat_settings DEFAULT VALUES;

CREATE TABLE support_holidays (
    day  date PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100)
);

CREATE TABLE support_guest_sessions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- sha256 of the opaque token; the token itself is never stored.
    token_hash         bytea NOT NULL UNIQUE,
    name               text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    email              text NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254),
    -- Set only when a signed-in, verified customer with this email presented the token.
    linked_customer_id uuid REFERENCES customers (id) ON DELETE SET NULL,
    linked_at          timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_seen_at       timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL
);
CREATE INDEX support_guest_sessions_email_idx ON support_guest_sessions (lower(email), created_at DESC);

CREATE SEQUENCE support_conversation_number_seq START 1001;

CREATE TABLE support_conversations (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number                   bigint NOT NULL UNIQUE DEFAULT nextval('support_conversation_number_seq'),
    customer_id              uuid REFERENCES customers (id) ON DELETE CASCADE,
    guest_id                 uuid REFERENCES support_guest_sessions (id) ON DELETE CASCADE,
    source                   text NOT NULL DEFAULT 'app' CHECK (source IN ('site', 'app', 'help')),
    subject                  text NOT NULL DEFAULT '' CHECK (char_length(subject) <= 160),
    status                   text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'pending', 'resolved')),
    priority                 text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    tags                     text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 20),
    assignee_id              uuid REFERENCES users (id) ON DELETE SET NULL,
    team                     text CHECK (team IS NULL OR char_length(team) BETWEEN 1 AND 60),
    case_id                  uuid REFERENCES support_cases (id) ON DELETE SET NULL,
    order_id                 uuid REFERENCES orders (id) ON DELETE SET NULL,
    subscription_id          uuid REFERENCES subscriptions (id) ON DELETE SET NULL,
    last_seq                 bigint NOT NULL DEFAULT 0,
    last_message_at          timestamptz,
    last_customer_message_at timestamptz,
    first_human_response_at  timestamptz,
    resolved_at              timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT support_conversations_owner CHECK ((customer_id IS NULL) <> (guest_id IS NULL)),
    CONSTRAINT support_conversations_resolved CHECK ((status = 'resolved') = (resolved_at IS NOT NULL))
);
CREATE INDEX support_conversations_customer_idx ON support_conversations (customer_id, updated_at DESC) WHERE customer_id IS NOT NULL;
CREATE INDEX support_conversations_guest_idx ON support_conversations (guest_id, updated_at DESC) WHERE guest_id IS NOT NULL;
CREATE INDEX support_conversations_inbox_idx ON support_conversations (status, assignee_id, updated_at DESC);

CREATE TABLE support_messages (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id   uuid NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    seq               bigint NOT NULL,
    author_kind       text NOT NULL CHECK (author_kind IN ('customer', 'guest', 'agent', 'system')),
    author_user_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    body              text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    -- The sender's own id for the message: a resend after a dropped connection is the same message.
    client_message_id text CHECK (client_message_id IS NULL OR client_message_id ~ '^[A-Za-z0-9_-]{8,64}$'),
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT support_messages_seq UNIQUE (conversation_id, seq)
);
CREATE UNIQUE INDEX support_messages_client_id ON support_messages (conversation_id, client_message_id) WHERE client_message_id IS NOT NULL;
CREATE TRIGGER support_messages_immutable BEFORE UPDATE OR DELETE ON support_messages
    FOR EACH ROW WHEN (pg_trigger_depth() = 0) EXECUTE FUNCTION forbid_change();

CREATE TABLE support_read_positions (
    conversation_id uuid NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    reader_kind     text NOT NULL CHECK (reader_kind IN ('customer', 'guest', 'agent')),
    reader_id       uuid NOT NULL,
    last_read_seq   bigint NOT NULL DEFAULT 0,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, reader_kind, reader_id)
);

CREATE TABLE support_conversation_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    type            text NOT NULL CHECK (type IN ('created', 'assigned', 'unassigned', 'transferred', 'resolved', 'reopened',
                                                  'escalated', 'linked', 'priority', 'tags', 'guest_linked')),
    detail          jsonb NOT NULL DEFAULT '{}',
    actor_id        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_conversation_events_conv_idx ON support_conversation_events (conversation_id, id);
CREATE TRIGGER support_conversation_events_immutable BEFORE UPDATE OR DELETE ON support_conversation_events
    FOR EACH ROW WHEN (pg_trigger_depth() = 0) EXECUTE FUNCTION forbid_change();

-- Staff-only notes. Never joined into a customer or guest response.
CREATE TABLE support_internal_notes (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    author_id       uuid REFERENCES users (id) ON DELETE SET NULL,
    body            text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_internal_notes_conv_idx ON support_internal_notes (conversation_id, id);
CREATE TRIGGER support_internal_notes_immutable BEFORE UPDATE OR DELETE ON support_internal_notes
    FOR EACH ROW WHEN (pg_trigger_depth() = 0) EXECUTE FUNCTION forbid_change();

CREATE TABLE support_saved_replies (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title      text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    body       text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    archived   boolean NOT NULL DEFAULT false,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One rating per conversation (the latest answer counts), 1 (poor) to 5 (great).
CREATE TABLE support_feedback (
    conversation_id uuid PRIMARY KEY REFERENCES support_conversations (id) ON DELETE CASCADE,
    rating          smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment         text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 1000),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Reply notifications, one row per conversation per cool-down window.
CREATE TABLE support_chat_notifications (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    dedupe_key      text NOT NULL UNIQUE,
    seq             bigint NOT NULL,
    status          text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'skipped', 'failed')),
    reason          text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz
);

-- ---------------------------------------------------------------- help centre

CREATE TABLE help_categories (
    slug     text PRIMARY KEY CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    name     text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    position integer NOT NULL DEFAULT 0
);
-- Structure only: no article text is written here.
INSERT INTO help_categories (slug, name, position) VALUES
    ('getting-started', 'Getting started', 1),
    ('subscriptions-and-billing', 'Subscriptions and billing', 2),
    ('meals-and-menus', 'Meals and menus', 3),
    ('delivery', 'Delivery', 4),
    ('account-and-privacy', 'Account and privacy', 5);

CREATE TABLE help_articles (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 100),
    category_slug text NOT NULL REFERENCES help_categories (slug),
    title         text NOT NULL CHECK (char_length(title) BETWEEN 3 AND 160),
    summary       text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 300),
    body          text NOT NULL DEFAULT '' CHECK (char_length(body) <= 20000),
    status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'in_review', 'published', 'archived')),
    version       integer NOT NULL DEFAULT 1,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    submitted_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    published_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    published_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    search        tsvector GENERATED ALWAYS AS (
                      setweight(to_tsvector('english', title), 'A') ||
                      setweight(to_tsvector('english', summary), 'B') ||
                      setweight(to_tsvector('english', body), 'C')) STORED,
    CONSTRAINT help_articles_published CHECK (status <> 'published' OR (published_at IS NOT NULL AND char_length(body) > 0))
);
CREATE INDEX help_articles_search_idx ON help_articles USING gin (search);
CREATE INDEX help_articles_public_idx ON help_articles (category_slug, title) WHERE status = 'published';

CREATE TABLE help_article_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id uuid NOT NULL REFERENCES help_articles (id) ON DELETE CASCADE,
    action     text NOT NULL CHECK (action IN ('created', 'edited', 'submitted', 'returned', 'published', 'archived')),
    version    integer NOT NULL,
    note       text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX help_article_events_article_idx ON help_article_events (article_id, id);
CREATE TRIGGER help_article_events_immutable BEFORE UPDATE OR DELETE ON help_article_events
    FOR EACH ROW WHEN (pg_trigger_depth() = 0) EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- permissions

INSERT INTO permissions (key, description) VALUES
    ('support_chat.read',           'See chat conversations and messages.'),
    ('support_chat.reply',          'Reply to customers and guests in chat; set priority, tags and links.'),
    ('support_chat.assign',         'Assign chat conversations.'),
    ('support_chat.transfer',       'Transfer chat conversations to another agent or team.'),
    ('support_chat.resolve',        'Resolve and reopen chat conversations.'),
    ('support_chat.internal_notes', 'Read and write internal notes on chat conversations.'),
    ('support_chat.manage',         'Manage chat settings, business hours and saved replies.'),
    ('support_chat.analytics',      'View support chat analytics.'),
    ('help_centre.edit',            'Write and edit help centre articles and submit them for review.'),
    ('help_centre.publish',         'Publish and archive help centre articles.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('super_admin', ARRAY['support_chat.read', 'support_chat.reply', 'support_chat.assign', 'support_chat.transfer', 'support_chat.resolve',
                          'support_chat.internal_notes', 'support_chat.manage', 'support_chat.analytics', 'help_centre.edit', 'help_centre.publish']),
    ('admin',       ARRAY['support_chat.read', 'support_chat.reply', 'support_chat.assign', 'support_chat.transfer', 'support_chat.resolve',
                          'support_chat.internal_notes', 'support_chat.manage', 'support_chat.analytics', 'help_centre.edit', 'help_centre.publish']),
    ('operations',  ARRAY['support_chat.read', 'support_chat.reply', 'support_chat.assign', 'support_chat.transfer', 'support_chat.resolve',
                          'support_chat.internal_notes', 'support_chat.analytics']),
    ('support',     ARRAY['support_chat.read', 'support_chat.reply', 'support_chat.assign', 'support_chat.transfer', 'support_chat.resolve',
                          'support_chat.internal_notes', 'help_centre.edit']),
    ('marketing',   ARRAY['help_centre.edit'])
) AS g(r, perms), unnest(perms) AS p;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key LIKE 'support_chat.%' OR permission_key LIKE 'help_centre.%';
DELETE FROM permissions WHERE key LIKE 'support_chat.%' OR key LIKE 'help_centre.%';
DROP TABLE help_article_events;
DROP TABLE help_articles;
DROP TABLE help_categories;
DROP TABLE support_chat_notifications;
DROP TABLE support_feedback;
DROP TABLE support_saved_replies;
DROP TABLE support_internal_notes;
DROP TABLE support_conversation_events;
DROP TABLE support_read_positions;
DROP TABLE support_messages;
DROP TABLE support_conversations;
DROP SEQUENCE support_conversation_number_seq;
DROP TABLE support_guest_sessions;
DROP TABLE support_holidays;
DROP TABLE support_chat_settings;
