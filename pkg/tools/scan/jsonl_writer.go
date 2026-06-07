package scan

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/eventbus"
	"github.com/chainreactors/aiscan/pkg/output"
	"github.com/chainreactors/aiscan/pkg/tools/scan/pipeline"
	"github.com/chainreactors/parsers"
	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

type scanJSONLWriter struct {
	mu         sync.Mutex
	file       *os.File
	scanUnsub  func()
	agentUnsub func()
}

func newScanJSONLWriter(path string, scanBus *eventbus.Bus[pipeline.Observation], agentBus *eventbus.Bus[agent.Event]) (*scanJSONLWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &scanJSONLWriter{file: f}
	w.scanUnsub = scanBus.Subscribe(w.handleObservation)
	if agentBus != nil {
		w.agentUnsub = agentBus.Subscribe(w.handleAgentEvent)
	}
	return w, nil
}

func (w *scanJSONLWriter) Close() error {
	if w.scanUnsub != nil {
		w.scanUnsub()
		w.scanUnsub = nil
	}
	if w.agentUnsub != nil {
		w.agentUnsub()
		w.agentUnsub = nil
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

func (w *scanJSONLWriter) handleAgentEvent(event agent.Event) {
	w.writeJSON(agent.SerializableEvent(event))
}

func (w *scanJSONLWriter) writeJSON(v any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	line, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.file.Write(line)
	_, _ = w.file.Write([]byte("\n"))
}

func observationToRecords(e event) []output.Record {
	switch e.Kind {
	case eventTarget:
		return targetToRecords(e)
	case eventLoot:
		return lootToRecords(e)
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

func lootToRecords(e event) []output.Record {
	if e.Loot == nil {
		return nil
	}
	return []output.Record{output.NewRecord(output.TypeLoot, e.Loot)}
}

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

type ServiceResult = parsers.GOGOResult
type SprayResult = parsers.SprayResult
type ZombieResult = parsers.ZombieResult
type VulnResult = sdktypes.VulnResult
