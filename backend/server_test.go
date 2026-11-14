package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeArcade imitates the few ArcadeDB endpoints the backend uses and records commands.
type fakeArcade struct {
	mu        sync.Mutex
	commands  []map[string]any
	users     int
	children  int
	migrated  map[string]bool
	userHash  string
	committed int
}

func (f *fakeArcade) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/api/v1/server", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"result":"ok"}`) })
	mux.HandleFunc("/api/v1/begin/education", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("arcadedb-session-id", "AS-1")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/commit/education", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.committed++
		f.children++
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/command/education", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "root" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.commands = append(f.commands, body)
		cmd, _ := body["command"].(string)
		params, _ := body["params"].(map[string]any)
		switch {
		case strings.Contains(cmd, "FROM SchemaMigration"):
			n := 0
			if f.migrated[params["version"].(string)] {
				n = 1
			}
			json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{"n": n}}})
		case strings.HasPrefix(cmd, "INSERT INTO SchemaMigration"):
			f.migrated[params["version"].(string)] = true
			io.WriteString(w, `{"result":[]}`)
		case strings.Contains(cmd, "count(u)"):
			json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{"n": f.users}}})
		case strings.Contains(cmd, "count(c)"):
			json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{"n": f.children}}})
		case strings.HasPrefix(cmd, "CREATE (u:User"):
			f.users++
			f.userHash = params["hash"].(string)
			io.WriteString(w, `{"result":[]}`)
		case strings.Contains(cmd, "u.passwordHash AS hash") && f.users == 0:
			io.WriteString(w, `{"result":[]}`)
		case strings.Contains(cmd, "SET u.passwordHash"):
			f.userHash = params["hash"].(string)
			io.WriteString(w, `{"result":[]}`)
		case strings.Contains(cmd, "u.passwordHash AS hash"):
			json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{"id": "user_1", "hash": f.userHash}}})
		default:
			io.WriteString(w, `{"result":[]}`)
		}
	})
	return mux
}

func newTestServer(t *testing.T) (*fakeArcade, *httptest.Server) {
	t.Helper()
	fake := &fakeArcade{migrated: map[string]bool{}}
	arcade := httptest.NewServer(fake.handler())
	t.Cleanup(arcade.Close)
	db := NewDB(arcade.URL, "education", "root", "secret")
	ctx := context.Background()
	if err := db.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureDatabase(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	app := &Server{DB: db, Sessions: Sessions{Secret: []byte("0123456789abcdef0123456789abcdef")},
		Now: func() time.Time { return time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC) }}
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	return fake, srv
}

