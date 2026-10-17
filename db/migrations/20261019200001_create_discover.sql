-- +goose Up
-- Screener read model: one row per asset, derived by `make refresh-metrics` (cmd/metrics, domain.Derive).
-- A table filled by a command instead of a materialized view because the derivations (valuation chain, growth from
-- the statements jsonb, yield units, return windows) are Go domain functions with unit tests; the page query then
-- reads ~42k narrow rows through the sort indexes. Must be refreshed after `make import-market` / `make classify-assets`.
CREATE TABLE asset_metrics (
    symbol            TEXT PRIMARY KEY REFERENCES assets (symbol) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    name_lc           TEXT NOT NULL,
    symbol_lc         TEXT NOT NULL,
    asset_class       TEXT NOT NULL,
    class_group       TEXT NOT NULL,
    exchange          TEXT,
    currency          TEXT,
    last_price        DOUBLE PRECISION,
    last_trade_date   DATE,
    ret_7d            DOUBLE PRECISION,
    ret_1y            DOUBLE PRECISION,
    market_cap        DOUBLE PRECISION,
    valuation_metric  TEXT CHECK (valuation_metric IN ('PE', 'PB', 'PS')),
    valuation_value   DOUBLE PRECISION,
    growth_metric     TEXT CHECK (growth_metric IN ('E', 'R')),
    growth_pct        DOUBLE PRECISION,
    div_yield_pct     DOUBLE PRECISION,
    industry          TEXT NOT NULL,
    refreshed_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE asset_metrics IS 'Discover screener rows. Percentages are percent numbers (6.27 = 6.27%). Fundamentals-based columns exist for ~100 IDX stocks only.';

-- Keyset order is (column DIR NULLS LAST, symbol ASC): one index per sort column and direction.
CREATE INDEX asset_metrics_mcap_desc ON asset_metrics (market_cap DESC NULLS LAST, symbol);
CREATE INDEX asset_metrics_mcap_asc ON asset_metrics (market_cap ASC NULLS LAST, symbol);
CREATE INDEX asset_metrics_price_desc ON asset_metrics (last_price DESC NULLS LAST, symbol);
CREATE INDEX asset_metrics_price_asc ON asset_metrics (last_price ASC NULLS LAST, symbol);
CREATE INDEX asset_metrics_r7_desc ON asset_metrics (ret_7d DESC NULLS LAST, symbol);
CREATE INDEX asset_metrics_r7_asc ON asset_metrics (ret_7d ASC NULLS LAST, symbol);
CREATE INDEX asset_metrics_r1y_desc ON asset_metrics (ret_1y DESC NULLS LAST, symbol);
CREATE INDEX asset_metrics_r1y_asc ON asset_metrics (ret_1y ASC NULLS LAST, symbol);
CREATE INDEX asset_metrics_yield_desc ON asset_metrics (div_yield_pct DESC NULLS LAST, symbol);
CREATE INDEX asset_metrics_yield_asc ON asset_metrics (div_yield_pct ASC NULLS LAST, symbol);
CREATE INDEX asset_metrics_name_asc ON asset_metrics (name_lc, symbol);
CREATE INDEX asset_metrics_name_desc ON asset_metrics (name_lc DESC, symbol);
CREATE INDEX asset_metrics_name_trgm ON asset_metrics USING gin (name_lc gin_trgm_ops);
CREATE INDEX asset_metrics_symbol_trgm ON asset_metrics USING gin (symbol_lc gin_trgm_ops);
CREATE INDEX asset_metrics_group_idx ON asset_metrics (class_group);
CREATE INDEX asset_metrics_exchange_idx ON asset_metrics (exchange);
CREATE INDEX asset_metrics_industry_idx ON asset_metrics (lower(industry));

CREATE TABLE watchlist (
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL REFERENCES assets (symbol) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, symbol)
);
CREATE INDEX watchlist_user_idx ON watchlist (user_id, created_at DESC);

-- +goose Down
DROP TABLE watchlist;
DROP TABLE asset_metrics;
