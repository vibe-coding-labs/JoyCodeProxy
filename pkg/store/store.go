package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

const (
	DefaultDBDir  = ".joycode-proxy"
	DefaultDBName = "proxy.db"
	encKeyFile    = ".enc_key"
)

type Account struct {
	APIKey       string `json:"api_key"`
	APIToken     string `json:"api_token"`
	PtKey        string `json:"-"`
	UserID       string `json:"user_id"`
	IsDefault    bool   `json:"is_default"`
	DefaultModel string `json:"default_model"`
	CreatedAt    string `json:"created_at,omitempty"`
}

type AccountInfo struct {
	APIKey       string `json:"api_key"`
	APIToken     string `json:"api_token"`
	UserID       string `json:"user_id"`
	IsDefault    bool   `json:"is_default"`
	DefaultModel string `json:"default_model"`
	CreatedAt    string `json:"created_at,omitempty"`
}

type Stats struct {
	TotalRequests int            `json:"total_requests"`
	AccountsCount int            `json:"accounts_count"`
	AvgLatencyMs  float64        `json:"avg_latency_ms"`
	ByModel       []ModelCount   `json:"by_model"`
	ByAccount     []AccountCount `json:"by_account"`
}

type ModelCount struct {
	Model string `json:"model"`
	Count int    `json:"count"`
}

type AccountCount struct {
	APIKey string `json:"api_key"`
	Count  int    `json:"count"`
}

type AccountStats struct {
	APIKey        string          `json:"api_key"`
	TotalRequests int             `json:"total_requests"`
	ByModel       []ModelCount    `json:"by_model"`
	ByEndpoint    []EndpointCount `json:"by_endpoint"`
	AvgLatencyMs  float64         `json:"avg_latency_ms"`
	StreamCount   int             `json:"stream_count"`
	ErrorCount    int             `json:"error_count"`
}

type EndpointCount struct {
	Endpoint string `json:"endpoint"`
	Count    int    `json:"count"`
}