func call(t *testing.T, c *http.Client, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

const sampleInput = `{
 "child":{"nickname":"Raka","birthDate":"2012-09-03"},
 "position":{"level":"junior","grade":8},
 "school":{"name":"SMP Contoh","type":"private","curriculum":"cambridge","startYear":2025},
 "baseline":[{"domain":"numeracy","level":2,"enjoyment":2},{"domain":"computing","level":3,"enjoyment":0}],
 "interests":[{"name":"Robotika","score":4}],
 "sports":[{"name":"Renang","level":3,"enjoyment":3,"hoursPerWeek":2},{"name":"Aikido","level":3,"enjoyment":0,"hoursPerWeek":3}],
 "methods":["Sempoa"],
 "competitions":[{"name":"Lomba Poster Kota","type":"general","year":2023,"tier":"local","result":"Juara 2"}],
 "goals":{"weeklyHourCap":12,"majors":["Informatika"],"aiConsent":true}
}`

func TestInstallToOnboardingFlow(t *testing.T) {
	fake, srv := newTestServer(t)

	// Migrations and seed ran once and are recorded.
	if !fake.migrated["001_schema"] || !fake.migrated[seedVersion] {
		t.Fatalf("migrations not recorded: %v", fake.migrated)
	}

	jar := &cookieJar{}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// A fresh install sends the visitor to the wizard.
	res, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/onboarding" {
		t.Fatalf("fresh install: got %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if code, body := call(t, client, "GET", srv.URL+"/api/setup/status", nil); code != 200 || body["state"] != StateNeedsOwner {
		t.Fatalf("status: %d %v", code, body)
	}

	// The API is closed until an owner exists and is signed in.
	if code, _ := call(t, client, "POST", srv.URL+"/api/onboarding/complete", json.RawMessage(sampleInput)); code != http.StatusUnauthorized {
		t.Fatalf("complete without session: %d", code)
	}
	if code, _ := call(t, client, "POST", srv.URL+"/api/setup/owner", map[string]string{"username": "parent", "password": "short"}); code != http.StatusBadRequest {
		t.Fatalf("weak password accepted: %d", code)
	}
	if code, body := call(t, client, "POST", srv.URL+"/api/setup/owner", map[string]string{"username": "Parent", "email": "Parent@Example.com", "password": "a-long-password"}); code != http.StatusCreated {
		t.Fatalf("create owner: %d %v", code, body)
	}
	if code, _ := call(t, client, "POST", srv.URL+"/api/setup/owner", map[string]string{"username": "other", "password": "a-long-password"}); code != http.StatusConflict {
		t.Fatalf("second owner: %d", code)
	}
	if code, body := call(t, client, "GET", srv.URL+"/api/setup/status", nil); body["state"] != StateNeedsChild || body["signedIn"] != true {
		t.Fatalf("status after owner: %d %v", code, body)
	}

	// Draft round trip, then a rejected and an accepted completion.
	if code, _ := call(t, client, "PUT", srv.URL+"/api/onboarding/draft", map[string]any{"step": 3}); code != 200 {
		t.Fatalf("save draft: %d", code)
	}
	if code, body := call(t, client, "POST", srv.URL+"/api/onboarding/complete", map[string]any{"child": map[string]string{"nickname": ""}}); code != http.StatusBadRequest || !strings.Contains(body["error"].(string), "nickname") {
		t.Fatalf("invalid input: %d %v", code, body)
	}
	before := len(fake.commands)
	code, body := call(t, client, "POST", srv.URL+"/api/onboarding/complete", json.RawMessage(sampleInput))
	if code != http.StatusCreated || body["state"] != StateReady {
		t.Fatalf("complete: %d %v", code, body)
	}
	if fake.committed != 1 {
		t.Fatalf("transaction not committed")
	}
	for _, c := range fake.commands[before:] {
		if c["language"] != "cypher" {
			t.Fatalf("onboarding wrote with %v", c["language"])
		}
	}

	// Now the home page is served instead of the wizard.
	res, _ = client.Get(srv.URL + "/")
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("home after onboarding: %d", res.StatusCode)
	}

	// Sign in again with the stored hash.
	fresh := &http.Client{Jar: &cookieJar{}}
	if code, _ := call(t, fresh, "POST", srv.URL+"/api/login", map[string]string{"username": "parent", "password": "a-long-password"}); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code, _ := call(t, fresh, "POST", srv.URL+"/api/login", map[string]string{"username": "parent", "password": "wrong-password"}); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	fake, _ := newTestServer(t)
	n := len(fake.commands)
	// A second run against the same fake must not re-apply anything.
	arcade := httptest.NewServer(fake.handler())
	defer arcade.Close()
	again := NewDB(arcade.URL, "education", "root", "secret")
	if err := Migrate(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if err := Seed(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	// prepare table + one "already applied?" check per migration and per seed
	if extra := len(fake.commands) - n; extra != 5 {
		t.Fatalf("second run sent %d commands, want 5", extra)
	}
}

func TestStatementsAndRules(t *testing.T) {
	today := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	var in OnboardingInput
	if err := json.Unmarshal([]byte(sampleInput), &in); err != nil {
		t.Fatal(err)
	}
	if err := in.Validate(today); err != nil {
		t.Fatal(err)
	}
	n := 0
	st := in.Statements("user_1", "child_1", today, func() string { n++; return "obs" })
	joined := ""
	for _, s := range st {
		joined += s.Command + "\n"
		for k := range s.Params {
			if !strings.Contains(s.Command, "$"+k) {
				t.Errorf("param %q is not used in: %s", k, s.Command)
			}
		}
	}
	for _, want := range []string{"GUARDIAN_OF", "ENROLLED_IN", "ATTENDS", "MASTERS", "INTERESTED_IN", "PRACTICES", "LEARNS_WITH", "PARTICIPATES_IN", "AIMS_FOR"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing relation %s", want)
		}
	}
	if n != 2 {
		t.Errorf("observations created: %d, want 2", n)
	}
	// Grade 8 means stages A, B, C are done and D is active: four ENROLLED_IN writes.
	if got := strings.Count(joined, "ENROLLED_IN"); got != 4 {
		t.Errorf("ENROLLED_IN writes: %d, want 5", got)
	}
	// Enjoyment is written only when it was answered.
	if got := strings.Count(joined, "m.enjoyment"); got != 1 {
		t.Errorf("m.enjoyment writes: %d, want 1", got)
	}

	cases := []struct {
		birth string
		want  int
	}{{"2019-06-30", 1}, {"2019-07-02", 0}, {"2012-09-03", 7}, {"2012-05-01", 8}, {"2006-01-01", 13}, {"2022-01-01", 0}}
	for _, c := range cases {
		b, _ := time.Parse("2006-01-02", c.birth)
		if got := SuggestGrade(b, today); got != c.want {
			t.Errorf("SuggestGrade(%s) = %d, want %d", c.birth, got, c.want)
		}
	}
	if StageForGrade(8) != "national_d" || StageForGrade(10) != "national_e" || StageForGrade(0) != "" {
		t.Error("StageForGrade mapping is wrong")
	}
	if Slug("  Lomba Poster Kota! ") != "lomba_poster_kota" {
		t.Errorf("Slug: %q", Slug("  Lomba Poster Kota! "))
	}
	hash, _ := HashPassword("a-long-password")
	if !CheckPassword(hash, "a-long-password") || CheckPassword(hash, "other") {
		t.Error("password hashing is wrong")
	}
	if len(splitSQL("-- c\nCREATE A;\n\nCREATE B;\n")) != 2 {
		t.Error("splitSQL is wrong")
	}
}

// cookieJar is a minimal jar: one host, no expiry handling.
type cookieJar struct {
	mu      sync.Mutex
	cookies map[string]*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cookies == nil {
		j.cookies = map[string]*http.Cookie{}
	}
	for _, c := range cs {
		j.cookies[c.Name] = c
	}
}

func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []*http.Cookie
	for _, c := range j.cookies {
		if c.MaxAge >= 0 {
			out = append(out, c)
		}
	}
	return out
}
