# backend (Go)

Go 1.27, chi, pgx/v5, goose (migrations), sqlc (queries), slog.

## Run

```
cp .env.example .env
make run          # :8080
make check        # fmt + lint + test
make migrate-up   # needs DATABASE_URL
```

Health: `GET /healthz` (liveness), `GET /readyz` (pings the database when `DATABASE_URL` is set).

## Architecture: vertical slices + DDD

```
cmd/api/                  entrypoint: config, wiring, graceful shutdown
internal/app/             composition root: builds each slice and mounts its routes
internal/features/<slice>/
    domain/               entities, value rules, repository ports. Pure Go, no HTTP, no SQL
    application/          use cases (one service per slice), orchestrates domain + ports
    infrastructure/       adapters for the ports (Postgres/sqlc, memory)
    transport/            HTTP handlers, DTOs, validation tags, route table
internal/platform/        cross-cutting technical code: config, logger, httpx, database, aiclient
internal/shared/apperr/   the single error type shared by every layer
db/migrations, db/queries goose + sqlc inputs
```

Rules:
- A slice owns everything for its feature. Slices never import each other; share through `platform` or `shared`, or via an application-level port.
- Dependencies point inward: transport -> application -> domain; infrastructure -> domain.
- Handlers have the signature `func(w, r) error`; return an `apperr`, never write error responses by hand.
- Invariants live in the domain constructor (`domain.New`), request-shape checks in DTO `validate` tags.
- `internal/features/note` is a working template (in-memory repo). Copy it for a new slice, then delete it when the first real slice lands.

Contract: [docs/error-contract.md](docs/error-contract.md).

## Adding a Postgres-backed slice

1. `make migrate-new name=create_things`, write SQL.
2. Put queries in `db/queries/things.sql`, run `make sqlc` (generates into `internal/platform/database/sqlcgen`).
3. Implement the domain repository in `infrastructure/` on top of the generated code; translate `pgx.ErrNoRows` to `NOT_FOUND` and unique violations to `CONFLICT`.

## Auth slice

`internal/features/auth` (domain / application / infrastructure / transport). Endpoints under `/api/v1/auth`:

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/register` | create unverified account, email a 6-digit OTP |
| POST | `/verify-email` | consume OTP, mark verified, start session |
| POST | `/resend-otp` | new OTP (60s cooldown, never reveals accounts) |
| POST | `/login` | email + password (`remember` extends the refresh cookie to 30 days) |
| POST | `/google` | exchange a Google access token for a session |
| POST | `/forgot-password`, `/reset-password` | OTP-based reset, ends all sessions |
| POST | `/refresh`, `/logout` | rotate / revoke the `refresh_token` httpOnly cookie |
| GET | `/me` | current user (Bearer access token) |

Access token: 15 min JWT (HS256) returned in JSON. Refresh token: opaque, stored hashed, rotated on every use; a replay after a 10s grace window revokes every session of the user.

Config (`.env`): `JWT_SECRET` (32+ chars), `GOOGLE_CLIENT_ID`, `SMTP_*`, `MAIL_FROM`, TTLs. With no `SMTP_HOST` the OTP is printed to the log (development only; production requires SMTP). Auth is mounted only when `DATABASE_URL` is set; run `make migrate-up` first.

## Profile slice

`internal/features/profile`: questionnaire v4.0, consent, autosave, deterministic rule engine (YAML Steps 1-7), immutable assessments, AI explanation. Contract: `../documentation/profiling-contract.md`. Rules decide, the AI service only words the result.

All routes need `Authorization: Bearer <access token>` and live under `/api/v1/profile`:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/questionnaire` | embedded `domain/questionnaire.json` (EN/ID text, options, limits, follow-up) |
| GET | `/` | status `none|draft|completed`, consent, draft answers, latest result, `canRetake` |
| POST | `/consent` | `{accepted:true, whatsappOptIn}`; required before any answers are saved (403 otherwise) |
| PUT | `/answers` | autosave: validates only the keys sent, merges into the draft, returns `missing`; `null` clears an answer; `optOut` replaces opt-outs (scoped to `section` when given) |
| POST | `/submit` | full validation (422 per field), rule engine, stores assessment (input + trace + output + versions), asks the AI service (15s, `explanation:null` on any failure) |
| GET | `/result` | latest result, 404 if none |
| POST | `/retake` | reopens a draft seeded from the last answers; 409 with `nextAllowedAt` inside the 30 day cooldown or after 3 assessments in a rolling year |
| DELETE | `/` | erases consent, draft and every assessment (204) |

