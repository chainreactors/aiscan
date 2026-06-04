package scan

import (
	"os"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/output"
	"github.com/chainreactors/parsers"
	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

type recorder struct {
	mu   sync.Mutex
	file *os.File
}

func newRecorder(path string) (*recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &recorder{file: f}, nil
}

func (r *recorder) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Close()
}

func (r *recorder) write(rec output.Record) {
	if r == nil || r.file == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.file.Write(rec.Marshal())
	_, _ = r.file.Write([]byte("\n"))
}

func (r *recorder) ScanStart(targets []string, mode string, flags []string) {
	r.write(output.NewRecord(output.TypeScanStart, output.ScanStart{
		Targets: targets,
		Mode:    mode,
		Flags:   flags,
	}))
}

func (r *recorder) Service(result *parsers.GOGOResult) {
	if result == nil {
		return
	}
	r.write(output.NewRecord(output.TypeService, result))
}

func (r *recorder) Web(result *parsers.SprayResult) {
	if result == nil {
		return
	}
	r.write(output.NewRecord(output.TypeWeb, result))
}

func (r *recorder) Zombie(result *parsers.ZombieResult) {
	if result == nil {
		return
	}
	r.write(output.NewRecord(output.TypeFinding, result))
}

func (r *recorder) Vuln(result *sdktypes.VulnResult) {
	if result == nil {
		return
	}
	r.write(output.NewRecord(output.TypeFinding, result))
}

func (r *recorder) AISkill(skill, target, status, summary, detail string, duration time.Duration) {
	r.write(output.NewRecord(output.TypeAISkill, output.AISkill{
		Skill:    skill,
		Target:   target,
		Status:   status,
		Summary:  summary,
		Detail:   detail,
		Duration: duration.Seconds(),
	}))
}

func (r *recorder) AITurn(skill string, turn int, prompt string, messages []provider.ChatMessage, usage *provider.Usage, duration time.Duration) {
	r.write(output.NewRecord(output.TypeAITurn, output.AITurn{
		Skill:    skill,
		Turn:     turn,
		Prompt:   prompt,
		Messages: messages,
		Usage:    usage,
		Duration: duration.Seconds(),
	}))
}

func (r *recorder) ScanEnd(duration time.Duration, targets, services, webs, findings, aiSkills, errors int) {
	r.write(output.NewRecord(output.TypeScanEnd, output.ScanEnd{
		Duration: duration.Seconds(),
		Targets:  targets,
		Services: services,
		Webs:     webs,
		Findings: findings,
		AISkills: aiSkills,
		Errors:   errors,
	}))
}
