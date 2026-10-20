package app

// Hostile-input edge cases for the API, driven through the real router and a real database.
// Case ids (BE-nn) match the edge case report. Needs SEC_TEST_DATABASE_URL with all migrations applied;
// the tests create users with the e-mail pattern sec-*@edge.test and delete them afterwards.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/platform/config"
)

const (
	edgePassword = "Passw0rd!edge"
	edgeSecret   = "k-edge-case-secret-0123456789-abcdefghij"
)

var edgeCounter atomic.Int64

type edgeEnv struct {
	t    *testing.T
	r    http.Handler
	pool *pgxpool.Pool
	addr string // when set, every request comes from this address (rate limit tests)
}

func newEdgeEnv(t *testing.T) *edgeEnv {
	t.Helper()
	url := os.Getenv("SEC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SEC_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE email LIKE 'sec-%@edge.test'`) }
	cleanup()
	t.Cleanup(func() { cleanup(); pool.Close() })
	cfg := config.Config{Env: "development", CORSOrigins: []string{"http://localhost:3000"},
		Auth: config.AuthConfig{JWTSecret: edgeSecret, AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, RememberTTL: time.Hour}}
	r, err := NewRouter(Deps{Config: cfg, DB: pool, Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	return &edgeEnv{t: t, r: r, pool: pool}
}

// leaks are fragments no response may ever contain: database errors, stack traces, file paths, secrets.
var leaks = []string{"SQLSTATE", "pgx", "pq:", "goroutine ", "panic(", "/Users/", "password_hash", "runtime error", ".go:", "PRIVATE KEY", edgeSecret}

func (e *edgeEnv) do(method, path, body string, h map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = e.addr
	if req.RemoteAddr == "" {
		n := edgeCounter.Add(1)
		req.RemoteAddr = fmt.Sprintf("198.51.%d.%d:4000", (n/250)%250, n%250+1) // a different client each call
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	all := w.Body.String() + fmt.Sprint(w.Header())
	for _, l := range leaks {
		if strings.Contains(all, l) {
			e.t.Errorf("%s %s leaked %q: %.200s", method, path, l, w.Body.String())
		}
	}
	return w
}

func (e *edgeEnv) no5xx(w *httptest.ResponseRecorder, what string) {
	e.t.Helper()
	if w.Code >= 500 {
		e.t.Errorf("%s: status %d, body %.200s", what, w.Code, w.Body.String())
	}
}

func dataField(t *testing.T, w *httptest.ResponseRecorder, path ...string) string {
	t.Helper()
	var cur any
	if err := json.Unmarshal(w.Body.Bytes(), &cur); err != nil {
		t.Fatalf("not json: %.200s", w.Body.String())
	}
	cur = cur.(map[string]any)["data"]
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("no %s in %.200s", p, w.Body.String())
		}
		cur = m[p]
	}
	s, _ := cur.(string)
	return s
}

type edgeUser struct{ email, id, token string }

func uniqueEmail(label string) string {
	return fmt.Sprintf("sec-%s-%d@edge.test", label, time.Now().UnixNano())
}

// newUser registers, marks the address verified directly in the database, and signs in.
func (e *edgeEnv) newUser(label string) edgeUser {
	e.t.Helper()
	email := uniqueEmail(label)
	body := fmt.Sprintf(`{"name":"Edge %s","email":%q,"password":%q}`, label, email, edgePassword)
	if w := e.do("POST", "/api/v1/auth/register", body, nil); w.Code != http.StatusCreated {
		e.t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET email_verified = true WHERE email = $1`, email); err != nil {
		e.t.Fatal(err)
	}
	w := e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, edgePassword), nil)
	if w.Code != http.StatusOK {
		e.t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return edgeUser{email: email, id: dataField(e.t, w, "user", "id"), token: dataField(e.t, w, "accessToken")}
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func (e *edgeEnv) userCount() (n int) {
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n)
	return
}

func (e *edgeEnv) sub(name string, f func(t *testing.T)) {
	e.t.Helper()
	e.t.Run(name, f)
}

// ---------------------------------------------------------------------------------------------- injection

func TestBE01to03SQLInjection(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("sqli")
	before := e.userCount()

	e.sub("BE-01 login fields carry SQL", func(t *testing.T) {
		for _, body := range []string{
			`{"email":"admin'--@x.com","password":"x"}`,
			fmt.Sprintf(`{"email":%q,"password":"' OR '1'='1"}`, u.email),
			fmt.Sprintf(`{"email":%q,"password":"x'; DROP TABLE users;--"}`, u.email),
			`{"email":"a@b.com' UNION SELECT password_hash FROM users--","password":"xxxxxxxx"}`,
		} {
			w := e.do("POST", "/api/v1/auth/login", body, nil)
			if w.Code != 401 && w.Code != 422 {
				t.Errorf("%s -> %d", body, w.Code)
			}
		}
		if e.userCount() != before {
			t.Fatal("users table changed")
		}
	})

	e.sub("BE-02 search parameters carry SQL and control bytes", func(t *testing.T) {
		for _, q := range []string{"%27%20OR%201%3D1--", "%27%3B%20DROP%20TABLE%20assets%3B--", "%00", "%25%27", "a%00b", "%5C", strings.Repeat("a", 5000)} {
			w := e.do("GET", "/api/v1/assets/search?q="+q+"&class="+q, "", bearer(u.token))
			e.no5xx(w, "search q="+q[:min(len(q), 30)])
		}
	})

	e.sub("BE-03 a hostile name is stored as plain text", func(t *testing.T) {
		email := uniqueEmail("sqli-name")
		name := `Robert'); DROP TABLE users;--`
		body := fmt.Sprintf(`{"name":%q,"email":%q,"password":%q}`, name, email, edgePassword)
		if w := e.do("POST", "/api/v1/auth/register", body, nil); w.Code != 201 {
			t.Fatalf("register: %d %s", w.Code, w.Body.String())
		}
		var stored string
		if err := e.pool.QueryRow(context.Background(), `SELECT name FROM users WHERE email = $1`, email).Scan(&stored); err != nil || stored != name {
			t.Fatalf("stored %q err %v", stored, err)
		}
	})
}

// ---------------------------------------------------------------------------------------------- tokens

func (e *edgeEnv) forge(method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func TestBE04to09TokenForgery(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("jwt")
	now := time.Now()
	good := jwt.RegisteredClaims{Subject: u.id, Issuer: "shenzen", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}
	with := func(f func(c *jwt.RegisteredClaims)) jwt.RegisteredClaims { c := good; f(&c); return c }
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q,"iss":"shenzen","exp":%d}`, u.id, now.Add(time.Hour).Unix())))

	// sanity: the genuine token works, so a 401 below means the forgery was refused
	if w := e.do("GET", "/api/v1/auth/me", "", bearer(u.token)); w.Code != 200 {
		t.Fatalf("genuine token refused: %d", w.Code)
	}
	forged := map[string]string{
		"BE-04 alg none (library)":     e.forge(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, good),
		"BE-04 alg none (hand built)":  base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + payload + ".",
		"BE-04 alg none empty segment": base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + payload,
		"BE-05 wrong secret":           e.forge(jwt.SigningMethodHS256, []byte("another-secret-another-secret-0000"), good),
		"BE-05 other HMAC algorithm":   e.forge(jwt.SigningMethodHS512, []byte(edgeSecret), good),
		"BE-06 expired":                e.forge(jwt.SigningMethodHS256, []byte(edgeSecret), with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute)) })),
		"BE-06 no expiry":              e.forge(jwt.SigningMethodHS256, []byte(edgeSecret), with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil })),
		"BE-07 wrong issuer":           e.forge(jwt.SigningMethodHS256, []byte(edgeSecret), with(func(c *jwt.RegisteredClaims) { c.Issuer = "evil" })),
		"BE-08 empty subject":          e.forge(jwt.SigningMethodHS256, []byte(edgeSecret), with(func(c *jwt.RegisteredClaims) { c.Subject = "" })),
		"BE-08 unknown user":           e.forge(jwt.SigningMethodHS256, []byte(edgeSecret), with(func(c *jwt.RegisteredClaims) { c.Subject = "u_does_not_exist" })),
		"BE-08 sql subject":            e.forge(jwt.SigningMethodHS256, []byte(edgeSecret), with(func(c *jwt.RegisteredClaims) { c.Subject = "x' OR '1'='1" })),
		"BE-04 tampered payload":       strings3(u.token, payload),
		"BE-04 garbage":                "not.a.jwt",
		"BE-04 empty segments":         "..",
		"BE-04 huge":                   strings.Repeat("A", 100000),
	}
	for name, tok := range forged {
		e.sub(name, func(t *testing.T) {
			w := e.do("GET", "/api/v1/auth/me", "", bearer(tok))
			if w.Code != 401 {
				t.Errorf("got %d, want 401: %.150s", w.Code, w.Body.String())
			}
		})
	}

	e.sub("BE-09 Authorization header variants", func(t *testing.T) {
		for _, h := range []string{"", "Bearer", "Bearer ", "bearer " + u.token, "Basic " + u.token, "Token " + u.token, u.token, "Bearer  " + u.token, "Bearer " + u.token + " extra"} {
			w := e.do("GET", "/api/v1/auth/me", "", map[string]string{"Authorization": h})
			if w.Code != 401 {
				t.Errorf("%.40q -> %d", h, w.Code)
			}
		}
		// a token in the query string or a cookie is not a credential
		if w := e.do("GET", "/api/v1/auth/me?access_token="+u.token, "", nil); w.Code != 401 {
			t.Errorf("query token -> %d", w.Code)
		}
		if w := e.do("GET", "/api/v1/auth/me", "", map[string]string{"Cookie": "access_token=" + u.token}); w.Code != 401 {
			t.Errorf("cookie token -> %d", w.Code)
		}
	})
}

// strings3 swaps the payload of a genuine token for another one and keeps the old signature.
func strings3(token, payload string) string {
	parts := strings.Split(token, ".")
	return parts[0] + "." + payload + "." + parts[2]
}

// ---------------------------------------------------------------------------------------------- access control

func TestBE10OneUserCannotTouchAnothersHoldings(t *testing.T) {
	e := newEdgeEnv(t)
	a, b := e.newUser("idor-a"), e.newUser("idor-b")
	create := `{"manual":{"name":"Deposito rahasia A","assetClass":"deposit","currency":"IDR","currentPrice":1000000},"units":1,"avgCost":1000000,"acquiredAt":"2026-01-01"}`
	w := e.do("POST", "/api/v1/portfolio/holdings/", create, bearer(a.token))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := dataField(t, w, "id")
	if id == "" {
		t.Fatalf("no id in %s", w.Body.String())
	}
	e.sub("BE-10 PATCH and DELETE by another user", func(t *testing.T) {
		if w := e.do("PATCH", "/api/v1/portfolio/holdings/"+id, `{"units":999}`, bearer(b.token)); w.Code != 404 {
			t.Errorf("patch -> %d", w.Code)
		}
		if w := e.do("DELETE", "/api/v1/portfolio/holdings/"+id, "", bearer(b.token)); w.Code != 404 {
			t.Errorf("delete -> %d", w.Code)
		}
	})
	e.sub("BE-10 the other user's list never shows it", func(t *testing.T) {
		if w := e.do("GET", "/api/v1/portfolio/holdings/", "", bearer(b.token)); strings.Contains(w.Body.String(), "Deposito rahasia A") || strings.Contains(w.Body.String(), id) {
			t.Error("holding leaked into another user's list")
		}
	})
	e.sub("BE-10 the owner still has it, untouched", func(t *testing.T) {
		if w := e.do("GET", "/api/v1/portfolio/holdings/", "", bearer(a.token)); !strings.Contains(w.Body.String(), id) {
			t.Error("owner lost the holding")
		}
	})
	e.sub("BE-23 hostile ids are a 404, not an error", func(t *testing.T) {
		for _, bad := range []string{"..%2F..%2Fetc%2Fpasswd", "%00", "x'%20OR%20'1'%3D'1", strings.Repeat("a", 3000), "%2e%2e", "1%3B%20DROP%20TABLE%20users"} {
			w := e.do("DELETE", "/api/v1/portfolio/holdings/"+bad, "", bearer(a.token))
			e.no5xx(w, "id "+bad[:min(len(bad), 20)])
		}
	})
	e.sub("BE-10 mass assignment of the owner", func(t *testing.T) {
		w := e.do("PATCH", "/api/v1/portfolio/holdings/"+id, fmt.Sprintf(`{"units":2,"userId":%q,"user_id":%q}`, b.id, b.id), bearer(a.token))
		e.no5xx(w, "patch with userId")
		if w := e.do("GET", "/api/v1/portfolio/holdings/", "", bearer(b.token)); strings.Contains(w.Body.String(), id) {
			t.Error("a holding was reassigned to another user")
		}
	})
}

func TestBE25DeletedAccountTokenStopsWorking(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("deleted")
	w := e.do("DELETE", "/api/v1/auth/account", fmt.Sprintf(`{"password":%q,"confirm":"DELETE"}`, edgePassword), bearer(u.token))
	if w.Code != 200 && w.Code != 204 {
		t.Fatalf("delete account: %d %s", w.Code, w.Body.String())
	}
	if w := e.do("GET", "/api/v1/auth/me", "", bearer(u.token)); w.Code != 401 && w.Code != 404 {
		t.Errorf("old token on a deleted account -> %d, want 401", w.Code)
	}
	if w := e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, u.email, edgePassword), nil); w.Code != 401 {
		t.Errorf("login after deletion -> %d", w.Code)
	}
}

func TestBE26RefreshCookieHandling(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("refresh")
	e.sub("BE-26 a forged refresh cookie is refused", func(t *testing.T) {
		for _, c := range []string{"garbage", strings.Repeat("A", 5000), "' OR 1=1--", ""} {
			w := e.do("POST", "/api/v1/auth/refresh", "{}", map[string]string{"Cookie": "refresh_token=" + c})
			if w.Code != 401 && w.Code != 400 {
				t.Errorf("%.20q -> %d", c, w.Code)
			}
		}
	})
	e.sub("BE-26 the session cookie is httpOnly and SameSite", func(t *testing.T) {
		w := e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, u.email, edgePassword), nil)
		cookie := w.Header().Get("Set-Cookie")
		if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite") {
			t.Errorf("weak cookie flags: %s", cookie)
		}
		if strings.Contains(w.Body.String(), "refresh") && strings.Contains(strings.ToLower(w.Body.String()), "refreshtoken") {
			t.Error("the refresh token must not be in the JSON body")
		}
	})
}

// ---------------------------------------------------------------------------------------------- brute force and rate limits

func TestBE11FailedLoginsAreLimitedAndTheLimitCannotBeSpoofed(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("brute")
	e.addr = "203.0.113.9:5555"
	var codes []int
	for i := 0; i < 9; i++ {
		// a different X-Forwarded-For each time: with no trusted proxy it must change nothing
		w := e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"wrong-%d"}`, u.email, i), map[string]string{"X-Forwarded-For": fmt.Sprintf("10.0.0.%d", i+1), "X-Real-IP": fmt.Sprintf("10.9.9.%d", i+1)})
		codes = append(codes, w.Code)
	}
	if codes[0] != 401 || codes[len(codes)-1] != 429 {
		t.Fatalf("status sequence %v: want 401s and then 429", codes)
	}
	// the right password is refused as well while the lock lasts
	if w := e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, u.email, edgePassword), map[string]string{"X-Forwarded-For": "10.0.0.99"}); w.Code != 429 {
		t.Errorf("correct password during lock -> %d, want 429", w.Code)
	}
}