Notes:
- `Accept-Language: en` makes the explanation English; anything else is Indonesian.
- q8 may not exceed `max(0, q2 - q3 - q6_installment)`, so a user with a negative surplus can still submit with 0.
- `profile_assessments` rows cannot be updated (DB trigger); they are removed only by erasure or `ON DELETE CASCADE` from `users`.
- Config: `AI_SERVICE_URL` (default `http://localhost:8001`), `AI_INTERNAL_API_KEY` (sent as `X-Internal-Api-Key`, falls back to `INTERNAL_API_KEY`). Routes are mounted only when `DATABASE_URL` is set; run `make migrate-up` first.
- Tests: `go test ./internal/features/profile/...` (component tables, boundaries 40/70/3/6/12/25, 36 scenarios, monotonicity properties, 10,000 answer simulation, validation, application tests with fakes).

## Market data

Slice `internal/features/market` (domain / infrastructure), importer `cmd/importer`, classifier `cmd/classifier`. Migration `db/migrations/20261018000001_create_market.sql`. Contract: `../documentation/dashboard-contract.md` section 1.

```
make migrate-up
make import-market        # DATA_DIR defaults to ../data/assets&news, override: make import-market DATA_DIR="/path/with spaces"
make classify-assets      # reads assets + prices_daily, writes asset_classifications
```

Importer: streams every JSON file (never loads the 1.6 GB at once), loads prices with `pgx.CopyFrom` in ~100k row transactions. Idempotent: assets are upserted; prices are replaced per symbol; dividends, yields, fundamentals and news are replaced (delete + COPY in one transaction). Resumable: `import_files` records each loaded price file (size, mtime, cutoff) and a re-run skips them (`IMPORT_ARGS="-force"` reloads, `-only assets,prices,dividends,yields,fundamentals,news` runs steps). Prices keep `trade_date >= newest trade_date in the data - 2 years` (newest is found by reading the tail of each file). It prints progress, the skipped rows with reasons, and final table counts.

### Tables

| Table | Key | Notes |
|-------|-----|-------|
| `assets` | `symbol` | name, `asset_class` (source: equity, etf, gold, commodity, crypto, fx), currency, exchange (JKT, US, LSE, HKG, SHZ, SHH, NSI, SET, GER, TOR, KLS, PAR, SES), country, isin, figi, `has_metadata`. `IDR=X` is typed `fx` (source says commodity). trigram GIN on lower(name) and lower(symbol) |
| `prices_daily` | `(symbol, trade_date)` | open/high/low/close/volume as double precision; crypto has close only; FK to assets; no partitioning (7.3M rows, the PK serves series and latest-row queries) |
| `dividends` | `(symbol, ex_date)` | amount, source currency (GBp = pence) |
| `treasury_yields` | `(trade_date, tenor)` | tenors 1M..30Y, `tenor_months`, `yield_pct` |
| `fundamentals` | `symbol` | sector, industry, market_cap + `info` / `statements` jsonb (100 IDX stocks only) |
| `news_articles` | `id` | `category` capital_markets/macro/global/real_sector, `tickers text[]` (GIN), `search` tsvector over title+summary (GIN, config `simple`), sentiment (always NULL in the source) |
| `asset_classifications` | `symbol` | six dimension labels + `annualized_vol`, `avg_daily_value_idr`, `market_cap_idr`, `alert_threshold_pct`, `trace` jsonb, `classifier_version` |
| `import_files` | `path` | importer bookkeeping |
| `asset_catalog_view` | | assets + last price + classification |

