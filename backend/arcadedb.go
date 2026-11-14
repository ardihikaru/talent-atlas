package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DB is a minimal client for the ArcadeDB HTTP/JSON API (port 2480).
type DB struct {
	BaseURL  string
	Database string
	User     string
	Password string
	HTTP     *http.Client
}

func NewDB(baseURL, database, user, password string) *DB {
	return &DB{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Database: database,
		User:     user,
		Password: password,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Row is one result row of a query.
type Row map[string]any

// Statement is one command with its bound parameters.
type Statement struct {
	Language string
	Command  string
	Params   map[string]any
}

// Cypher builds a Cypher statement. Values always travel as parameters.
func Cypher(command string, params map[string]any) Statement {
	return Statement{Language: "cypher", Command: command, Params: params}
}

// SQL builds an ArcadeDB SQL statement (parameters bind as :name).
func SQL(command string, params map[string]any) Statement {
	return Statement{Language: "sql", Command: command, Params: params}
}

func (d *DB) do(ctx context.Context, method, path string, body any, sessionID string) (int, []byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.BaseURL+path, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	req.SetBasicAuth(d.User, d.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sessionID != "" {
		req.Header.Set("arcadedb-session-id", sessionID)
	}
	res, err := d.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	return res.StatusCode, raw, res.Header, err
}

// Ready reports whether the server is online. The endpoint needs no auth.
func (d *DB) Ready(ctx context.Context) error {
	status, _, _, err := d.do(ctx, http.MethodGet, "/api/v1/ready", nil, "")
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("arcadedb not ready: HTTP %d", status)
	}
	return nil
}

// WaitReady polls until the server answers or the context ends.
func (d *DB) WaitReady(ctx context.Context) error {
	var last error
	for {
		if last = d.Ready(ctx); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("arcadedb did not become ready: %w", last)
		case <-time.After(2 * time.Second):
		}
	}
}

// EnsureDatabase creates the database when it does not exist yet. A database
// created up front by an operator needs no server-level rights here.
func (d *DB) EnsureDatabase(ctx context.Context) error {
	status, raw, _, err := d.do(ctx, http.MethodGet, "/api/v1/exists/"+url.PathEscape(d.Database), nil, "")
	if err == nil && status == http.StatusOK && bytes.Contains(raw, []byte(`"result":true`)) {
		return nil
	}
	status, raw, _, err = d.do(ctx, http.MethodPost, "/api/v1/server",
		map[string]string{"command": "create database " + d.Database}, "")
	if err != nil {
		return err
	}
	if status/100 == 2 || status == http.StatusConflict ||
		strings.Contains(strings.ToLower(string(raw)), "already exists") {
		return nil
	}
	return fmt.Errorf("create database: HTTP %d: %s", status, truncate(raw))
}

// Run executes one statement. sessionID is empty outside a transaction.
func (d *DB) Run(ctx context.Context, st Statement, sessionID string) ([]Row, error) {
	body := map[string]any{"language": st.Language, "command": st.Command}
	if len(st.Params) > 0 {
		body["params"] = st.Params
	}
	status, raw, _, err := d.do(ctx, http.MethodPost, "/api/v1/command/"+url.PathEscape(d.Database), body, sessionID)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("arcadedb %s command failed: HTTP %d: %s", st.Language, status, truncate(raw))
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode arcadedb response: %w", err)
	}
	var rows []Row
	if len(envelope.Result) > 0 && envelope.Result[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(envelope.Result, &items); err != nil {
			return nil, err
		}
		for _, item := range items {
			row := Row{}
			if err := json.Unmarshal(item, &row); err != nil {
				row = Row{"value": string(item)} // scalar result
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// InTx runs all statements in one server-side transaction.
func (d *DB) InTx(ctx context.Context, statements []Statement) error {
	path := "/api/v1/begin/" + url.PathEscape(d.Database)
	status, raw, header, err := d.do(ctx, http.MethodPost, path, nil, "")
	if err != nil {
		return err
	}
	sessionID := header.Get("arcadedb-session-id")
	if status/100 != 2 || sessionID == "" {
		return fmt.Errorf("begin transaction: HTTP %d: %s", status, truncate(raw))
	}
	for i, st := range statements {
		if _, err := d.Run(ctx, st, sessionID); err != nil {
			_, _, _, _ = d.do(context.WithoutCancel(ctx), http.MethodPost,
				"/api/v1/rollback/"+url.PathEscape(d.Database), nil, sessionID)
			return fmt.Errorf("statement %d: %w", i+1, err)
		}
	}
	status, raw, _, err = d.do(ctx, http.MethodPost, "/api/v1/commit/"+url.PathEscape(d.Database), nil, sessionID)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return fmt.Errorf("commit transaction: HTTP %d: %s", status, truncate(raw))
	}
	return nil
}

// Count returns the integer in column "n" of the first row.
func (d *DB) Count(ctx context.Context, st Statement) (int, error) {
	rows, err := d.Run(ctx, st, "")
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	switch v := rows[0]["n"].(type) {
	case float64:
		return int(v), nil
	case string:
		var n int
		_, err := fmt.Sscanf(v, "%d", &n)
		return n, err
	}
	return 0, nil
}

func truncate(raw []byte) string {
	const max = 400
	if len(raw) > max {
		return string(raw[:max]) + "…"
	}
	return string(raw)
}