func TestBE19VerificationCodeBruteForce(t *testing.T) {
	e := newEdgeEnv(t)
	email := uniqueEmail("otp")
	if w := e.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"name":"Otp","email":%q,"password":%q}`, email, edgePassword), nil); w.Code != 201 {
		t.Fatalf("register: %d", w.Code)
	}
	var last int
	for i := 0; i < 8; i++ {
		w := e.do("POST", "/api/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":"%d%d%d%d%d%d"}`, email, i, i, i, i, i, i), nil)
		last = w.Code
		if w.Code == 200 || w.Code >= 500 {
			t.Fatalf("attempt %d -> %d", i, w.Code)
		}
	}
	var attempts int
	_ = e.pool.QueryRow(context.Background(), `SELECT coalesce(max(attempts),0) FROM auth_otps o JOIN users u ON u.id = o.user_id WHERE u.email = $1`, email).Scan(&attempts)
	if attempts < 5 || attempts > 8 {
		t.Errorf("attempts recorded: %d", attempts)
	}
	if last == 200 {
		t.Error("code accepted")
	}
	var verified bool
	_ = e.pool.QueryRow(context.Background(), `SELECT email_verified FROM users WHERE email = $1`, email).Scan(&verified)
	if verified {
		t.Error("account verified without the code")
	}
}

