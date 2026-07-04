package web

import (
	"context"

	"github.com/chainreactors/aiscan/core/output"
)

type Store interface {
	// Scan CRUD
	Create(ctx context.Context, job *ScanJob) error
	Get(ctx context.Context, id string) (*ScanJob, error)
	List(ctx context.Context, limit int) ([]*ScanJob, error)
	Update(ctx context.Context, job *ScanJob) error
	Delete(ctx context.Context, id string) error

	// Chat sessions
	CreateSession(ctx context.Context, session *ChatSession) error
	GetSession(ctx context.Context, id string) (*ChatSession, error)
	ListSessions(ctx context.Context, limit int) ([]*ChatSession, error)
	UpdateSession(ctx context.Context, session *ChatSession) error
	DeleteSession(ctx context.Context, id string) error

	// Chat messages
	AddMessage(ctx context.Context, msg *ChatMessage) error
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*ChatMessage, error)

	// Session-scan association
	LinkScanToSession(ctx context.Context, sessionID, scanID string) error
	SessionScanIDs(ctx context.Context, sessionID string) ([]string, error)

	// Asset pool (deduplicated by Target within a Project)
	UpsertAsset(ctx context.Context, asset *PoolAsset) error
	ListAssets(ctx context.Context, projectID string, limit int) ([]*PoolAsset, error)
	DeleteAsset(ctx context.Context, projectID, id string) error

	// Projects (asset-pool scope)
	ListProjects(ctx context.Context) ([]*Project, error)
	CreateProject(ctx context.Context, project *Project) error
	ProjectExists(ctx context.Context, id string) (bool, error)
	DeleteProject(ctx context.Context, id string) error // cascades to the project's assets
	// Records
	InsertRecord(ctx context.Context, rec *output.Record) error
	InsertRecords(ctx context.Context, recs []*output.Record) error
}