type RequestLog struct {
	ID         int64  `json:"id"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	Endpoint   string `json:"endpoint"`
	Stream     bool   `json:"stream"`
	StatusCode int    `json:"status_code"`
	LatencyMs  int64  `json:"latency_ms"`
	CreatedAt  string `json:"created_at"`
}

type Store struct {
	db     *sql.DB
	enc    cipher.AEAD
	mu     sync.Mutex
	dbPath string
}

func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DefaultDBDir)
	return filepath.Join(dir, DefaultDBName), nil
}

func Open(dbPath string) (*Store, error) {
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath}

	encKey, err := s.loadOrCreateEncKey(dir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("encryption key: %w", err)
	}

	block, err := aes.NewCipher(encKey)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	s.enc, err = cipher.NewGCM(block)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func generateToken() string {
	b := make([]byte, 32)
	io.ReadFull(rand.Reader, b)
	return "sk-joy-" + hex.EncodeToString(b)
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			api_key TEXT PRIMARY KEY,
			api_token TEXT NOT NULL DEFAULT '',
			pt_key TEXT NOT NULL,
			user_id TEXT NOT NULL,
			is_default INTEGER DEFAULT 0,
			default_model TEXT DEFAULT '',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			api_key TEXT,
			model TEXT,
			endpoint TEXT,
			stream INTEGER DEFAULT 0,
			status_code INTEGER,
			latency_ms INTEGER,
			created_at TEXT DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		return err
	}

	// Migration: add api_token column to existing DBs
	s.db.Exec("ALTER TABLE accounts ADD COLUMN api_token TEXT NOT NULL DEFAULT ''")

	// Generate tokens for accounts missing one
	rows, err := s.db.Query("SELECT api_key FROM accounts WHERE api_token = ''")
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		token := generateToken()
		s.db.Exec("UPDATE accounts SET api_token = ? WHERE api_key = ?", token, key)
	}
	return nil
}

// --- Encryption ---

func (s *Store) loadOrCreateEncKey(dir string) ([]byte, error) {
	keyPath := filepath.Join(dir, encKeyFile)
	data, err := os.ReadFile(keyPath)
	if err == nil {
		key, err := hex.DecodeString(string(data))
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return key, nil
}

func (s *Store) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, s.enc.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := s.enc.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func (s *Store) decrypt(ciphertext string) (string, error) {
	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	nonceSize := s.enc.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := s.enc.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// --- Account CRUD ---

func (s *Store) AddAccount(apiKey, ptKey, userID string, isDefault bool, defaultModel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		return fmt.Errorf("encrypt pt_key: %w", err)
	}

	if isDefault {
		s.db.Exec("UPDATE accounts SET is_default = 0 WHERE is_default = 1")
	}

	def := 0
	if isDefault {
		def = 1
	}

	token := generateToken()
	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO accounts (api_key, api_token, pt_key, user_id, is_default, default_model) VALUES (?, ?, ?, ?, ?, ?)",
		apiKey, token, encPtKey, userID, def, defaultModel,
	)
	return err
}

func (s *Store) ListAccounts() ([]AccountInfo, error) {
	rows, err := s.db.Query("SELECT api_key, api_token, user_id, is_default, default_model, created_at FROM accounts ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountInfo
	for rows.Next() {
		var a AccountInfo
		var isDef int
		if err := rows.Scan(&a.APIKey, &a.APIToken, &a.UserID, &isDef, &a.DefaultModel, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.IsDefault = isDef == 1
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (s *Store) GetAccount(apiKey string) (*Account, error) {
	var a Account
	var encPtKey string
	var isDef int
	err := s.db.QueryRow(
		"SELECT api_key, api_token, pt_key, user_id, is_default, default_model, created_at FROM accounts WHERE api_key = ?",
		apiKey,
	).Scan(&a.APIKey, &a.APIToken, &encPtKey, &a.UserID, &isDef, &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = isDef == 1
	return &a, nil
}

func (s *Store) GetAccountByToken(token string) (*Account, error) {
	var a Account
	var encPtKey string
	var isDef int
	err := s.db.QueryRow(
		"SELECT api_key, api_token, pt_key, user_id, is_default, default_model, created_at FROM accounts WHERE api_token = ?",
		token,
	).Scan(&a.APIKey, &a.APIToken, &encPtKey, &a.UserID, &isDef, &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = isDef == 1
	return &a, nil
}

func (s *Store) RenewToken(apiKey string) (string, error) {
	token := generateToken()
	_, err := s.db.Exec("UPDATE accounts SET api_token = ?, updated_at = datetime('now') WHERE api_key = ?", token, apiKey)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) GetDefaultAccount() (*Account, error) {
	var a Account
	var encPtKey string
	err := s.db.QueryRow(
		"SELECT api_key, api_token, pt_key, user_id, is_default, default_model, created_at FROM accounts WHERE is_default = 1 LIMIT 1",
	).Scan(&a.APIKey, &a.APIToken, &encPtKey, &a.UserID, new(int), &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = true
	return &a, nil
}

func (s *Store) RemoveAccount(apiKey string) error {
	_, err := s.db.Exec("DELETE FROM accounts WHERE api_key = ?", apiKey)
	return err
}

func (s *Store) SetDefault(apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE accounts SET is_default = 0, updated_at = datetime('now')"); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE accounts SET is_default = 1, updated_at = datetime('now') WHERE api_key = ?", apiKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateAccountModel(apiKey, model string) error {
	_, err := s.db.Exec(
		"UPDATE accounts SET default_model = ?, updated_at = datetime('now') WHERE api_key = ?",
		model, apiKey,
	)
	return err
}

// --- Settings ---

func (s *Store) GetSettings() (map[string]string, error) {
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))",
		key, value,
	)
	return err
}

func (s *Store) SetSettings(settings map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for k, v := range settings {
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))",
			k, v,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- Request Logging ---

func (s *Store) LogRequest(apiKey, model, endpoint string, stream bool, statusCode int, latencyMs int64) error {
	sInt := 0
	if stream {
		sInt = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO request_logs (api_key, model, endpoint, stream, status_code, latency_ms) VALUES (?, ?, ?, ?, ?, ?)",
		apiKey, model, endpoint, sInt, statusCode, latencyMs,
	)
	return err
}

func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}

	err := s.db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&stats.TotalRequests)
	if err != nil {
		return nil, err
	}

	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM request_logs").Scan(&stats.AvgLatencyMs)

	s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.AccountsCount)

	rows, err := s.db.Query("SELECT model, COUNT(*) as cnt FROM request_logs GROUP BY model ORDER BY cnt DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mc ModelCount
		if err := rows.Scan(&mc.Model, &mc.Count); err != nil {
			return nil, err
		}
		stats.ByModel = append(stats.ByModel, mc)
	}

	rows2, err := s.db.Query("SELECT api_key, COUNT(*) as cnt FROM request_logs GROUP BY api_key ORDER BY cnt DESC")
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var ac AccountCount
		if err := rows2.Scan(&ac.APIKey, &ac.Count); err != nil {
			return nil, err
		}
		stats.ByAccount = append(stats.ByAccount, ac)
	}

	return stats, nil
}

func (s *Store) GetAccountStats(apiKey string) (*AccountStats, error) {
	as := &AccountStats{APIKey: apiKey}

	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ?", apiKey).Scan(&as.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM request_logs WHERE api_key = ?", apiKey).Scan(&as.AvgLatencyMs)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND stream = 1", apiKey).Scan(&as.StreamCount)
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE api_key = ? AND status_code >= 400", apiKey).Scan(&as.ErrorCount)

	rows, err := s.db.Query("SELECT model, COUNT(*) as cnt FROM request_logs WHERE api_key = ? GROUP BY model ORDER BY cnt DESC", apiKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mc ModelCount
		if err := rows.Scan(&mc.Model, &mc.Count); err != nil {
			return nil, err
		}
		as.ByModel = append(as.ByModel, mc)
	}

	rows2, err := s.db.Query("SELECT endpoint, COUNT(*) as cnt FROM request_logs WHERE api_key = ? GROUP BY endpoint ORDER BY cnt DESC", apiKey)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var ec EndpointCount
		if err := rows2.Scan(&ec.Endpoint, &ec.Count); err != nil {
			return nil, err
		}
		as.ByEndpoint = append(as.ByEndpoint, ec)
	}

	return as, nil
}

func (s *Store) GetAccountLogs(apiKey string, limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		"SELECT id, api_key, model, endpoint, stream, status_code, latency_ms, created_at FROM request_logs WHERE api_key = ? ORDER BY id DESC LIMIT ?",
		apiKey, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var streamInt int
		if err := rows.Scan(&l.ID, &l.APIKey, &l.Model, &l.Endpoint, &streamInt, &l.StatusCode, &l.LatencyMs, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Stream = streamInt == 1
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (s *Store) GetRecentLogs(limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		"SELECT id, api_key, model, endpoint, stream, status_code, latency_ms, created_at FROM request_logs ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var streamInt int
		if err := rows.Scan(&l.ID, &l.APIKey, &l.Model, &l.Endpoint, &streamInt, &l.StatusCode, &l.LatencyMs, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Stream = streamInt == 1
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// ReassignLogs maps old api_key values in request_logs to a new api_key.
func (s *Store) ReassignLogs(oldKeys []string, newKey string) (int64, error) {
	ph := "?"
	for i := 1; i < len(oldKeys); i++ {
		ph += ",?"
	}
	args := make([]interface{}, len(oldKeys)+1)
	args[0] = newKey
	for i, k := range oldKeys {
		args[i+1] = k
	}
	result, err := s.db.Exec("UPDATE request_logs SET api_key = ? WHERE api_key IN ("+ph+")", args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// EnsureDataDir ensures the data directory exists with correct permissions.
func EnsureDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DefaultDBDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// Copy from os.ReadFile pattern — used to check if DB exists.
func DBExists() bool {
	path, err := DefaultDBPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