func TestBE29ExportIsRateLimited(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("export")
	e.addr = "203.0.113.40:1000"
	var codes []int
	for i := 0; i < 5; i++ {
		codes = append(codes, e.do("GET", "/api/v1/auth/export", "", bearer(u.token)).Code)
	}
	if codes[len(codes)-1] != 429 {
		t.Errorf("export statuses %v: the data export must be limited", codes)
	}
}

// ---------------------------------------------------------------------------------------------- request shape

func TestBE12to15BodyAbuse(t *testing.T) {
	e := newEdgeEnv(t)
	reg := "/api/v1/auth/register"

	e.sub("BE-12 a 2 MB body is refused, not read", func(t *testing.T) {
		big := fmt.Sprintf(`{"name":"%s","email":"a@edge.test","password":"%s"}`, strings.Repeat("x", 2<<20), edgePassword)
		if w := e.do("POST", reg, big, nil); w.Code < 400 || w.Code >= 500 {
			t.Errorf("got %d", w.Code)
		}
	})
	e.sub("BE-13 mass assignment through unknown fields", func(t *testing.T) {
		email := uniqueEmail("mass")
		w := e.do("POST", reg, fmt.Sprintf(`{"name":"M","email":%q,"password":%q,"email_verified":true,"emailVerified":true,"id":"admin","role":"admin"}`, email, edgePassword), nil)
		if w.Code == 201 {
			var verified bool
			_ = e.pool.QueryRow(context.Background(), `SELECT email_verified FROM users WHERE email = $1`, email).Scan(&verified)
			if verified {
				t.Fatal("a client set its own email_verified flag")
			}
		} else if w.Code != 400 && w.Code != 422 {
			t.Errorf("got %d", w.Code)
		}
	})
	e.sub("BE-14 deeply nested JSON", func(t *testing.T) {
		w := e.do("POST", reg, strings.Repeat("[", 200000)+strings.Repeat("]", 200000), nil)
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("got %d", w.Code)
		}
	})
	e.sub("BE-15 malformed bodies", func(t *testing.T) {
		for name, body := range map[string]string{
			"empty": "", "not json": "hello", "array": "[]", "null": "null", "number": "1", "truncated": `{"name":`,
			"two objects": `{"name":"a"} {"name":"b"}`, "bom": "\ufeff{}", "nan": `{"name":NaN}`, "wrong types": `{"name":1,"email":[],"password":{}}`,
			"null bytes":   "{\"name\":\"a\x00b\",\"email\":\"a@edge.test\",\"password\":\"Passw0rd!edge\"}",
			"escaped null": `{"name":"a\u0000b","email":"a@edge.test","password":"Passw0rd!edge"}`,
		} {
			w := e.do("POST", reg, body, nil)
			if w.Code < 400 || w.Code >= 500 {
				t.Errorf("%s -> %d", name, w.Code)
			}
		}
		// a form body or a wrong content type must not be parsed as JSON credentials
		w := e.do("POST", "/api/v1/auth/login", "email=a%40edge.test&password=x", map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("form body -> %d", w.Code)
		}
	})
}

