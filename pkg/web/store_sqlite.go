package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/core/output"
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
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS scans (
			id         TEXT PRIMARY KEY,
			target     TEXT NOT NULL,
			mode       TEXT NOT NULL DEFAULT 'quick',
			ai         INTEGER NOT NULL DEFAULT 0,
			verify     INTEGER NOT NULL DEFAULT 0,
			sniper     INTEGER NOT NULL DEFAULT 0,
			deep       INTEGER NOT NULL DEFAULT 0,
			project    TEXT NOT NULL DEFAULT 'default',
			status     TEXT NOT NULL DEFAULT 'queued',
			progress   TEXT NOT NULL DEFAULT '',
			report     TEXT NOT NULL DEFAULT '',
			result     TEXT NOT NULL DEFAULT '',
			error      TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS chat_sessions (
			id         TEXT PRIMARY KEY,
			agent_id   TEXT NOT NULL DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			title      TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS chat_messages (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
			role       TEXT NOT NULL,
			agent_id   TEXT NOT NULL DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			metadata   TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS session_scans (
			session_id TEXT NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
			scan_id    TEXT NOT NULL,
			PRIMARY KEY (session_id, scan_id)
		);

		CREATE TABLE IF NOT EXISTS assets (
			id           TEXT PRIMARY KEY,
			project_id   TEXT NOT NULL DEFAULT 'default',
			target       TEXT NOT NULL,
			label        TEXT NOT NULL DEFAULT '',
			source       TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT '',
			note         TEXT NOT NULL DEFAULT '',
			services     INTEGER NOT NULL DEFAULT 0,
			loots        INTEGER NOT NULL DEFAULT 0,
			last_scan_id TEXT NOT NULL DEFAULT '',
			first_seen   TEXT NOT NULL,
			last_seen    TEXT NOT NULL,
			UNIQUE(project_id, target)
		);

		CREATE TABLE IF NOT EXISTS projects (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
	`); err != nil {
		return err
	}

	for _, column := range []sqliteColumnMigration{
		{table: "scans", name: "mode", definition: "TEXT NOT NULL DEFAULT 'quick'"},
		{table: "scans", name: "ai", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "scans", name: "verify", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "scans", name: "sniper", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "scans", name: "deep", definition: "INTEGER NOT NULL DEFAULT 0"},
		{table: "scans", name: "project", definition: "TEXT NOT NULL DEFAULT 'default'"},
		{table: "scans", name: "progress", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "scans", name: "report", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "scans", name: "result", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "scans", name: "error", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "chat_sessions", name: "agent_id", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "chat_sessions", name: "agent_name", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "chat_sessions", name: "title", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "chat_sessions", name: "status", definition: "TEXT NOT NULL DEFAULT 'active'"},
		{table: "chat_messages", name: "agent_id", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "chat_messages", name: "agent_name", definition: "TEXT NOT NULL DEFAULT ''"},
		{table: "chat_messages", name: "metadata", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureSQLiteColumn(db, column); err != nil {
			return err
		}
	}

	// Pre-project asset tables (global UNIQUE(target), no project_id) are rebuilt
	// into the per-project schema, folding every existing row into the default
	// project so the switch is lossless.
	if err := migrateAssetsProjectScope(db); err != nil {
		return err
	}
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO projects (id, name, created_at) VALUES (?, ?, ?)`,
		DefaultProjectID, "Default", time.Now().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS records (
			id         TEXT PRIMARY KEY,
			type       TEXT NOT NULL,
			scan_id    TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL DEFAULT '',
			agent_id   TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT '',
			target     TEXT NOT NULL DEFAULT '',
			turn       INTEGER NOT NULL DEFAULT 0,
			priority   TEXT NOT NULL DEFAULT '',
			summary    TEXT NOT NULL DEFAULT '',
			loot       INTEGER NOT NULL DEFAULT 0,
			tags       TEXT NOT NULL DEFAULT '',
			data       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
	`); err != nil {
		return err
	}

	_, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_scans_created ON scans(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_sessions_updated ON chat_sessions(updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_sessions_agent ON chat_sessions(agent_id);
		CREATE INDEX IF NOT EXISTS idx_messages_session ON chat_messages(session_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_assets_seen ON assets(project_id, last_seen DESC);
		CREATE INDEX IF NOT EXISTS idx_records_scan ON records(scan_id, type, created_at);
		CREATE INDEX IF NOT EXISTS idx_records_session ON records(session_id, type);
		CREATE INDEX IF NOT EXISTS idx_records_type ON records(type, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_records_priority ON records(priority, type);
	`)
	return err
}

// migrateAssetsProjectScope upgrades a pre-project assets table (global
// UNIQUE(target), no project_id) to the per-project schema: it adds project_id
// and switches uniqueness to UNIQUE(project_id, target). Existing rows are
// assigned to the default project. Idempotent — a no-op once project_id exists,
// so fresh DBs (already created with the new schema) skip the rebuild.
func migrateAssetsProjectScope(db *sql.DB) error {
	has, err := sqliteColumnExists(db, "assets", "project_id")
	if err != nil || has {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec(`DROP TABLE IF EXISTS assets_new`); err != nil {
		return err
	}
	if _, err = tx.Exec(`
		CREATE TABLE assets_new (
			id           TEXT PRIMARY KEY,
			project_id   TEXT NOT NULL DEFAULT 'default',
			target       TEXT NOT NULL,
			label        TEXT NOT NULL DEFAULT '',
			source       TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT '',
			note         TEXT NOT NULL DEFAULT '',
			services     INTEGER NOT NULL DEFAULT 0,
			loots        INTEGER NOT NULL DEFAULT 0,
			last_scan_id TEXT NOT NULL DEFAULT '',
			first_seen   TEXT NOT NULL,
			last_seen    TEXT NOT NULL,
			UNIQUE(project_id, target)
		)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`
		INSERT INTO assets_new (id, project_id, target, label, source, status, note, services, loots, last_scan_id, first_seen, last_seen)
			SELECT id, 'default', target, label, source, status, note, services, loots, last_scan_id, first_seen, last_seen FROM assets
	`); err != nil {
		return err
	}
	if _, err = tx.Exec(`DROP TABLE assets`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE assets_new RENAME TO assets`); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

type sqliteColumnMigration struct {
	table      string
	name       string
	definition string
}

func ensureSQLiteColumn(db *sql.DB, column sqliteColumnMigration) error {
	exists, err := sqliteColumnExists(db, column.table, column.name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s %s",
		quoteSQLiteIdent(column.table),
		quoteSQLiteIdent(column.name),
		column.definition,
	))
	return err
}

func sqliteColumnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdent(table)))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			pk           int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func quoteSQLiteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Create(ctx context.Context, job *ScanJob) error {
	normalizeJobAnalysis(job)
	job.Project = normalizeProjectID(job.Project)
	resultJSON := marshalResult(job)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scans (id, target, mode, ai, verify, sniper, deep, project, status, progress, report, result, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Target, job.Mode, boolToInt(job.AI), boolToInt(job.Verify), boolToInt(job.Sniper), boolToInt(job.Deep),
		job.Project, string(job.Status), job.Progress, job.Report, resultJSON, job.Error,
		job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

// scanColumns is the ordered column list read by scanFromScanner; keep the two
// in sync when the scans schema changes.
const scanColumns = "id, target, mode, ai, verify, sniper, deep, project, status, progress, report, result, error, created_at, updated_at"

func (s *SQLiteStore) Get(ctx context.Context, id string) (*ScanJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+scanColumns+` FROM scans WHERE id = ?`, id)
	return scanFromScanner(row)
}

func (s *SQLiteStore) List(ctx context.Context, limit int) ([]*ScanJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+scanColumns+` FROM scans ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*ScanJob
	for rows.Next() {
		job, err := scanFromScanner(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStore) Update(ctx context.Context, job *ScanJob) error {
	normalizeJobAnalysis(job)
	job.Project = normalizeProjectID(job.Project)
	resultJSON := marshalResult(job)
	_, err := s.db.ExecContext(ctx,
		`UPDATE scans SET ai=?, verify=?, sniper=?, deep=?, project=?, status=?, progress=?, report=?, result=?, error=?, updated_at=? WHERE id=?`,
		boolToInt(job.AI), boolToInt(job.Verify), boolToInt(job.Sniper), boolToInt(job.Deep),
		job.Project, string(job.Status), job.Progress, job.Report, resultJSON, job.Error,
		job.UpdatedAt.Format(time.RFC3339Nano), job.ID,
	)
	return err
}

func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scans WHERE id=?`, id)
	return err
}

// --- Asset pool CRUD ---

func (s *SQLiteStore) UpsertAsset(ctx context.Context, a *PoolAsset) error {
	if a.ID == "" {
		a.ID = generateID()
	}
	if a.ProjectID == "" {
		a.ProjectID = DefaultProjectID
	}
	now := time.Now()
	if a.FirstSeen.IsZero() {
		a.FirstSeen = now
	}
	if a.LastSeen.IsZero() {
		a.LastSeen = now
	}
	// Dedup by target: a repeat sighting refreshes last_seen and fills any
	// fields the new sighting knows about, but never clobbers existing data
	// with blanks (an agent hit shouldn't wipe a scan's service count).
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO assets (id, project_id, target, label, source, status, note, services, loots, last_scan_id, first_seen, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_id, target) DO UPDATE SET
		   label        = CASE WHEN excluded.label != '' THEN excluded.label ELSE assets.label END,
		   status       = CASE WHEN excluded.status != '' THEN excluded.status ELSE assets.status END,
		   note         = CASE WHEN excluded.note != '' THEN excluded.note ELSE assets.note END,
		   services     = CASE WHEN excluded.services > 0 THEN excluded.services ELSE assets.services END,
		   loots        = CASE WHEN excluded.loots > 0 THEN excluded.loots ELSE assets.loots END,
		   last_scan_id = CASE WHEN excluded.last_scan_id != '' THEN excluded.last_scan_id ELSE assets.last_scan_id END,
		   last_seen    = excluded.last_seen`,
		a.ID, a.ProjectID, a.Target, a.Label, a.Source, a.Status, a.Note, a.Services, a.Loots, a.LastScanID,
		a.FirstSeen.Format(time.RFC3339Nano), a.LastSeen.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) ListAssets(ctx context.Context, projectID string, limit int) ([]*PoolAsset, error) {
	if limit <= 0 {
		limit = 200
	}
	if projectID == "" {
		projectID = DefaultProjectID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, target, label, source, status, note, services, loots, last_scan_id, first_seen, last_seen
		 FROM assets WHERE project_id = ? ORDER BY last_seen DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assets []*PoolAsset
	for rows.Next() {
		a, err := assetFromScanner(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (s *SQLiteStore) DeleteAsset(ctx context.Context, projectID, id string) error {
	if projectID == "" {
		projectID = DefaultProjectID
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM assets WHERE id=? AND project_id=?`, id, projectID)
	return err
}

// --- Projects (asset-pool scope) ---

func (s *SQLiteStore) ListProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.created_at,
		        (SELECT COUNT(*) FROM assets a WHERE a.project_id = p.id) AS asset_count
		 FROM projects p ORDER BY p.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []*Project
	for rows.Next() {
		var p Project
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &createdAt, &p.Assets); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		projects = append(projects, &p)
	}
	return projects, rows.Err()
}

