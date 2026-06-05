package scan

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/chainreactors/aiscan/pkg/eventbus"
	"github.com/chainreactors/aiscan/pkg/output"
	"github.com/chainreactors/aiscan/pkg/tools/scan/pipeline"
	"github.com/chainreactors/parsers"
	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

type scanJSONLWriter struct {
	mu   sync.Mutex
	file *os.File
	unsub func()
}

func newScanJSONLWriter(path string, bus *eventbus.Bus[pipeline.Observation]) (*scanJSONLWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &scanJSONLWriter{file: f}
	w.unsub = bus.Subscribe(w.handleObservation)
	return w, nil
}

func (w *scanJSONLWriter) Close() error {
	if w.unsub != nil {
		w.unsub()
		w.unsub = nil
	}
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *scanJSONLWriter) WriteRecord(rec output.Record) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = w.file.Write(line)
	_, _ = w.file.Write([]byte("\n"))
}

func (w *scanJSONLWriter) handleObservation(obs pipeline.Observation) {
	if obs.Action != pipeline.ActionAccept {
		return
	}
	e, ok := obs.Event.(event)
	if !ok {
		return
	}
	for _, rec := range observationToRecords(e) {
		w.WriteRecord(rec)
	}
}

func observationToRecords(e event) []output.Record {
	switch e.Kind {
	case eventTarget:
		return targetToRecords(e)
	case eventFinding:
		return findingToRecords(e)
	default:
		return nil
	}
}

func targetToRecords(e event) []output.Record {
	switch target := e.Target.(type) {
	case serviceTarget:
		if target.Result != nil {
			return []output.Record{output.NewRecord(output.TypeService, target.Result)}
		}
	case webProbeTarget:
		if reportableSprayResultForCapability(target.Result, target.Capability) && target.Result != nil {
			return []output.Record{output.NewRecord(output.TypeWeb, target.Result)}
		}
	}
	return nil
}

func findingToRecords(e event) []output.Record {
	switch finding := e.Finding.(type) {
	case weakpassFinding:
		if finding.Result != nil {
			return []output.Record{output.NewRecord(output.TypeFinding, finding.Result)}
		}
	case vulnFinding:
		if finding.String() != "" {
			return []output.Record{output.NewRecord(output.TypeFinding, finding.Result)}
		}
	case aiSkillFinding:
		if finding.Summary != "" || finding.Detail != "" {
			return []output.Record{output.NewRecord(output.TypeAISkill, output.AISkill{
				Skill:   finding.Skill,
				Target:  finding.Target,
				Status:  finding.Status,
				Summary: finding.Summary,
				Detail:  finding.Detail,
			})}
		}
	}
	return nil
}

// observationToRecord converts a pipeline.Observation to an output.Record for external consumers.
// This is used by the unified JSONL writer in core/runner when subscribing to the pipeline bus.
func ObservationToRecord(obs pipeline.Observation) *output.Record {
	if obs.Action != pipeline.ActionAccept {
		return nil
	}
	e, ok := obs.Event.(event)
	if !ok {
		return nil
	}
	records := observationToRecords(e)
	if len(records) == 0 {
		return nil
	}
	return &records[0]
}

// targetTypes for external type assertions on pipeline events
type ServiceResult = parsers.GOGOResult
type SprayResult = parsers.SprayResult
type ZombieResult = parsers.ZombieResult
type VulnResult = sdktypes.VulnResult