func TestBE16to18AccountEdgeCases(t *testing.T) {
	e := newEdgeEnv(t)
	reg := func(name, email, password string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"name": name, "email": email, "password": password})
		return e.do("POST", "/api/v1/auth/register", string(b), nil)
	}

	e.sub("BE-16 passwords at the limits", func(t *testing.T) {
		cases := map[string]struct {
			pw   string
			want []int
		}{
			"7 chars":                  {"Abcde1!", []int{422}},
			"72 ascii chars":           {"Aa1" + strings.Repeat("a", 69), []int{201}},
			"73 chars":                 {strings.Repeat("a", 73), []int{422}},
			"72 two-byte chars":        {strings.Repeat("é", 72), []int{201, 422}}, // 144 bytes: bcrypt's 72 byte limit
			"36 emoji (144 bytes)":     {strings.Repeat("\U0001F600", 36), []int{201, 422}},
			"all spaces":               {strings.Repeat(" ", 12), []int{201, 422}},
			"null bytes":               {"pass\x00word123", []int{201, 400, 422}},
			"10 KB":                    {strings.Repeat("a", 10000), []int{422}},
			"sql":                      {"' OR '1'='1' --", []int{201}},
			"only a unicode combining": {"éééééé", []int{201, 422}},
		}
		for name, c := range cases {
			w := reg("P", uniqueEmail("pw"), c.pw)
			ok := false
			for _, want := range c.want {
				ok = ok || w.Code == want
			}
			if !ok {
				t.Errorf("%s -> %d, want one of %v: %.120s", name, w.Code, c.want, w.Body.String())
			}
		}
	})

	e.sub("BE-17 e-mail case and spacing give one account", func(t *testing.T) {
		base := fmt.Sprintf("sec-case-%d@edge.test", time.Now().UnixNano())
		upper := strings.ToUpper(base[:len(base)-len("@edge.test")]) + "@Edge.Test"
		if w := reg("C", base, edgePassword); w.Code != 201 {
			t.Fatalf("first: %d", w.Code)
		}
		w := reg("C", upper, edgePassword)
		var n int
		_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE lower(email) = lower($1)`, base).Scan(&n)
		if n != 1 {
			t.Errorf("%d accounts for one address (second register -> %d)", n, w.Code)
		}
		for _, bad := range []string{" " + base, base + " ", "a b@edge.test", "a@b@edge.test", strings.Repeat("a", 250) + "@edge.test", "é@edge.test"} {
			if w := reg("C", bad, edgePassword); w.Code >= 500 {
				t.Errorf("%q -> %d", bad, w.Code)
			}
		}
	})

	e.sub("BE-18 the API does not reveal which addresses have accounts", func(t *testing.T) {
		u := e.newUser("enum")
		strip := func(w *httptest.ResponseRecorder) string {
			var m map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &m)
			delete(m, "requestId")
			b, _ := json.Marshal(m)
			return fmt.Sprintf("%d %s", w.Code, b)
		}
		wrongPw := strip(e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"Wrong-pass-1"}`, u.email), nil))
		unknown := strip(e.do("POST", "/api/v1/auth/login", `{"email":"nobody-here@edge.test","password":"Wrong-pass-1"}`, nil))
		if wrongPw != unknown {
			t.Errorf("login differs: known=%s unknown=%s", wrongPw, unknown)
		}
		resendKnown := strip(e.do("POST", "/api/v1/auth/resend-otp", fmt.Sprintf(`{"email":%q}`, u.email), nil))
		resendUnknown := strip(e.do("POST", "/api/v1/auth/resend-otp", `{"email":"nobody-here@edge.test"}`, nil))
		if resendKnown != resendUnknown {
			t.Errorf("resend differs: known=%s unknown=%s", resendKnown, resendUnknown)
		}
		forgotKnown := strip(e.do("POST", "/api/v1/auth/forgot-password", fmt.Sprintf(`{"email":%q}`, u.email), nil))
		forgotUnknown := strip(e.do("POST", "/api/v1/auth/forgot-password", `{"email":"nobody-here@edge.test"}`, nil))
		if forgotKnown != forgotUnknown {
			t.Errorf("forgot-password differs: known=%s unknown=%s", forgotKnown, forgotUnknown)
		}
		dupe := e.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"name":"D","email":%q,"password":%q}`, u.email, edgePassword), nil)
		fresh := e.do("POST", "/api/v1/auth/register", fmt.Sprintf(`{"name":"D","email":%q,"password":%q}`, uniqueEmail("fresh"), edgePassword), nil)
		// known and accepted: registering an address that already has an account answers 409 so a person who forgot
		// they have one is told to sign in. Login, resend and forgot-password above do not reveal it.
		if dupe.Code != http.StatusConflict || fresh.Code != http.StatusCreated {
			t.Errorf("register status changed: existing -> %d, new -> %d; update the report (R-23)", dupe.Code, fresh.Code)
		}
	})

	e.sub("BE-28 an unverified account cannot sign in", func(t *testing.T) {
		email := uniqueEmail("unverified")
		if w := reg("U", email, edgePassword); w.Code != 201 {
			t.Fatalf("register: %d", w.Code)
		}
		w := e.do("POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, edgePassword), nil)
		if w.Code == 200 || strings.Contains(w.Body.String(), "accessToken") {
			t.Errorf("unverified login -> %d", w.Code)
		}
	})
}

// ---------------------------------------------------------------------------------------------- browser-facing controls

func TestBE20CORS(t *testing.T) {
	e := newEdgeEnv(t)
	for _, origin := range []string{"https://evil.example", "null", "http://localhost:3000.evil.example", "http://localhost.evil.example", "http://localhost:3001", "https://localhost:3000", ""} {
		for _, method := range []string{"GET", "OPTIONS"} {
			w := e.do(method, "/api/v1/auth/me", "", map[string]string{"Origin": origin, "Access-Control-Request-Method": "GET"})
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("%s with Origin %q was allowed: %q", method, origin, got)
			}
		}
	}
	w := e.do("OPTIONS", "/api/v1/auth/login", "", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST"})
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("the configured origin was not allowed: %v", w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Error("wildcard origin with credentials")
	}
}

func TestBE21SecurityHeadersOnEveryKindOfResponse(t *testing.T) {
	e := newEdgeEnv(t)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"404":         e.do("GET", "/nope", "", nil),
		"405":         e.do("PUT", "/api/v1/auth/login", "{}", nil),
		"401":         e.do("GET", "/api/v1/auth/me", "", nil),
		"422":         e.do("POST", "/api/v1/auth/login", "{}", nil),
		"health":      e.do("GET", "/healthz", "", nil),
		"preflight":   e.do("OPTIONS", "/api/v1/auth/login", "", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST"}),
		"bad request": e.do("POST", "/api/v1/auth/login", "{", nil),
	} {
		for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Strict-Transport-Security", "Content-Security-Policy", "Cache-Control", "Referrer-Policy"} {
			if w.Header().Get(h) == "" {
				t.Errorf("%s response is missing %s", name, h)
			}
		}
	}
	if got := e.do("GET", "/api/v1/auth/me", "", nil).Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("personal data may be cached: %q", got)
	}
}

func TestBE22to24RequestPlumbing(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("plumb")

	e.sub("BE-22 the inbound request id cannot inject or bloat", func(t *testing.T) {
		for _, rid := range []string{strings.Repeat("x", 10000), "id with spaces", `quote"semi;<b>x</b>`, "éé", "a\tb"} {
			w := e.do("GET", "/nope", "", map[string]string{"X-Request-Id": rid})
			got := w.Header().Get("X-Request-Id")
			if len(got) > 64 || strings.ContainsAny(got, " \"<>;\t\r\n") {
				t.Errorf("request id %.30q was echoed as %.40q", rid, got)
			}
		}
	})
	e.sub("BE-23 odd paths", func(t *testing.T) {
		for _, p := range []string{"/api/v1/../../etc/passwd", "/api/v1/%2e%2e/%2e%2e/etc/passwd", "//api//v1//auth//me", "/api/v1/auth/me/", "/api/v1/auth/me%00", "/.env", "/api/v1/auth/%252e%252e/me", "/api/v1/" + strings.Repeat("a/", 3000)} {
			w := e.do("GET", p, "", bearer(u.token))
			e.no5xx(w, p[:min(len(p), 40)])
		}
	})
	e.sub("BE-24 method confusion", func(t *testing.T) {
		if w := e.do("POST", "/api/v1/auth/me", "{}", map[string]string{"X-HTTP-Method-Override": "DELETE", "Authorization": "Bearer " + u.token}); w.Code != 405 && w.Code != 404 {
			t.Errorf("POST on a GET route -> %d", w.Code)
		}
		for _, m := range []string{"TRACE", "CONNECT", "PROPFIND", "PATCH"} {
			e.no5xx(e.do(m, "/api/v1/auth/me", "", bearer(u.token)), m)
		}
		if w := e.do("HEAD", "/healthz", "", nil); w.Code >= 500 {
			t.Errorf("HEAD -> %d", w.Code)
		}
		if w := e.do("DELETE", "/api/v1/auth/me", "", map[string]string{"Authorization": "Bearer " + u.token}); w.Code == 200 {
			t.Error("DELETE accepted on /me")
		}
	})
}

