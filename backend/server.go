package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

//go:embed web
var webFiles embed.FS

// Setup states, in order. The web app sends the visitor to the wizard until "ready".
const (
	StateNeedsOwner = "needs_owner"
	StateNeedsChild = "needs_child"
	StateReady      = "ready"
)

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	DB       *DB
	Sessions Sessions
	Now      func() time.Time
}

// Routes wires every endpoint.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/owner", s.handleCreateOwner)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/onboarding/draft", s.auth(s.handleGetDraft))
	mux.HandleFunc("PUT /api/onboarding/draft", s.auth(s.handlePutDraft))
	mux.HandleFunc("POST /api/onboarding/complete", s.auth(s.handleCompleteOnboarding))
	mux.HandleFunc("GET /api/children", s.auth(s.handleListChildren))
	mux.HandleFunc("GET /api/children/{id}/summary", s.auth(s.handleChildSummary))

	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(static)
	mux.HandleFunc("GET /onboarding", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "onboarding.html")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		state, err := s.setupState(r.Context())
		if err != nil {
			http.Error(w, "database is not reachable", http.StatusServiceUnavailable)
			return
		}
		if state != StateReady {
			http.Redirect(w, r, "/onboarding", http.StatusFound)
			return
		}
		http.ServeFileFS(w, r, static, "index.html")
	})
	mux.Handle("GET /", files)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
			// Writes must be JSON: a plain HTML form on another site cannot send that.
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.ContentLength != 0 &&
				!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type contextKey string

const userKey contextKey = "userID"

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := s.Sessions.UserID(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign in first")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, userID)))
	}
}

func userID(r *http.Request) string { return r.Context().Value(userKey).(string) }

func (s *Server) setupState(ctx context.Context) (string, error) {
	users, err := s.DB.Count(ctx, Cypher("MATCH (u:User) RETURN count(u) AS n", nil))
	if err != nil {
		return "", err
	}
	if users == 0 {
		return StateNeedsOwner, nil
	}
	children, err := s.DB.Count(ctx, Cypher("MATCH (c:Child) RETURN count(c) AS n", nil))
	if err != nil {
		return "", err
	}
	if children == 0 {
		return StateNeedsChild, nil
	}
	return StateReady, nil
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	state, err := s.setupState(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	_, authErr := s.Sessions.UserID(r)
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "signedIn": authErr == nil})
}

type credentials struct {
	Username string `json:"username"`
	Email    string `json:"email"` // optional; older clients sign in with it
	Password string `json:"password"`
}

// login returns what the visitor typed to identify themselves.
func (c credentials) login() string {
	if c.Username != "" {
		return strings.ToLower(strings.TrimSpace(c.Username))
	}
	return strings.ToLower(strings.TrimSpace(c.Email))
}

func (s *Server) handleCreateOwner(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !readJSON(w, r, &in) {
		return
	}
	state, err := s.setupState(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if state != StateNeedsOwner {
		writeError(w, http.StatusConflict, "an owner account already exists; sign in instead")
		return
	}
	username, err := NormalizeUsername(in.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil || len(in.Email) > 200 {
			writeError(w, http.StatusBadRequest, "email is not valid")
			return
		}
	}
	if len(in.Password) < 10 || len(in.Password) > 200 {
		writeError(w, http.StatusBadRequest, "password must be 10 to 200 characters")
		return
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		serverError(w, err)
		return
	}
	id := NewID("user")
	_, err = s.DB.Run(r.Context(), Cypher(
		"CREATE (u:User {id: $id, username: $username, passwordHash: $hash, role: 'owner', createdAt: $now})",
		map[string]any{"id": id, "username": username, "hash": hash, "now": s.Now().UTC().Format(time.RFC3339)}), "")
	if err == nil && in.Email != "" {
		_, err = s.DB.Run(r.Context(), Cypher("MATCH (u:User {id: $id}) SET u.email = $email",
			map[string]any{"id": id, "email": in.Email}), "")
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.Sessions.Issue(w, id)
	writeJSON(w, http.StatusCreated, map[string]string{"state": StateNeedsChild})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !readJSON(w, r, &in) {
		return
	}
	rows, err := s.DB.Run(r.Context(), Cypher(
		"MATCH (u:User) WHERE u.username = $login OR u.email = $login RETURN u.id AS id, u.passwordHash AS hash",
		map[string]any{"login": in.login()}), "")
	if err != nil {
		serverError(w, err)
		return
	}
	var id, hash string
	if len(rows) > 0 {
		id, _ = rows[0]["id"].(string)
		hash, _ = rows[0]["hash"].(string)
	}
	if id == "" || !CheckPassword(hash, in.Password) {
		time.Sleep(400 * time.Millisecond) // slow down guessing
		writeError(w, http.StatusUnauthorized, "username or password is wrong")
		return
	}
	s.Sessions.Issue(w, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.Sessions.Clear(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Run(r.Context(), Cypher(
		"MATCH (u:User {id: $id}) RETURN u.onboardingDraft AS draft", map[string]any{"id": userID(r)}), "")
	if err != nil {
		serverError(w, err)
		return
	}
	draft := json.RawMessage("null")
	if len(rows) > 0 {
		if text, ok := rows[0]["draft"].(string); ok && json.Valid([]byte(text)) {
			draft = json.RawMessage(text)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft})
}

func (s *Server) handlePutDraft(w http.ResponseWriter, r *http.Request) {
	var draft json.RawMessage
	if !readJSON(w, r, &draft) {
		return
	}
	_, err := s.DB.Run(r.Context(), Cypher(
		"MATCH (u:User {id: $id}) SET u.onboardingDraft = $draft",
		map[string]any{"id": userID(r), "draft": string(draft)}), "")
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleCompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	var in OnboardingInput
	if !readJSON(w, r, &in) {
		return
	}
	now := s.Now()
	if err := in.Validate(now); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	childID := NewID("child")
	statements := in.Statements(userID(r), childID, now, func() string { return NewID("observation") })
	statements = append(statements, Cypher(
		"MATCH (u:User {id: $id}) SET u.onboardingDraft = ''", map[string]any{"id": userID(r)}))
	if err := s.DB.InTx(r.Context(), statements); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"childId": childID, "writes": len(statements) - 1, "state": StateReady})
}

func (s *Server) handleListChildren(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Run(r.Context(), Cypher(
		`MATCH (u:User {id: $id})-[:GUARDIAN_OF]->(c:Child)
RETURN c.id AS id, c.nickname AS nickname, c.level AS level, c.grade AS grade, c.birthDate AS birthDate`,
		map[string]any{"id": userID(r)}), "")
	if err != nil {
		serverError(w, err)
		return
	}
	if rows == nil {
		rows = []Row{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"children": rows})
}

func (s *Server) handleChildSummary(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Run(r.Context(), Cypher(
		`MATCH (u:User {id: $userId})-[:GUARDIAN_OF]->(c:Child {id: $childId})
MATCH (c)-[r]->(x)
RETURN type(r) AS relation, count(x) AS n`,
		map[string]any{"userId": userID(r), "childId": r.PathValue("id")}), "")
	if err != nil {
		serverError(w, err)
		return
	}
	if rows == nil {
		rows = []Row{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"relations": rows})
}

func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	if err := dec.Decode(into); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		} else {
			writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err) // details stay in the log, not in the response
	writeError(w, http.StatusInternalServerError, "the server could not complete the request")
}