News category rule (`domain/news.go`): known channel decides (CNBC `market`, Bisnis `Bursa & Saham`/`Emiten`/`Perbankan`, Finnhub `company`/`crypto`/`merger` -> capital_markets; `ekonomi`/`Kurs`/`Pajak`/`Nasional` -> macro; `internasional`, Finnhub `top news`/`business` -> global; `Energi & Tambang`/`Telekomunikasi`/`Infrastruktur`/`Properti`... -> real_sector). Ambiguous channels (`all`, unknown) use title keywords, then macro for Indonesian and global for foreign sources.

### Asset Classifier

`domain.Classify(Input) Result` is pure and deterministic (`domain/classifier.go`, `ClassifierVersion` 1.0.0). All six dimensions and thresholds of the YAML are implemented: market cap > Rp 10 triliun, volatility 10% / 25% (std of daily returns x sqrt(252), last 252 returns, population std, at least 30 returns), liquidity Rp 1 / 10 miliar (avg volume x avg close over the last 30 sessions, converted to IDR) with the YAML overrides (deposito, RD pasar uang, RD saham, emas, BTC/ETH), alert thresholds per class. Every result carries a 6-step `trace` (rule text, `assumed` flag when a default stood in for missing data). Anything not derivable is the explicit label `unknown`.

Implementation choices and gaps (the YAML `gap_analysis` is real):
- Sharia status: no ISSI / DSN-MUI / OJK data exists in the repo. Only the YAML `sample_assets` are curated (12 labelled symbols); sukuk are sharia_compliant by definition; everything else is `unknown` (42,271 of 42,283 assets).
- Sector: only the 100 IDX stocks with fundamentals get a sector (yfinance sector/industry mapped to IDX-IC; curated for the samples). Global ETFs and foreign equities are `unknown`.
- Market cap: fundamentals exist for the 100 largest IDX stocks (min Rp 13.7 triliun) = `equity_large_cap`. Other IDX equities have no market cap and are labelled `equity_mid_small` with `assumed: true` in the trace. Foreign equities are `equity_global`; equities whose name matches REIT/DIRE are `property`.
- Liquidity needs an IDR value: only IDR assets and USD assets (via `IDR=X`) can be converted. Other currencies (CNY, HKD, EUR, INR, GBP...) have no FX series in the data, so their liquidity is `unknown` with the reason in the trace (add `<CCY>=X` price files to light them up). Futures volume is in contracts, not notional. Crypto has no volume (only BTC/ETH get `high` through the override).
- Volatility is `unknown` for assets with < 30 returns or no prices (692 German ETFs have no price file). Corporate actions/splits are not adjusted. Crypto has 365 daily points, scaled with sqrt(252) as the YAML formula says.
- Mutual funds, SBN/ORI/SR, deposits, digital gold are not in the data at all; the classifier supports them (`SourceClass` mutual_fund/sbn/bond/sukuk/deposit, tested with the YAML samples) but no rows exist. Holdings of those types are manual instruments in the portfolio feature.
- No alert-rule engine here; `alert_threshold_pct` is stored per asset (for bonds/sukuk it is a yield change, not a price change).

### Data facts and quality (as loaded 2026-10-17)

- `assets.json` had 30,575 rows; `universe/etf_GER.json` adds 6,092 ETFs; 5,616 price files in `prices/US` have no metadata at all, so stubs were created (`has_metadata=false`, name = symbol, USD/US; crypto stubs IDR). Total 42,283 assets, 695 of them without any price row.
- `prices_summary.json` is the only price source for `SPY` and `pax-gold` and is loaded for symbols without their own file.
- Price history in the data is only ~1 year (2025-09-11 .. 2026-10-16), so the 2-year window drops nothing. One row skipped (`PARTY-RE-SM.NS`, close = 0). 6,902 crypto rows have no OHLC/volume.
- `sec_metrics.json` is empty; `no_data.json` is empty; `_rename_map.json` is not needed.

### Read helpers (`internal/features/market/infrastructure`, `infrastructure.NewRepo(pool)`)