func TestBE27ErrorsNeverLeakInternals(t *testing.T) {
	e := newEdgeEnv(t)
	u := e.newUser("leak")
	// every response goes through the leak check inside e.do; this drives the error paths on purpose
	for _, p := range []struct{ method, path, body string }{
		{"POST", "/api/v1/auth/register", `{"name":"` + strings.Repeat("a", 500) + `","email":"x","password":"y"}`},
		{"POST", "/api/v1/auth/login", `{"email":"a@b.c","password":"` + strings.Repeat("a", 500) + `"}`},
		{"GET", "/api/v1/portfolio/holdings/", ""},
		{"POST", "/api/v1/profile/answers", `{"q1":` + strings.Repeat("9", 400) + `}`},
		{"PUT", "/api/v1/profile/answers", `{"nope":[1,2,3]}`},
		{"POST", "/api/v1/profile/submit", `{}`},
		{"GET", "/api/v1/dashboard/summary?currency=%00", ""},
		{"GET", "/api/v1/news?limit=-1&offset=99999999999999999999", ""},
		{"GET", "/api/v1/discover?class=%27", ""},
		{"POST", "/api/v1/advisory/chat", `{"message":"` + strings.Repeat("a", 5000) + `"}`},
		{"POST", "/api/v1/advisory/chat", `{"message":"hai","runId":"../x"}`},
	} {
		w := e.do(p.method, p.path, p.body, bearer(u.token))
		if w.Code >= 500 && w.Code != 502 && w.Code != 503 && w.Code != 504 {
			t.Errorf("%s %s -> %d: %.150s", p.method, p.path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Header().Get("Content-Type"), "json") && w.Code != 404 && w.Code != 405 {
			t.Errorf("%s %s answered with %q", p.method, p.path, w.Header().Get("Content-Type"))
		}
	}
}
