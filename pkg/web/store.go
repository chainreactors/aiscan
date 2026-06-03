package web

import "context"

type Store interface {
	Create(ctx context.Context, job *ScanJob) error
	Get(ctx context.Context, id string) (*ScanJob, error)
	List(ctx context.Context, limit int) ([]*ScanJob, error)
	Update(ctx context.Context, job *ScanJob) error
	Delete(ctx context.Context, id string) error
}
