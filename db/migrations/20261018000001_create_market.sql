-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Instrument catalog. asset_class is the raw source class (equity, etf, gold, commodity, crypto);
-- 'fx' is assigned by the importer to currency pairs (symbol ending in =X). The classified
-- Asset Classifier class lives in asset_classifications.
CREATE TABLE assets (
    symbol       TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    asset_class  TEXT NOT NULL CHECK (asset_class IN ('equity', 'etf', 'gold', 'commodity', 'crypto', 'fx')),
    currency     TEXT,
    exchange     TEXT,
    country      TEXT,
    isin         TEXT,
    figi         TEXT,
    source       TEXT NOT NULL,
    -- false for stubs created from a price file that has no row in assets.json or universe/*
    has_metadata BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE assets IS 'All instruments (assets.json + market/universe + stubs for price files without metadata).';
COMMENT ON COLUMN assets.exchange IS 'Source exchange code: JKT, US, LSE, HKG, SHZ, SHH, NSI, SET, GER, TOR, KLS, PAR, SES; NULL for crypto/commodity/gold.';
COMMENT ON COLUMN assets.has_metadata IS 'FALSE when only the price file existed (name = symbol, currency/exchange inferred from the price folder).';
CREATE INDEX assets_class_idx ON assets (asset_class);
CREATE INDEX assets_exchange_idx ON assets (exchange);
CREATE UNIQUE INDEX assets_isin_uq ON assets (isin) WHERE isin IS NOT NULL;
CREATE INDEX assets_name_trgm_idx ON assets USING gin (lower(name) gin_trgm_ops);
CREATE INDEX assets_symbol_trgm_idx ON assets USING gin (lower(symbol) gin_trgm_ops);
CREATE INDEX assets_symbol_prefix_idx ON assets (lower(symbol) text_pattern_ops);

-- One row per symbol and trading day. Double precision on purpose: the source is float32-derived
-- (yfinance) and the table holds ~7M rows; NUMERIC would triple the size for no real precision.
-- Crypto rows have only close (open/high/low/volume NULL). A 2-year window x ~41k symbols stays in a
-- single table: the (symbol, trade_date) primary key serves every read pattern, no partitioning needed.
CREATE TABLE prices_daily (
    symbol     TEXT NOT NULL REFERENCES assets (symbol) ON DELETE CASCADE,
    trade_date DATE NOT NULL,
    open       DOUBLE PRECISION,
    high       DOUBLE PRECISION,
    low        DOUBLE PRECISION,
    close      DOUBLE PRECISION NOT NULL CHECK (close > 0),
    volume     DOUBLE PRECISION,
    source     TEXT NOT NULL,
    PRIMARY KEY (symbol, trade_date)
);
COMMENT ON TABLE prices_daily IS 'Daily OHLCV, last 2 years relative to the newest trade_date in the data. close is in the asset currency (crypto: IDR).';
CREATE INDEX prices_daily_date_idx ON prices_daily (trade_date);

CREATE TABLE dividends (
    symbol   TEXT NOT NULL REFERENCES assets (symbol) ON DELETE CASCADE,
    ex_date  DATE NOT NULL,
    amount   DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    currency TEXT,
    source   TEXT NOT NULL,
    PRIMARY KEY (symbol, ex_date)
);
COMMENT ON COLUMN dividends.currency IS 'Source currency; NULL on 5 source rows without one. GBp is pence.';
CREATE INDEX dividends_ex_date_idx ON dividends (ex_date);

CREATE TABLE treasury_yields (
    trade_date   DATE NOT NULL,
    tenor        TEXT NOT NULL,
    tenor_months NUMERIC(6, 2),
    yield_pct    NUMERIC(8, 4) NOT NULL,
    PRIMARY KEY (trade_date, tenor)
);
COMMENT ON TABLE treasury_yields IS 'Government yield curve (percent) by tenor: 1M,1_5M,2M,3M,4M,6M,1Y,2Y,3Y,5Y,7Y,10Y,20Y,30Y.';
CREATE INDEX treasury_yields_tenor_idx ON treasury_yields (tenor, trade_date DESC);

CREATE TABLE fundamentals (
    symbol     TEXT PRIMARY KEY REFERENCES assets (symbol) ON DELETE CASCADE,
    sector     TEXT,
    industry   TEXT,
    market_cap DOUBLE PRECISION,
    info       JSONB NOT NULL,
    statements JSONB NOT NULL DEFAULT '{}',
    loaded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE fundamentals IS 'yfinance fundamentals (info = ratios, statements = annual/quarterly income, balance, cashflow). Only 100 IDX stocks in the source.';
COMMENT ON COLUMN fundamentals.market_cap IS 'info.marketCap in the asset currency (IDR for IDX), used by the classifier large-cap threshold.';

CREATE TABLE news_articles (
    id               TEXT PRIMARY KEY,
    source           TEXT NOT NULL,
    channel          TEXT NOT NULL,
    category         TEXT NOT NULL CHECK (category IN ('capital_markets', 'macro', 'global', 'real_sector')),
    title            TEXT NOT NULL,
    summary          TEXT NOT NULL DEFAULT '',
    url              TEXT NOT NULL UNIQUE,
    published_at     TIMESTAMPTZ NOT NULL,
    language         TEXT,
    region           TEXT,
    tickers          TEXT[] NOT NULL DEFAULT '{}',
    sentiment        DOUBLE PRECISION,
    ticker_sentiment JSONB NOT NULL DEFAULT '{}',
    search           TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', title || ' ' || summary)) STORED
);
COMMENT ON COLUMN news_articles.category IS 'Derived by the importer from channel/region/source (see market/domain/news.go).';
COMMENT ON COLUMN news_articles.sentiment IS 'Always NULL in the current source data.';
CREATE INDEX news_published_idx ON news_articles (published_at DESC, id DESC);
CREATE INDEX news_category_published_idx ON news_articles (category, published_at DESC, id DESC);
CREATE INDEX news_tickers_gin ON news_articles USING gin (tickers);
CREATE INDEX news_search_gin ON news_articles USING gin (search);

-- Output of the deterministic Asset Classifier (six dimensions + trace).
CREATE TABLE asset_classifications (
    symbol               TEXT PRIMARY KEY REFERENCES assets (symbol) ON DELETE CASCADE,
    asset_class          TEXT NOT NULL,
    sector               TEXT NOT NULL,
    sharia_status        TEXT NOT NULL CHECK (sharia_status IN ('sharia_compliant', 'non_sharia', 'mixed', 'unknown')),
    geography            TEXT NOT NULL CHECK (geography IN ('indonesia', 'global', 'emerging', 'developed', 'unknown')),
    volatility_class     TEXT NOT NULL CHECK (volatility_class IN ('low', 'medium', 'high', 'unknown')),
    liquidity_class      TEXT NOT NULL CHECK (liquidity_class IN ('low', 'medium', 'high', 'unknown')),
    annualized_vol       DOUBLE PRECISION,
    avg_daily_value_idr  DOUBLE PRECISION,
    market_cap_idr       DOUBLE PRECISION,
    alert_threshold_pct  DOUBLE PRECISION,
    trace                JSONB NOT NULL,
    classifier_version   TEXT NOT NULL,
    classified_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE asset_classifications IS 'YAML Asset Classifier output. "unknown" = not derivable from the available data (see trace).';
CREATE INDEX asset_classifications_class_idx ON asset_classifications (asset_class);
CREATE INDEX asset_classifications_gate_idx ON asset_classifications (sharia_status, geography, volatility_class, liquidity_class);

-- Importer bookkeeping: a price file already loaded with the same size+mtime is skipped on re-run.
CREATE TABLE import_files (
    path      TEXT PRIMARY KEY,
    size      BIGINT NOT NULL,
    mtime     TIMESTAMPTZ NOT NULL,
    cutoff    DATE NOT NULL,
    rows      INTEGER NOT NULL,
    loaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Catalog row for search/listing: asset + latest close + classification.
CREATE VIEW asset_catalog_view AS
SELECT a.symbol, a.name, a.asset_class AS source_class, a.currency, a.exchange, a.country,
       p.trade_date AS last_trade_date, p.close AS last_price,
       c.asset_class, c.sector, c.sharia_status, c.geography, c.volatility_class, c.liquidity_class
FROM assets a
LEFT JOIN LATERAL (SELECT trade_date, close FROM prices_daily WHERE symbol = a.symbol ORDER BY trade_date DESC LIMIT 1) p ON TRUE
LEFT JOIN asset_classifications c ON c.symbol = a.symbol;

-- +goose Down
DROP VIEW IF EXISTS asset_catalog_view;
DROP TABLE IF EXISTS import_files, asset_classifications, news_articles, fundamentals, treasury_yields, dividends, prices_daily, assets;