func (s *SQLiteStore) CreateProject(ctx context.Context, p *Project) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name = excluded.name`,
		p.ID, p.Name, p.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) ProjectExists(ctx context.Context, id string) (bool, error) {
	id = normalizeProjectID(id)
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? LIMIT 1`, id).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// DeleteProject removes a project and cascades to its asset-pool rows. Both
// deletes run in one transaction so a mid-way failure can't strand assets that
// point at a project id no longer in the list.
func (s *SQLiteStore) DeleteProject(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM assets WHERE project_id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func assetFromScanner(sc scanner) (*PoolAsset, error) {
	var a PoolAsset
	var firstSeen, lastSeen string
	if err := sc.Scan(&a.ID, &a.ProjectID, &a.Target, &a.Label, &a.Source, &a.Status, &a.Note,
		&a.Services, &a.Loots, &a.LastScanID, &firstSeen, &lastSeen); err != nil {
		return nil, err
	}
	a.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen)
	a.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
	return &a, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanFromScanner(sc scanner) (*ScanJob, error) {
	var job ScanJob
	var project, status, resultJSON, createdAt, updatedAt string
	var ai, verify, sniper, deep int
	err := sc.Scan(&job.ID, &job.Target, &job.Mode, &ai, &verify, &sniper, &deep, &project, &status,
		&job.Progress, &job.Report, &resultJSON, &job.Error, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	job.AI = ai != 0
	job.Verify = verify != 0
	job.Sniper = sniper != 0
	job.Deep = deep != 0
	job.Project = normalizeProjectID(project)
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

// --- Chat session CRUD ---

func (s *SQLiteStore) CreateSession(ctx context.Context, session *ChatSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_sessions (id, agent_id, agent_name, title, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.AgentID, session.AgentName, session.Title, session.Status,
		session.CreatedAt.Format(time.RFC3339Nano), session.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func sessionFromScanner(sc scanner) (*ChatSession, error) {
	var cs ChatSession
	var createdAt, updatedAt string
	if err := sc.Scan(&cs.ID, &cs.AgentID, &cs.AgentName, &cs.Title, &cs.Status, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	cs.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	cs.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &cs, nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*ChatSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, agent_name, title, status, created_at, updated_at FROM chat_sessions WHERE id = ?`, id)
	cs, err := sessionFromScanner(row)
	if err != nil {
		return nil, err
	}
	scanIDs, _ := s.SessionScanIDs(ctx, id)
	cs.ScanIDs = scanIDs
	return cs, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, limit int) ([]*ChatSession, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, agent_name, title, status, created_at, updated_at FROM chat_sessions ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*ChatSession
	for rows.Next() {
		cs, err := sessionFromScanner(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, cs)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) UpdateSession(ctx context.Context, session *ChatSession) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE chat_sessions SET title=?, status=?, updated_at=? WHERE id=?`,
		session.Title, session.Status, session.UpdatedAt.Format(time.RFC3339Nano), session.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_sessions WHERE id=?`, id)
	return err
}

// --- Chat message CRUD ---

func (s *SQLiteStore) AddMessage(ctx context.Context, msg *ChatMessage) error {
	metadata := ""
	if msg.Metadata != nil {
		metadata = string(msg.Metadata)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_messages (id, session_id, role, agent_id, agent_name, content, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.SessionID, msg.Role, msg.AgentID, msg.AgentName, msg.Content, metadata,
		msg.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) ListMessages(ctx context.Context, sessionID string, limit int) ([]*ChatMessage, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, agent_id, agent_name, content, metadata, created_at
		 FROM chat_messages WHERE session_id = ? ORDER BY created_at ASC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []*ChatMessage
	for rows.Next() {
		var m ChatMessage
		var metadata, createdAt string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.AgentID, &m.AgentName, &m.Content, &metadata, &createdAt); err != nil {
			return nil, err
		}
		if metadata != "" {
			m.Metadata = json.RawMessage(metadata)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		msgs = append(msgs, &m)
	}
	return msgs, rows.Err()
}

// --- Session-scan association ---

func (s *SQLiteStore) LinkScanToSession(ctx context.Context, sessionID, scanID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO session_scans (session_id, scan_id) VALUES (?, ?)`,
		sessionID, scanID,
	)
	return err
}

func (s *SQLiteStore) SessionScanIDs(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT scan_id FROM session_scans WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- Records ---

const insertRecordSQL = `INSERT OR IGNORE INTO records (id, type, scan_id, session_id, agent_id, source, target, turn, priority, summary, loot, tags, data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func recordArgs(rec *output.Record) []any {
	tagsJSON, _ := json.Marshal(rec.Tags)
	return []any{
		rec.ID, string(rec.Type), rec.ScanID, rec.SessionID, rec.AgentID,
		rec.Source, rec.Target, rec.Turn, rec.Priority, rec.Summary,
		boolToInt(rec.Loot), string(tagsJSON), string(rec.Data),
		rec.Timestamp.Format(time.RFC3339Nano),
	}
}

func (s *SQLiteStore) InsertRecord(ctx context.Context, rec *output.Record) error {
	_, err := s.db.ExecContext(ctx, insertRecordSQL, recordArgs(rec)...)
	return err
}

func (s *SQLiteStore) InsertRecords(ctx context.Context, recs []*output.Record) error {
	if len(recs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, insertRecordSQL)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, rec := range recs {
		if _, err := stmt.ExecContext(ctx, recordArgs(rec)...); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
