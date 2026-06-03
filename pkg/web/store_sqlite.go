package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS scans (
			id         TEXT PRIMARY KEY,
			target     TEXT NOT NULL,
			mode       TEXT NOT NULL DEFAULT 'quick',
			ai         INTEGER NOT NULL DEFAULT 0,
			verify     INTEGER NOT NULL DEFAULT 0,
			sniper     INTEGER NOT NULL DEFAULT 0,
			deep       INTEGER NOT NULL DEFAULT 0,
			status     TEXT NOT NULL DEFAULT 'queued',
			progress   TEXT NOT NULL DEFAULT '',
			report     TEXT NOT NULL DEFAULT '',
			result     TEXT NOT NULL DEFAULT '',
			error      TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_scans_created ON scans(created_at DESC);
	`)
	if err != nil {
		return err
	}
	if err := ensureColumn(db, "scans", "result", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(db, "scans", "ai", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(db, "scans", "verify", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(db, "scans", "sniper", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return ensureColumn(db, "scans", "deep", "INTEGER NOT NULL DEFAULT 0")
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Create(ctx context.Context, job *ScanJob) error {
	normalizeJobAnalysis(job)
	resultJSON := marshalResult(job)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scans (id, target, mode, ai, verify, sniper, deep, status, progress, report, result, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Target, job.Mode, boolToInt(job.AI), boolToInt(job.Verify), boolToInt(job.Sniper), boolToInt(job.Deep),
		string(job.Status), job.Progress, job.Report, resultJSON, job.Error,
		job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*ScanJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, target, mode, ai, verify, sniper, deep, status, progress, report, result, error, created_at, updated_at
		 FROM scans WHERE id = ?`, id)
	return scanRow(row)
}

func (s *SQLiteStore) List(ctx context.Context, limit int) ([]*ScanJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, target, mode, ai, verify, sniper, deep, status, progress, report, result, error, created_at, updated_at
		 FROM scans ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*ScanJob
	for rows.Next() {
		job, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStore) Update(ctx context.Context, job *ScanJob) error {
	normalizeJobAnalysis(job)
	resultJSON := marshalResult(job)
	_, err := s.db.ExecContext(ctx,
		`UPDATE scans SET ai=?, verify=?, sniper=?, deep=?, status=?, progress=?, report=?, result=?, error=?, updated_at=? WHERE id=?`,
		boolToInt(job.AI), boolToInt(job.Verify), boolToInt(job.Sniper), boolToInt(job.Deep),
		string(job.Status), job.Progress, job.Report, resultJSON, job.Error,
		job.UpdatedAt.Format(time.RFC3339Nano), job.ID,
	)
	return err
}

func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scans WHERE id=?`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanFromScanner(sc scanner) (*ScanJob, error) {
	var job ScanJob
	var status, resultJSON, createdAt, updatedAt string
	var ai, verify, sniper, deep int
	err := sc.Scan(&job.ID, &job.Target, &job.Mode, &ai, &verify, &sniper, &deep, &status,
		&job.Progress, &job.Report, &resultJSON, &job.Error, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	job.AI = ai != 0
	job.Verify = verify != 0
	job.Sniper = sniper != 0
	job.Deep = deep != 0
	normalizeJobAnalysis(&job)
	job.Status = ScanStatus(status)
	if resultJSON != "" {
		_ = json.Unmarshal([]byte(resultJSON), &job.Result)
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &job, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeJobAnalysis(job *ScanJob) {
	if job == nil {
		return
	}
	if job.AI && !job.Verify && !job.Sniper {
		job.Verify = true
		job.Sniper = true
	}
	job.AI = job.Verify || job.Sniper
}

func marshalResult(job *ScanJob) string {
	if job == nil || job.Result == nil {
		return ""
	}
	data, err := json.Marshal(job.Result)
	if err != nil {
		return ""
	}
	return string(data)
}

func scanRow(row *sql.Row) (*ScanJob, error) {
	return scanFromScanner(row)
}

func scanRows(rows *sql.Rows) (*ScanJob, error) {
	return scanFromScanner(rows)
}
