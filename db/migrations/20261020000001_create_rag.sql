-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE rag_documents (
  id          bigserial PRIMARY KEY,
  r2_key      text NOT NULL UNIQUE,
  etag        text NOT NULL,
  source_type text NOT NULL CHECK (source_type IN ('ojk_regulasi', 'ojk_edukasi', 'idx_financial_statement', 'idx_laporan_keuangan')),
  title       text NOT NULL,
  ticker      text,
  fiscal_year int,
  language    text,
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded')),
  pages       int,
  scanned     boolean NOT NULL DEFAULT false,
  ingested_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX rag_documents_filter_idx ON rag_documents (source_type, ticker, fiscal_year);

CREATE TABLE rag_chunks (
  id          bigserial PRIMARY KEY,
  document_id bigint NOT NULL REFERENCES rag_documents (id) ON DELETE CASCADE,
  ordinal     int NOT NULL,
  page_start  int NOT NULL,
  page_end    int NOT NULL,
  section     text,
  has_table   boolean NOT NULL DEFAULT false,
  content     text NOT NULL,
  embedding   vector(1024) NOT NULL,
  tsv         tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
  UNIQUE (document_id, ordinal)
);
CREATE INDEX rag_chunks_embedding_idx ON rag_chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX rag_chunks_tsv_idx ON rag_chunks USING gin (tsv);

-- +goose Down
DROP TABLE IF EXISTS rag_chunks;
DROP TABLE IF EXISTS rag_documents;
-- keep the vector extension: other objects may use it