```go
func NewRepo(p *pgxpool.Pool) Repo
func (r Repo) GetAsset(ctx, symbol string) (domain.Asset, error)                       // domain.ErrNotFound
func (r Repo) SearchAssets(ctx, q, class string, limit int) ([]domain.AssetHit, error) // class = source or classified class, "" = any
func (r Repo) LatestPrice(ctx, symbol string) (domain.Price, error)
func (r Repo) LatestPrices(ctx, symbols []string) (map[string]domain.Price, error)
func (r Repo) PriceSeries(ctx, symbol string, from, to time.Time) ([]domain.Price, error) // ascending, zero time = open
func (r Repo) FXRate(ctx, from, to string, date time.Time) (float64, error)            // units of `to` per 1 `from`; only USD<->IDR in the data
func (r Repo) Classification(ctx, symbol string) (domain.Classification, error)
func (r Repo) Dividends(ctx, symbol string, since time.Time) ([]domain.Dividend, error)
func (r Repo) LatestYields(ctx) ([]domain.YieldPoint, error)
func (r Repo) Fundamentals(ctx, symbol string) (domain.Fundamentals, error)
func (r Repo) ListNews(ctx, f domain.NewsFilter, cursor string) (domain.NewsPage, error) // keyset on (published_at, id)
func (r Repo) NewsLastUpdated(ctx) (time.Time, error)
func (r Repo) GetArticle(ctx, id string) (domain.Article, error)
func ClassifyAll(ctx, pool, progress func(done, total int)) (int, error)
```

Money: crypto closes are IDR, USD assets need `FXRate(ctx, "USD", "IDR", date)`. Tests: `go test ./internal/features/market/...` (domain: YAML samples, threshold boundaries, 20,000 random inputs; infrastructure: integration tests that skip when the database is empty).

## Portfolio, dashboard and news

