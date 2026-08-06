-- Atrio's local SQLite schema (T-021).
--
-- This database is the third store of ADR-006: it lives at .atrio/atrio.db,
-- never travels in the repository, and holds only state that is either
-- derivable from the repository or ephemeral by design. The architectural
-- invariant it must uphold (ADR-006, verified in CI by T-023) is that
-- deleting this file, cloning the repository and running sync reconstructs
-- 100% of the local state.
--
-- That invariant is also why there is no migration machinery: the DDL
-- generation lives in `pragma user_version`, and a build that finds a
-- generation it did not write discards the file and recreates it rather
-- than migrating. Nothing here is worth migrating — see localdb.go for the
-- per-table classification of what refills each one, and which task owns it.
--
-- Every real table is STRICT: without it SQLite would accept a string where
-- an integer is declared and store it as-is, which is exactly the class of
-- silent corruption a derived cache cannot afford (a rebuild would keep
-- reproducing it). Timestamps are TEXT in the same RFC 3339 form the JSON
-- artifacts use (store/repository.go's formatTimestamp), so a value read out
-- of here and a value read out of an artifact compare as written.

-- ---------------------------------------------------------------------------
-- Document index (derivable — refilled by T-022 from docs/**/*.md)
-- ---------------------------------------------------------------------------

-- One row per *valid* indexable document. The columns mirror
-- document-front-matter.schema.json's own fields, so what this table can hold
-- is exactly what that contract allows; tags and relations are arrays there
-- and get their own tables here.
--
-- The body text is deliberately absent: the corpus lives in docs/, and ADR-006
-- makes the repository its source of truth. What is searchable lives in
-- document_fts, which needs the tokens to do its job at all.
create table document (
    -- ULID, from the reduced documentEnvelope the front matter composes.
    id             text not null primary key,
    -- Repository-relative, slash-separated, e.g. 'docs/vision.md'. UNIQUE
    -- because two index rows claiming one file would make "which one is
    -- stale?" unanswerable.
    path           text not null unique,
    title          text not null,
    purpose        text not null,
    -- ISO 639-1, per common.schema.json's languageCode. Whether it matches
    -- the project's artifactLanguage is a cross-artifact rule the core owns,
    -- not something this table can see.
    language       text not null,
    -- Digest of the file as it was when indexed: what lets a later pass tell
    -- "already indexed" from "changed since".
    content_sha256 text not null,
    indexed_at     text not null
) strict;

create table document_tag (
    document_id text not null references document (id) on delete cascade,
    tag         text not null,
    primary key (document_id, tag)
) strict, without rowid;

create index document_tag_by_tag on document_tag (tag);

create table document_relation (
    document_id text not null references document (id) on delete cascade,
    -- The catalog is closed in document-front-matter.schema.json and repeated
    -- here on purpose: a CHECK is the only thing that stops a typo from
    -- becoming an unqueryable relation kind in a table no schema validates.
    kind        text not null check (
        kind in ('implements', 'supersedes', 'relates_to', 'derives_from')
    ),
    -- The two halves of common.schema.json's reference: {type, id}. Whether
    -- the target actually exists is resolved by store/refs.go (artifacts) and
    -- by T-022 (documents) — a foreign key here would be wrong, since a
    -- relation may legitimately point at an artifact this database never sees.
    target_type text not null,
    target_id   text not null,
    primary key (document_id, kind, target_type, target_id)
) strict, without rowid;

create index document_relation_by_target on document_relation (
    target_type, target_id
);

-- One row per document that could not be indexed, which 03-arquitectura.md:85
-- requires be *marked* rather than silently dropped, and attributed to whoever
-- last edited it. Keyed by path and not by id precisely because a document
-- whose front matter is unparseable may have no usable id at all.
--
-- The attribution columns are filled by T-022 from git blame; they default to
-- empty rather than being NOT NULL-with-no-default so that detecting an
-- invalid document never depends on git being answerable at that moment.
create table document_issue (
    path              text not null primary key,
    reason            text not null,
    detected_at       text not null,
    attributed_name   text not null default '',
    attributed_email  text not null default '',
    attributed_commit text not null default ''
) strict;

-- Full-text search over the corpus (FTS5, ADR-007).
--
-- This is a regular FTS5 table, not a contentless one: a contentless table
-- cannot produce snippet()/highlight() and requires the caller to hand back
-- the original column values just to delete a row, which would push a
-- correctness burden onto every future consumer. The cost is that the indexed
-- text is stored here as well as in docs/ — acceptable precisely because this
-- file is derived, gitignored and discarded on any generation change.
--
-- The join key is document.id, the ULID, and deliberately NOT document.rowid.
-- `document` declares a TEXT primary key, so it has no INTEGER PRIMARY KEY
-- alias, and SQLite documents VACUUM as free to renumber the rowids of exactly
-- such tables. Keying on rowid would mean a routine maintenance command could
-- repoint every search hit at the wrong document — silently, permanently, and
-- only once someone runs the VACUUM that nothing here runs today. Keying on
-- the identity the rest of the schema already uses has no such failure mode.
--
-- UNINDEXED keeps document_id out of the full-text index while still storing
-- it, so a hit can name its document without the ULID itself becoming a
-- searchable token (verified: a term present only in that column matches
-- nothing).
create virtual table document_fts using fts5 (
    document_id unindexed,
    title,
    purpose,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- Deletion is mirrored by trigger so the two can never drift: forgetting the
-- companion DELETE would leave a phantom hit pointing at a document that no
-- longer exists. Insertion is deliberately *not* triggered — the body text
-- this table indexes does not exist in `document`, so only the indexer can
-- supply it.
create trigger document_fts_after_delete after delete on document begin
delete from document_fts where document_id = old.id;
end;

-- ---------------------------------------------------------------------------
-- Materialization hashes (derivable — refilled by T-052 by recompiling)
-- ---------------------------------------------------------------------------

-- 03-arquitectura.md:107-111: materializing records a hash per generated file,
-- and every sync recompiles and compares to classify each item as modified,
-- missing or orphaned. Keyed by path because that is the question sync asks:
-- "what did Atrio write here, and does it still match?"
create table materialization (
    path            text not null primary key,
    provider_id     text not null,
    -- The marketplace item this file was materialized from: without it an
    -- orphaned file cannot be told from one whose definition simply moved.
    catalog_id      text not null,
    sha256          text not null,
    materialized_at text not null
) strict;

create index materialization_by_catalog on materialization (catalog_id);

-- ---------------------------------------------------------------------------
-- Notifications (ephemeral — nothing refills these; the table is born empty)
-- ---------------------------------------------------------------------------

-- T-042 owns levels and filters. The level and category catalogs are
-- deliberately left as open TEXT rather than CHECK constraints: closing a
-- catalog that the task which owns it has not defined yet would mean either
-- guessing it or forcing a generation bump the moment T-042 disagrees. This
-- is the same trade-off the schemas make for requiredCapabilities.
create table notification (
    id         text not null primary key,
    created_at text not null,
    level      text not null,
    category   text not null,
    title      text not null,
    body       text not null default '',
    -- NULL while unread. The partial index is what keeps "show me what I have
    -- not read" from scanning the whole table once this fills up.
    read_at    text
) strict;

create index notification_unread on notification (created_at)
where read_at is null;

-- ---------------------------------------------------------------------------
-- Session (ephemeral — nothing refills this)
-- ---------------------------------------------------------------------------

-- The current work session. 01-definicion-producto.md:192-197 makes this
-- load-bearing: a conversation that cannot complete its closing extraction
-- leaves the session 'pending closure' and notifies, so that state has to
-- survive the process that created it.
--
-- No portal session token column: the portal is an M2 requirement
-- (01-definicion-producto.md:231), its token is minted by the CLI when the
-- portal is raised, and adding a column for it now would be storing something
-- token-shaped with nothing to authenticate. When it lands it stores a digest,
-- never the token itself.
create table session (
    id               text not null primary key,
    started_at       text not null,
    ended_at         text,
    -- Catalog owned by whichever task first runs sessions (T-070/T-080), for
    -- the same reason notification.level is open.
    state            text not null,
    task_id          text,
    flow_progress_id text
) strict;

create index session_open on session (started_at) where ended_at is null;

-- ---------------------------------------------------------------------------
-- Lock registry (ephemeral — nothing refills this)
-- ---------------------------------------------------------------------------

-- 03-arquitectura.md:32 is precise about the division: the mutex *is* a
-- lockfile, and SQLite is where it is registered. This table is therefore a
-- registry and not a mutex — it records who holds what, so a human or the CLI
-- can answer "why is this project busy?" without stat-ing lockfiles.
--
-- Acquisition and release live with the first caller that needs them (T-031),
-- together with the policy this task has no basis to invent: how a holder is
-- detected dead, whether a lock expires, and who may break one.
create table project_lock (
    -- What is locked. A scope key, not a file path: 'project', 'task:<ulid>'.
    scope       text not null primary key,
    -- The lockfile that is the actual mutex, repository-relative.
    lockfile    text not null,
    holder_pid  integer not null,
    holder_host text not null,
    -- cli | portal | agent-run. Open TEXT: the catalog of process kinds is
    -- not fixed anywhere in the spec yet.
    holder_kind text not null,
    acquired_at text not null
) strict;
