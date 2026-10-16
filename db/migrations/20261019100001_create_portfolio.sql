-- +goose Up
CREATE TABLE holdings (
    id                      TEXT PRIMARY KEY,
    user_id                 TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    symbol                  TEXT REFERENCES assets (symbol),
    manual_name             TEXT,
    manual_class            TEXT,
    manual_currency         TEXT,
    manual_price            NUMERIC(24, 6),
    manual_price_updated_at TIMESTAMPTZ,
    units                   NUMERIC(24, 8) NOT NULL CHECK (units > 0),
    avg_cost                NUMERIC(24, 6) NOT NULL CHECK (avg_cost >= 0),
    acquired_at             DATE NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- exactly one source: a catalog symbol, or a complete manual instrument
    CONSTRAINT holdings_source_chk CHECK (
        (symbol IS NOT NULL AND manual_name IS NULL AND manual_class IS NULL AND manual_currency IS NULL
            AND manual_price IS NULL AND manual_price_updated_at IS NULL)
        OR
        (symbol IS NULL AND manual_name IS NOT NULL AND manual_class IS NOT NULL AND manual_currency IS NOT NULL
            AND manual_price IS NOT NULL AND manual_price_updated_at IS NOT NULL AND manual_price >= 0)
    )
);

CREATE INDEX holdings_user_idx ON holdings (user_id, created_at, id);

CREATE TABLE news_bookmarks (
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    article_id TEXT NOT NULL REFERENCES news_articles (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, article_id)
);

CREATE INDEX news_bookmarks_user_idx ON news_bookmarks (user_id, created_at DESC);

-- +goose Down
DROP TABLE news_bookmarks;
DROP TABLE holdings;