Three slices behind auth (`internal/features/{portfolio,dashboard,news}`), mounted only when `DATABASE_URL` is set, wired in `internal/app/dashboard_wiring.go` (adapters between slices and over the market tables; slices only know small interfaces). Migration `db/migrations/20261019100001_create_portfolio.sql` (`holdings`, `news_bookmarks`). Contract: `../documentation/dashboard-contract.md` sections 2 and 4.

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/assets/search?q=&class=&limit=` | catalog search (limit 1-50, default 20; empty `q` returns `[]`) |
| GET / POST | `/portfolio/holdings` | list valued holdings + totals / add a holding (201) |
| PATCH / DELETE | `/portfolio/holdings/{id}` | partial update (units, avgCost, acquiredAt, currentPrice for manual) / delete (204) |
| GET | `/dashboard/home?range=6M\|1Y\|ALL` | main dashboard |
| GET | `/dashboard/portfolio?range=1M\|3M\|6M\|1Y\|ALL` | portfolio dashboard |
| GET | `/news?category=&q=&limit=&cursor=` | feed, newest first, keyset cursor |
| POST / DELETE | `/news/{id}/bookmark` | idempotent, 204 (404 for an unknown article) |
| GET | `/news/bookmarks` | bookmarked articles, newest bookmark first (max 200) |

Holdings rules: `units > 0`, `avgCost >= 0`, `acquiredAt` a date not after today (Jakarta), exactly one of `symbol` (must exist and have prices) or `manual` (`assetClass` bond_sbn|mutual_fund|deposit|other, `currency` IDR, `currentPrice > 0`), at most 200 per user (409), every query is scoped by `user_id` (another user's id answers 404). Validation errors are 422 with per-field `details`.

Valuation (IDR, `portfolio/domain/valuation.go`):
- `avgCost`, manual `currentPrice` and `lastPrice` are IDR per unit. `marketValue = units x lastPrice`, `costBasis = units x avgCost`, `totalReturn = value - cost`, `weightPct = value / sum(values)`.
- Catalog prices: IDR and crypto closes as is, USD x latest `IDR=X`. Any other currency has no FX: the holding is valued at cost with `valuationAvailable: false` (no return, never an error).
- `change1dPct` is the last two closes in the instrument's own currency; `trend1y` is the last year of closes in IDR per unit, downsampled to 30 points (manual instruments are a flat line); `dividendsIDR` = per-share dividends with ex-date on or after `acquiredAt` x units (USD/IDR dividends only).
- History (`History`): daily value and cost over the union of the held symbols' trading dates; a holding counts from `acquiredAt`, uses the last close on or before each date and that date's FX, and cost is constant. Manual-only portfolios use a weekly grid.

Dashboard rules (`dashboard/domain/build.go`):
- Profile sources: q2 income, q3 expenses, q4 liquid savings, q5 dependents, q7 self-reported investment assets, q17 goal, q19 horizon, q22 sharia, latest assessment `finalProfile` (riskLevel: Conservative 1, Moderate 2, Aggressive 3). Read-only SQL on `users`, `profiles`, `profile_assessments`.
- Investment value = valued holdings, or q7 when the user has no holdings. `totalAssets = liquid savings + investment value` (savings are constant over history: no savings history exists). `changePct` of KPIs/total is versus one month earlier. Income, expenses and savings KPIs have flat 12-point sparklines and `changePct` 0.
- `assetTrend.points` are month-end points of the range (ALL = the 2 year data window, leading empty days dropped); `allocation` groups savings + holdings into Stocks, Bonds, Mutual funds, Gold, Crypto, Cash & other (one decimal, sums to exactly 100).
- Portfolio page: `returnYtd` = (profit now - profit on Jan 1) / (value on Jan 1 + cost added since), dividends excluded; performance is capped at 120 points.
- `recommendation` comes from `domain.Recommender`; the default `RulesRecommender` maps the final profile to a fixed table (Conservative 15/50/35, Moderate 50/35/15, Aggressive 70/20/10 stocks/bonds/cash; sharia "yes" says sukuk and sharia stocks). To use the matching engine replace `newRecommender` in `dashboard_wiring.go`; an error from the recommender falls back to the rules.
- No profile: `profileStatus "none"`, zeros, empty arrays, never 404.

News: `category` is validated, an invalid `cursor` is 422, `sentiment` is mapped from the source score to positive (>= 0.15) / neutral / negative (<= -0.15) or `null` (the data has none), `relevantToHoldings` compares article tickers with held symbols ignoring suffixes (`BBCA` matches `BBCA.JK`), `lastUpdatedAt` is the newest article time.

Tests: `go test ./internal/features/{portfolio,dashboard,news}/...` (valuation/FX/history/weights tables and properties, validation, ownership isolation and limit with fakes, HTTP status and envelope).

## Discover (asset screener)

Slice `internal/features/discover` (domain / application / infrastructure / transport), command `cmd/metrics`, migration `db/migrations/20261019200001_create_discover.sql`. Contract: `../documentation/discover-contract.md`.

```
make migrate-up
make refresh-metrics      # rebuilds asset_metrics (~11 s for 42k assets). Run it after make import-market / make classify-assets
```

`asset_metrics` is a plain table, not a materialized view: the derivations are Go domain functions (`domain.Derive`, unit tested) fed by one SQL over `assets`, `prices_daily`, `dividends`, `fundamentals` and `asset_classifications`; the table is truncated and bulk-loaded in one transaction. Page queries read it through one index per sort column and direction (`ORDER BY col DIR NULLS LAST, symbol`), so a page costs 0.3 to 6 ms in Postgres (end-to-end HTTP, worst of 5,076 consecutive pages across all 12 sorts: 81 ms).

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/discover/assets` | `q` (symbol prefix/contains or name contains), `class` (`all` equity etf crypto commodity fx; `commodity` = futures + gold), `exchange` (JKT, US, LSE...), `industry` (case-insensitive exact, use the facet value), `sort` (`marketCap` default, `lastPrice`, `ret7d`, `ret1y`, `divYield`, `name`), `dir` (default desc; asc for name), `limit` (50, max 100), `cursor`, `watchlist=true`. Returns `{items, nextCursor, total, asOf}`; 422 `VALIDATION_FAILED` on bad parameters |
| GET | `/api/v1/discover/facets` | `{classes[{value,labelId,labelEn,count}], exchanges[{value,count}], industries[{value,count}]}` (top 200 industries) |
| PUT / DELETE | `/api/v1/discover/watchlist/{symbol}` | 204 idempotent; PUT 404 for an unknown symbol; stars are per user |

Keyset cursor: opaque base64url of `{sort, dir, last value or null, last symbol}`; it is rejected (422) when used with a different `sort`/`dir`. NULLs sort last in both directions.

