package index

// FTS column order and weights. The weights are applied at query time through
// bm25(); keeping them next to the DDL makes the coupling explicit.
//
//	symbol ≫ title > expand > heading_path > prefix > body
//
// "expand" holds camelCase / snake_case splits of identifiers found in the
// chunk, so "swerve kinematics" matches SwerveDriveKinematics while the whole
// identifier still matches as one token.
const ftsWeights = "10.0, 4.0, 2.0, 1.5, 2.5, 1.0" // symbol, title, heading_path, prefix, expand, body

const ddl = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID;

CREATE TABLE chunk (
  id           INTEGER PRIMARY KEY,
  doc_id       TEXT    NOT NULL,
  doc_num      INTEGER NOT NULL,          -- dense per-shard document number (diversity without string compares)
  ord          INTEGER NOT NULL,
  library      TEXT    NOT NULL,
  version_lo   TEXT    NOT NULL,
  version_hi   TEXT    NOT NULL DEFAULT '',
  season       TEXT    NOT NULL,
  channel      TEXT    NOT NULL CHECK (channel IN ('stable','beta','alpha')),
  language     TEXT    NOT NULL CHECK (language IN ('java','cpp','python','any')),
  kind         TEXT    NOT NULL CHECK (kind IN ('prose','code','api','release','rule','forum')),
  title        TEXT    NOT NULL DEFAULT '',
  heading_path TEXT    NOT NULL DEFAULT '',
  symbol       TEXT    NOT NULL DEFAULT '',
  symbol_key   TEXT    NOT NULL DEFAULT '', -- lower-cased simple name of symbol, for in-SQL exact matching
  prefix       TEXT    NOT NULL,
  expand       TEXT    NOT NULL DEFAULT '',
  body         TEXT    NOT NULL,
  source_url   TEXT    NOT NULL,
  anchor       TEXT    NOT NULL DEFAULT '',
  upstream_rev TEXT    NOT NULL,
  retrieved_at INTEGER NOT NULL,
  license      TEXT    NOT NULL,
  trust        TEXT    NOT NULL CHECK (trust IN ('official','vendor','community')),
  suspect      INTEGER NOT NULL DEFAULT 0,
  authority    INTEGER NOT NULL DEFAULT 0,
  tokens       INTEGER NOT NULL,
  UNIQUE (doc_id, ord)
);
CREATE INDEX chunk_filter ON chunk (season, language, library);

CREATE VIRTUAL TABLE chunk_fts USING fts5(
  symbol, title, heading_path, prefix, expand, body,
  content='chunk', content_rowid='id',
  tokenize="unicode61 remove_diacritics 2 tokenchars '_'",
  detail=full
);

CREATE TABLE symbol (
  fqn           TEXT NOT NULL,
  simple        TEXT NOT NULL COLLATE NOCASE,
  owner         TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
  library       TEXT NOT NULL,
  version       TEXT NOT NULL,
  season        TEXT NOT NULL,
  language      TEXT NOT NULL CHECK (language IN ('java','cpp','python')),
  kind          TEXT NOT NULL,
  signature     TEXT NOT NULL,
  summary       TEXT NOT NULL DEFAULT '',
  since         TEXT NOT NULL DEFAULT '',
  deprecated_in TEXT NOT NULL DEFAULT '',
  removed_in    TEXT NOT NULL DEFAULT '',
  replacement   TEXT NOT NULL DEFAULT '',
  chunk_id      TEXT NOT NULL DEFAULT '',
  source_url    TEXT NOT NULL,
  upstream_rev  TEXT NOT NULL,
  retrieved_at  INTEGER NOT NULL,
  license       TEXT NOT NULL,
  trust         TEXT NOT NULL,
  PRIMARY KEY (library, version, language, fqn, signature)
) WITHOUT ROWID;
CREATE INDEX symbol_simple ON symbol (simple, season);
CREATE INDEX symbol_fqn    ON symbol (fqn, season);
CREATE INDEX symbol_repl   ON symbol (replacement) WHERE replacement <> '';

-- Exact vendordep facts from WPILib's vendor-json-repo catalog (never embedded).
CREATE TABLE vendordep (
  uuid         TEXT NOT NULL,
  name         TEXT NOT NULL COLLATE NOCASE,
  version      TEXT NOT NULL,
  season       TEXT NOT NULL,
  channel      TEXT NOT NULL,
  frc_year     TEXT NOT NULL DEFAULT '',
  file_name    TEXT NOT NULL DEFAULT '',
  json_url     TEXT NOT NULL DEFAULT '',
  maven_urls   TEXT NOT NULL DEFAULT '[]', -- JSON array
  conflicts    TEXT NOT NULL DEFAULT '[]', -- JSON array of {uuid, errorMessage, offlineFileName}
  java_deps    INTEGER NOT NULL DEFAULT 0,
  cpp_deps     INTEGER NOT NULL DEFAULT 0,
  description  TEXT NOT NULL DEFAULT '',
  website      TEXT NOT NULL DEFAULT '',
  raw_json     TEXT NOT NULL,
  source_url   TEXT NOT NULL,
  upstream_rev TEXT NOT NULL,
  retrieved_at INTEGER NOT NULL,
  PRIMARY KEY (uuid, version, season)
) WITHOUT ROWID;
CREATE INDEX vendordep_name ON vendordep (name, season);

-- Release facts (frc_whats_new): one row per upstream release. Added in M3;
-- readers treat a missing table as "no releases" (older shards).
CREATE TABLE release (
  library      TEXT NOT NULL,
  version      TEXT NOT NULL,
  season       TEXT NOT NULL,
  channel      TEXT NOT NULL,
  published_at INTEGER NOT NULL,
  breaking     INTEGER NOT NULL DEFAULT 0,
  title        TEXT NOT NULL DEFAULT '',
  summary      TEXT NOT NULL DEFAULT '',
  chunk_id     TEXT NOT NULL DEFAULT '',
  source_url   TEXT NOT NULL,
  upstream_rev TEXT NOT NULL,
  retrieved_at INTEGER NOT NULL,
  license      TEXT NOT NULL,
  trust        TEXT NOT NULL,
  PRIMARY KEY (library, version)
) WITHOUT ROWID;

-- Hardware specs (frc_hardware). One row per (part, source, season): values
-- from different sources are never merged. Added in M3.
CREATE TABLE hw_spec (
  part         TEXT NOT NULL,
  name         TEXT NOT NULL,
  category     TEXT NOT NULL,
  source       TEXT NOT NULL,
  season       TEXT NOT NULL,
  fields       TEXT NOT NULL, -- JSON object of numeric values with unit-suffixed keys
  factory      TEXT NOT NULL DEFAULT '',
  note         TEXT NOT NULL DEFAULT '',
  source_url   TEXT NOT NULL,
  upstream_rev TEXT NOT NULL,
  retrieved_at INTEGER NOT NULL,
  license      TEXT NOT NULL,
  trust        TEXT NOT NULL,
  PRIMARY KEY (part, source, season)
) WITHOUT ROWID;
`

// Enum codes returned by the light FTS query (no per-row string allocation).
const (
	TrustOfficial uint8 = iota
	TrustVendor
	TrustCommunity
)

const (
	KindProse uint8 = iota
	KindCode
	KindAPI
	KindRelease
	KindRule
	KindForum
)

// Meta keys.
const (
	metaSchema  = "schema_version"
	metaName    = "name"
	metaBuildID = "build_id"
	metaBuiltAt = "built_at"
	metaChunks  = "chunks"
	metaSymbols = "symbols"
)
