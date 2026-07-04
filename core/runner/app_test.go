package runner

import (
	"context"
	"testing"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func TestAppCloseCancelsAndWaitsForAsyncScannerInit(t *testing.T) {
	prev := ScannerInitFunc
	t.Cleanup(func() { ScannerInitFunc = prev })

	started := make(chan struct{})
	done := make(chan struct{})
	ScannerInitFunc = func(ctx context.Context, _ *App, _ cfg.RuntimeConfig, _ telemetry.Logger) {
		close(started)
		<-ctx.Done()
		close(done)
	}

	app, err := NewApp(context.Background(), cfg.RuntimeConfig{Logger: telemetry.NopLogger()})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scanner init did not start")
	}

	app.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not wait for scanner init to exit")
	}

	app.Close()
}