Data decisions (made after inspecting the real rows):
- `fundamentals.info` keys: `marketCap, trailingPE, forwardPE, priceToBook, totalRevenue, dividendYield, netIncomeToCommon...`. `dividendYield` is already a percent (BBCA 6.27 with close 5,225, max 14.34), used as is. Statements: `statements.income_stmt[<annual date>]["Net Income" | "Total Revenue"]`.
- Last price: latest close in the asset currency, no conversion (crypto is IDR, futures USD; sorting by price therefore mixes currencies). Source closes are float32-derived, so values like 95.80999755859375 appear.
- `ret7d` / `ret1y`: last close vs the close on or before (asset latest trade date - 7 days / - 1 year). No such row (history shorter than the window) gives `null`. Only 18,368 of 42,283 assets have a 1Y return because most histories start in Oct 2025.
- Valuation: PE if `trailingPE > 0`, else PB if `priceToBook > 0`, else PS = marketCap / totalRevenue. Result on the 100 IDX stocks: 89 PE, 11 PB.
- Growth: the two most recent annual periods with a value. `E` = net income change if the previous is > 0 (percent from a loss or zero is not meaningful), else `R` = revenue change, else null. `netIncomeToCommon` is a single TTM number and is not used for growth. Result: 91 E, 9 R.
- Div yield: fundamentals percent when > 0, else trailing 365-day sum of `dividends` / last close * 100 when `dividends.currency` equals the asset currency (GBp vs GBp ok, never GBp vs GBP). A zero yield is `null` (n/a). Yields above 100% are dropped as bad source data (58 assets, e.g. 453.6 per share dividend on a 1.36 price). 13,612 assets have a yield.
- Industry: fundamentals industry, else the classifier sector humanised (almost always `unknown`, so effectively skipped), else the asset class label in English (Equity, ETF, Cryptocurrency, Commodity, Gold, Currency). 31,091 equities and 11,055 ETFs therefore show `Equity` / `ETF`.
- Market cap: fundamentals only (100 IDX stocks), in IDR; everything else `null`. `analystTarget` is always `null`.
- `assetClass` in items is the raw source class (`gold` is separate from `commodity` there; labels `Emas`/`Gold`); the `commodity` filter and facet cover both.
- Search results follow the chosen sort, not relevance: searching `AAPL` lists matches by symbol when market cap is null for all of them.

## Advisory chat (RAG)

Slice `internal/features/advisory`, wired in `internal/app/advisory_wiring.go`, mounted only when `DATABASE_URL` is set. Migration `db/migrations/20261020100001_create_advisory_chat.sql` (`chat_sessions`, `chat_messages`; the `rag_*` tables belong to the AI service). Contract: `../documentation/advisory-contract.md` section 2. The backend builds the user context (profile facts, holdings weighted by market value, recommended classes and picks from the matching engine; no name, email or phone), sends the last 6 stored messages and calls `POST {AI_SERVICE_URL}/ml/rag/chat` with `X-Internal-Api-Key` (timeout 60 s, no retry). Env: `AI_SERVICE_URL`, `AI_INTERNAL_API_KEY`.

| Method | Path | Notes |
|--------|------|-------|
| POST | `/api/v1/advisory/chat` | body `{sessionId?, message (1..1000), lang: "id"\|"en"}` -> `{sessionId, message{id,role,content,citations,createdAt}, grounded}`. 404 without a completed profile ("Complete the financial profile first") or for another user's session, 429 above 20 messages per user per minute, 502 when the AI service fails (the user message stays stored, the client can retry) |
| GET | `/api/v1/advisory/sessions` | `{items:[{id,title,updatedAt}]}` newest first, max 50 |
| GET | `/api/v1/advisory/sessions/{id}` | `{id,title,messages[{id,role,content,citations,createdAt}]}`, 404 for other users |
| DELETE | `/api/v1/advisory/sessions/{id}` | 204, 404 for other users |

## Matching engine

Slice `internal/features/matching` implements `documentation/YAML MATCHING ENGINE.yaml` as pure Go (`domain/`, no I/O): rules decide every number, the text is a deterministic template (the AI service is not called). Endpoints (auth required, no completed profile = 404):

| Method | Path | Output |
|--------|------|--------|
| GET | `/api/v1/advice/products` | product matching, 5 layers: `portfolio[{class,label{id,en},allocation,currentPct,unallocated,assets[{ticker,name,kind,weight,score,sector,held,reason{id,en}}]}]`, `flags`, `trace{targetAllocation,gatesApplied,gateNotes,exclusions,scoresPerAsset,diversificationActions}`, `summary{id,en}` |
| GET | `/api/v1/advice/daily` | daily content, 4 layers: `alert`, `news` (+`moreNews`), `macro`, `nudge`, `quietMarket`, `degraded[]`, `trace{alerts,news,macro,nudge}` |

Product layers: (1) target allocation from the base ranges at their midpoint, then the YAML modifiers (horizon < 3 y, q10, q5 >= 3, q22 sukuk, q16 crypto/small-cap, ladder <= 2 = only deposits / money-market fund / government bond), normalised to 100; the equity bucket is split large/mid/global/ETF per profile (engine choice, the YAML only gives ranges). (2) Gates: sharia, horizon, Conservative x high volatility, knowledge, ladder. (3) Score = the YAML weighted formula (Sharpe vs the latest 1Y yield, return + positive months, expense ratio, liquidity, PER/PBV/ROE, goal, macro fit). (4) Caps: 10% per asset, 40% per sector, correlation > 0.85 keeps the higher score, class balance flags; holdings win ties and show `held` and `currentPct`. (5) Output + trace. `Recommender` (`application.Recommender`: `Recommend(ctx,userID) -> {textId,textEn,allocation[{label,labelId,pct}],source:"rules",generatedAt}`) feeds `/dashboard/home` through `internal/app/advice_wiring.go`.

Daily layers: alert (strictly greater than the class threshold, e.g. exactly 3.0% on large caps does not fire; corporate action = dividend ex-date in the last 3 days; new 52-week high/low; max 1, price move first; same alert 3 days in a row flips to a "still moving" wording; 5 quiet days -> market summary), news (score = 0.40 holding + 0.25 goal + 0.20 macro + 0.15 profile, threshold 70, filters for Conservative, tier 3 sources never quote numbers, same topic 3 days -> a different topic or "update terbaru"), macro (0.40 magnitude + 0.40 relevance + 0.20 recency, threshold 60, release within one day, USD/IDR must move > 2%), nudge (health score < 40 priorities, then the ladder default, amounts rounded to Rp 50.000, same nudge 3 days in a row is skipped). History comes from `briefing_runs.content`.

Data reality and gaps (all shown in the trace/flags, nothing invented):
- The catalog has equities, ETFs, gold, crypto, property; no mutual funds, SBN, sukuk or deposits. Those classes appear as `kind:"instrument"` entries (no ticker, no score) with a `class_balance_*` flag.
- Sharia status exists for only 12 symbols. With q22 = `yes` an asset must be `sharia_compliant`; `unknown` is excluded because it cannot be verified (stricter than the YAML, which only excludes `non_sharia`).
- q24, q25, q26 are not in questionnaire v4, so the geography, liquidity and exclusion gates are reported as not applied.
- Candidates are the top 40 per class by traded value with known volatility/liquidity and >= 120 price days; the catalog is cached for 30 minutes.
- There is no BI/BPS numeric feed: BI rate, inflation and GDP are Indonesian news headlines (no numbers quoted); only USD/IDR is numeric (`IDR=X`).
- IDX articles carry no ticker tags, so holdings are matched by symbol or company name in the title.

## WhatsApp delivery

Slice `internal/features/whatsapp` implements `documentation/YAML WA DELIVERY.yaml`: slots (alert, news, macro, nudge), at most 4, per-slot limits 180/200/150/180, total <= 800 characters (lowest priority slot dropped first, nudge mandatory), id/en templates, fallbacks (no content, data fetch failure, new user), reply commands, retries and analytics. No emoji is used (the YAML emoji prefixes are replaced by words).

| Method | Path | Notes |
|--------|------|-------|
| GET/PUT | `/api/v1/whatsapp/settings` | `{phone,verified,optIn,sendTimeLocal,timezone,lastSentAt}` plus `frequency`, `paused`, `deliveryIssue`; PUT `{phone,optIn,sendTimeLocal,frequency?,timezone?}`; `08..`/`62..` become `+62..`; a changed number must be verified again |
| POST | `/api/v1/whatsapp/verify/start`, `/verify/confirm` | 6-digit code via GoWA, stored as a hash, 10 min, 5 attempts, 60 s cooldown; confirm body `{code}` |
| GET | `/api/v1/briefing/preview?locale=id\|en` | `{text,slots[{key,text}],charCount,trace}` exactly what would be sent today, no side effects |
| POST | `/api/v1/briefing/send-test` | sends the preview to the verified number |
| POST | `/api/v1/whatsapp/webhook` | public, authenticated by `X-Hub-Signature-256: sha256=<hmac>` over the raw body (constant time); empty `GOWA_WEBHOOK_SECRET` rejects everything |

Commands (whole message, case/punctuation insensitive): `MORE` (two more news), `STOP` (opt-out), `RESUME`, `PROFILE`, `UPDATE` (link to onboarding), `HELP`; questions get a "not available yet" + HELP (chatbot is Feature 2), anything else gets HELP. Each inbound message is logged once (GoWA retries are de-duplicated by message id).

Scheduler: started only when `WA_DAILY_ENABLED=true`; ticks every 30 s; a contact is due from `sendTimeLocal` (user timezone, default Asia/Jakarta, `frequency` daily/weekdays/weekly) for 6 hours. One `briefing_runs` row per user per local date is claimed with `INSERT ... ON CONFLICT`, so repeated ticks or several instances never send twice. A failed send retries after 5 min, 3 attempts per day; 3 failed days in a row set `deliveryIssue` in settings. Opt-in, STOP and verification are honoured. Not enforced by default (no read receipts): the 7-day re-engagement and 30-day auto-pause rules exist as `Policy` fields (off). National-holiday skipping and the Stage 2 reminders are not implemented (the questionnaire is a single stage).

Privacy: numbers are stored as E.164 (unique per verified owner), masked in logs (`+62********890`), verification codes are never stored in plain text or in the message log, and `DELETE /api/v1/profile` also erases `whatsapp_*` and `briefing_runs` (UU PDP). Tables: `whatsapp_contacts`, `whatsapp_verifications`, `whatsapp_messages`, `briefing_runs`, `whatsapp_events` (analytics: briefing_sent, briefing_failed, slot_included, reply, opt_in, opt_out, delivery_issue), migration `20261019000001_create_whatsapp.sql`.

### Run GoWA and point its webhook here

```
git clone https://github.com/aldinokemal/go-whatsapp-web-multidevice && cd go-whatsapp-web-multidevice/src
go run . rest --basic-auth=gowa:CHANGE_ME --webhook="http://localhost:8080/api/v1/whatsapp/webhook" --webhook-secret="CHANGE_ME_TOO"
# or Docker: docker compose up -d --build  (set APP_BASIC_AUTH, WHATSAPP_WEBHOOK, WHATSAPP_WEBHOOK_SECRET in its env)
```

Open `http://localhost:3000`, scan the QR code with the sending phone (log in the device), then in `backend/.env`:

```
GOWA_BASE_URL=http://localhost:3000
GOWA_BASIC_AUTH_USER=gowa
GOWA_BASIC_AUTH_PASSWORD=CHANGE_ME
GOWA_WEBHOOK_SECRET=CHANGE_ME_TOO     # must equal GoWA's --webhook-secret
WA_DAILY_ENABLED=true
```

If the API runs on another host or in Docker, the webhook URL must be reachable from GoWA (for Docker use `http://host.docker.internal:8080/api/v1/whatsapp/webhook`). The client uses `POST /send/message` with `{"phone":"<digits>@s.whatsapp.net","message":...}` and `GET /app/status` (`is_logged_in`). Tests: `go test ./internal/features/matching/... ./internal/features/whatsapp/...` (the GoWA client runs against an `httptest` server that mimics its API).
