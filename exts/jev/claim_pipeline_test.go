package jev

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/internal/jevwire"
	"google.golang.org/protobuf/encoding/protojson"
)

// These tests start with no Claim, candidate or Reflex. Only inference is
// scripted in the local variant; the Agent, command executor, native contracts,
// compiler Agent, replay, semantic review, publication and reload are real.
func TestClaimToReflexPipeline(t *testing.T) {
	for _, claim := range []Claim{
		{Type: jevapi.ClaimNoul, Context: "Inspect the currently requested target through claimlab inspect and report its actual current receipt."},
		{Type: jevapi.ClaimChoice, Context: "For a requested current target, choose inspect to obtain its native receipt or defer when the target is absent.", Options: []string{"inspect", "defer"}},
		{Type: jevapi.ClaimScore, Context: "Assess evidence for the requested target's current native receipt, from unavailable to inspected to complete.", Options: []string{"unavailable", "inspected", "complete"}},
	} {
		t.Run(claim.Type.String(), func(t *testing.T) {
			client := fakeJEV(t, pipelineDecisions)
			runClaimPipeline(t, "scripted", client, nil, claim)
		})
	}
}

func TestClaimPipelineLiteralEncoding(t *testing.T) {
	for _, target := range []string{"冷启动 'quoted' \\ target", "", "owner's \"quoted\" target", "literal $(command) `code` $TARGET ; & value"} {
		command := coretool.JoinCommandLine("claimlab", []string{"inspect", target})
		decoded := literalCommand(command)
		if !slices.Equal([]string{"claimlab", "inspect", target}, decoded) {
			t.Fatalf("literal target changed in replay: command=%q replay=%q", command, decoded)
		}
	}
	for _, command := range []string{"claimlab inspect $TARGET", "claimlab inspect $(other)", "claimlab inspect current; other", "claimlab inspect *", "claimlab inspect current &"} {
		if literalCommand(command) != nil {
			t.Fatalf("nonliteral command accepted for replay: %q", command)
		}
	}
}

func TestClaimPipelineReviewReceivesResolvedReport(t *testing.T) {
	var reviewed atomic.Bool
	e, plan, actor := compilerReadPlan(t, func(req jevwire.Request) map[string]jevwire.Answer {
		if strings.Contains(string(req.State), `"reflex":`) {
			reviewed.Store(true)
			if !strings.Contains(string(req.State), `\"report\":\"current-native-receipt\"`) && !strings.Contains(string(req.State), `"report":"current-native-receipt"`) {
				t.Error("semantic review did not receive the actual resolved native receipt")
			}
			if strings.Contains(string(req.State), `"report":{"evidence":`) {
				t.Error("semantic review retained an unresolved report pointer")
			}
		}
		return declarationAnswers(req, true)
	})
	plan.job.cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(compilerTool("validate_reflex", struct {
			Artifact json.RawMessage `json:"artifact"`
		}{json.RawMessage(jsonText(compilerReadArtifact(actor)))})), nil
	})
	r, err := e.generateReflex(t.Context(), plan)
	if err != nil || r == nil || !reviewed.Load() {
		t.Fatalf("resolved evidence review failed: artifact=%v reviewed=%v err=%v", r != nil, reviewed.Load(), err)
	}
}

// JEV_PIPELINE_LIVE=1 uses real JEV for every decision with scripted model
// inference. JEV_PIPELINE_LLM_LIVE=1 additionally uses the configured real LLM.
// Neither variant seeds an executable source or bypasses qualification.
func TestLiveClaimToReflexPipeline(t *testing.T) {
	if os.Getenv("JEV_PIPELINE_LIVE") != "1" {
		t.Skip("set JEV_PIPELINE_LIVE=1 and TYPESAFE_API_KEY")
	}
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Fatal("TYPESAFE_API_KEY is required")
	}
	client := jevapi.New(os.Getenv("TYPESAFE_API_KEY"), "", 20*time.Second)
	t.Cleanup(client.Close)
	mode := "real-jev"
	var llm provider.Provider
	if os.Getenv("JEV_PIPELINE_LLM_LIVE") == "1" {
		for _, key := range []string{"CYBER_API_KEY", "CYBER_BASE_URL", "CYBER_MODEL"} {
			if os.Getenv(key) == "" {
				t.Fatalf("%s is required", key)
			}
		}
		var err error
		llm, err = provider.NewProvider(&provider.ProviderConfig{Provider: os.Getenv("CYBER_PROVIDER"), APIKey: os.Getenv("CYBER_API_KEY"), BaseURL: os.Getenv("CYBER_BASE_URL"), Model: os.Getenv("CYBER_MODEL"), Timeout: 60})
		if err != nil {
			t.Fatal(err)
		}
		mode = "real-jev-and-llm"
	}
	runClaimPipeline(t, mode, client, llm, Claim{Type: jevapi.ClaimNoul, Context: "Inspect the currently requested target through claimlab inspect and report its actual current receipt."})
}

func pipelineDecisions(req jevwire.Request) map[string]jevwire.Answer {
	if runtimeRequest(req) {
		return runtimeAnswers(req, "run")
	}
	out := declarationAnswers(req, true)
	for id := range req.Questions {
		if id == "input" || id == "binding" || id == "completion" {
			out[id] = answer("accept")
		}
	}
	// A planned or in-flight read cannot ground compilation. The review request
	// has a Reflex; the trigger request must contain a completed native result.
	if _, ok := req.Questions["compile"]; ok && !strings.Contains(string(req.State), `"reflex":`) && !strings.Contains(string(req.State), `"call_id":`) {
		out["compile"] = answer(Defer)
	}
	return out
}

type pipelineResult struct {
	Target  string `json:"target"`
	Receipt string `json:"receipt"`
}

type pipelineCounts struct {
	Ordinary    int `json:"ordinary"`
	Claims      int `json:"claims"`
	Compilation int `json:"compilation"`
	Parameters  int `json:"parameters"`
	Composition int `json:"composition"`
}

type pipelineReport struct {
	Mode        string           `json:"mode"`
	Stage       string           `json:"stage"`
	Passed      bool             `json:"passed"`
	Seeded      bool             `json:"seeded"`
	Error       string           `json:"error,omitempty"`
	ColdModel   pipelineCounts   `json:"cold_model"`
	WarmModel   pipelineCounts   `json:"warm_model"`
	NativeReads []pipelineResult `json:"native_reads"`
	ClaimIDs    []string         `json:"claim_ids"`
	ReflexIDs   []string         `json:"reflex_ids"`
	ElapsedMS   int64            `json:"elapsed_ms"`
	ProofChecks []string         `json:"proof_checks"`
	LibraryHash string           `json:"library_hash,omitempty"`
}

type pipelineFixture struct {
	mu       sync.Mutex
	reads    []pipelineResult
	counts   pipelineCounts
	reusing  bool
	repaired bool
}

func (f *pipelineFixture) snapshot() (pipelineCounts, []pipelineResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts, slices.Clone(f.reads)
}

func (f *pipelineFixture) contribution() extension.Extension {
	return extension.Func{LoadFunc: func(scope *extension.Scope) error {
		cmd := coretool.Command{Name: "claimlab", Usage: "claimlab inspect <target>\nRead the current receipt for a literal target. Returns JSON {target:string,receipt:string}. Effect-free; each actual read has a fresh random receipt.", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			if len(ex.Args) != 2 || ex.Args[0] != "inspect" {
				return nil, fmt.Errorf("expected inspect <target>")
			}
			var random [12]byte
			if _, err := rand.Read(random[:]); err != nil {
				return nil, err
			}
			row := pipelineResult{Target: ex.Args[1], Receipt: "receipt-" + hex.EncodeToString(random[:])}
			f.mu.Lock()
			f.reads = append(f.reads, row)
			f.mu.Unlock()
			fmt.Fprint(ex.Stdout, jsonText(row))
			return nil, nil
		}}
		if err := extension.Add(scope, cmd); err != nil {
			return err
		}
		return extension.Add(scope, coretool.NativeContract{ID: "claimlab", Version: "1", Description: cmd.Usage, Classify: func(call coretool.NativeCall) (coretool.NativeAccess, error) {
			if call.Name == "bash" && len(call.Argv) == 3 && call.Argv[0] == "claimlab" && call.Argv[1] == "inspect" {
				return coretool.NativeRead, nil
			}
			return coretool.NativeUnsupported, nil
		}})
	}}
}

func pipelineArtifact(target string) string {
	return jsonText(struct {
		Version   int                       `json:"api_version"`
		Steps     map[string]StepDefinition `json:"steps"`
		Schema    json.RawMessage           `json:"parameters_schema"`
		Arguments map[string]string         `json:"arguments"`
		Observe   string                    `json:"observe"`
	}{2, map[string]StepDefinition{}, json.RawMessage(`{"type":"object","required":["target"],"properties":{"target":{"type":"string"}},"additionalProperties":false}`), map[string]string{"target": target}, `js:function(context,args){if(!args||!args.target)return{defer:"missing current arguments",parameters:"target"};const seen=context.history.filter(function(r){return r.name==="bash"&&r.arguments&&typeof r.arguments.command==="string"&&r.arguments.command.indexOf("claimlab inspect ")===0&&!r.is_error&&r.data&&r.data.target===args.target&&typeof r.data.receipt==="string";});const r=seen.length?seen[seen.length-1]:execute({name:"bash",arguments:{command:command("claimlab",["inspect",args.target])},read:true});return{report:{evidence:r.call_id,path:["data","receipt"]}};}`})
}

func pipelineTask(target string) string {
	return "Read the current native receipt for the literal target below using claimlab inspect. Report only the exact receipt from its actual result.\nTarget: " + jsonText(target)
}

func pipelineTarget(req *provider.ChatCompletionRequest) (string, error) {
	texts := []string{}
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}
		text := provider.MessageText(msg)
		var envelope struct {
			Context struct {
				Messages []struct{ Role, Text string } `json:"messages"`
			} `json:"context"`
		}
		if json.Unmarshal([]byte(text), &envelope) == nil && len(envelope.Context.Messages) > 0 {
			for _, row := range envelope.Context.Messages {
				if row.Role == "user" {
					texts = append(texts, row.Text)
				}
			}
		} else {
			texts = append(texts, text)
		}
	}
	for _, text := range slices.Backward(texts) {
		if _, value, ok := strings.Cut(text, "\nTarget: "); ok {
			var target string
			if err := json.Unmarshal([]byte(value), &target); err == nil && target != "" {
				return target, nil
			}
		}
	}
	return "", fmt.Errorf("current target absent from model context")
}

func (f *pipelineFixture) inference(real provider.Provider, claim Claim, coldTarget string) provider.Provider {
	return testProvider(func(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		f.mu.Lock()
		kind := req.Purpose
		if len(req.Messages) > 0 && provider.MessageText(req.Messages[0]) == claimPrompt {
			kind = "claims"
		}
		switch kind {
		case "claims":
			f.counts.Claims++
		case "compilation":
			f.counts.Compilation++
		case "parameters":
			f.counts.Parameters++
		case "composition":
			f.counts.Composition++
		default:
			f.counts.Ordinary++
		}
		counts, reusing, reads := f.counts, f.reusing, slices.Clone(f.reads)
		f.mu.Unlock()
		if kind == "composition" && len(req.Tools) != 0 {
			return nil, fmt.Errorf("report composition retained executable tools")
		}
		if real != nil {
			return real.ChatCompletion(ctx, req)
		}
		switch kind {
		case "claims":
			return reply(provider.TextMessage("assistant", jsonText(struct {
				Claims []Claim `json:"claims"`
			}{[]Claim{claim}}))), nil
		case "compilation":
			// First submit a mismatched example. The real compiler must return
			// a replay diagnostic to this same model before accepting a repair.
			if counts.Compilation > 4 {
				return nil, fmt.Errorf("scripted compiler did not converge after repair")
			}
			target := coldTarget
			if counts.Compilation == 1 {
				target += "-wrong-example"
			} else {
				for _, msg := range req.Messages {
					if result := provider.MessageToolResult(msg); result != nil && strings.Contains(coretool.ResultText(result), "native_call_mismatch") {
						f.mu.Lock()
						f.repaired = true
						f.mu.Unlock()
					}
				}
			}
			return reply(compilerTool("validate_reflex", struct {
				Artifact json.RawMessage `json:"artifact"`
			}{json.RawMessage(pipelineArtifact(target))})), nil
		case "parameters":
			target, err := pipelineTarget(req)
			if err != nil {
				return nil, err
			}
			return reply(provider.TextMessage("assistant", jsonText(map[string]string{"target": target}))), nil
		default:
			for _, row := range slices.Backward(reads) {
				for _, msg := range req.Messages {
					text := provider.MessageText(msg)
					if result := provider.MessageToolResult(msg); result != nil {
						text = coretool.ResultText(result)
					}
					if strings.Contains(text, row.Receipt) {
						return reply(provider.TextMessage("assistant", row.Receipt)), nil
					}
				}
			}
			if reusing || kind == "composition" {
				return nil, fmt.Errorf("warm task requested planning or composition without current native evidence")
			}
			target, err := pipelineTarget(req)
			if err != nil {
				return nil, err
			}
			return reply(compilerTool("bash", map[string]string{"command": coretool.JoinCommandLine("claimlab", []string{"inspect", target})})), nil
		}
	})
}

func runClaimPipeline(t *testing.T, mode string, client *jevapi.Client, real provider.Provider, claim Claim) {
	t.Helper()
	started := time.Now()
	f := &pipelineFixture{}
	dir := t.TempDir()
	report := pipelineReport{Mode: mode, Stage: "cold"}
	if root := os.Getenv("JEV_PIPELINE_REPORT_DIR"); root != "" {
		dir = filepath.Join(root, mode, claim.Type.String())
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "library.json")); !os.IsNotExist(err) {
			t.Fatal("report directory must not contain a preexisting library")
		}
		defer func() {
			report.Passed, report.ElapsedMS = !t.Failed(), time.Since(started).Milliseconds()
			if t.Failed() && report.Error == "" {
				report.Error = "acceptance check failed at " + report.Stage
			}
			counts, reads := f.snapshot()
			report.NativeReads = reads
			if report.Stage == "cold" {
				report.ColdModel = counts
			} else if report.Stage == "reuse" {
				report.WarmModel = counts
			}
			data, err := json.MarshalIndent(report, "", "  ")
			if err == nil {
				err = os.WriteFile(filepath.Join(dir, "pipeline-report.json"), data, 0600)
			}
			if err != nil {
				t.Error(err)
			}
		}()
	}
	e, cfg, _ := testInstallationWithExtensions(t, Config{Mode: "auto", Directory: dir, CompilationTimeout: "90s"}, client, f.contribution())
	observePipeline(t, e)
	if lib := e.snapshot(); len(lib.Claims)+len(lib.Reflexes)+len(lib.Candidates) != 0 {
		t.Fatal("cold installation is not empty")
	}
	coldTarget := "冷启动 'quoted' \\ target"
	cfg.Provider = f.inference(real, claim, coldTarget)
	if real != nil {
		cfg.Model = os.Getenv("CYBER_MODEL")
	}
	cfg.SystemPrompt += "\n" + Prompt + "\n" + "claimlab inspect <target> is an effect-free native command returning JSON {target,receipt}. Invoke it through bash with its documented command string and quote the literal target exactly."
	cfg.SessionID = "claim-pipeline-cold"
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	result, err := agent.NewAgent(cfg).Run(ctx, agent.TextInput(pipelineTask(coldTarget)))
	if err != nil {
		report.Error = err.Error()
		t.Fatal(err)
	}
	if err := e.WaitIdle(ctx); err != nil {
		report.Error = err.Error()
		t.Fatal(err)
	}
	report.ColdModel, report.NativeReads = f.snapshot()
	if len(report.NativeReads) != 1 || report.NativeReads[0].Target != coldTarget || result == nil || !strings.Contains(result.Output, report.NativeReads[0].Receipt) {
		t.Fatalf("cold execution/compiler isolation failed: reads=%+v", report.NativeReads)
	}
	report.Stage = "publication"
	lib := e.snapshot()
	if len(lib.Claims) == 0 || len(lib.Reflexes) != 1 || len(lib.Candidates) != 0 || report.ColdModel.Claims == 0 || report.ColdModel.Compilation == 0 {
		t.Fatalf("first task failed to publish learned source: counts=%+v library=%s", report.ColdModel, jsonText(lib))
	}
	if real == nil {
		f.mu.Lock()
		repaired := f.repaired
		f.mu.Unlock()
		if !repaired || len(lib.Claims) != 1 {
			t.Fatal("mismatched source was not repaired through the ordinary compiler")
		}
	}
	for id, c := range lib.Claims {
		if c.Task == "" || id != "c"+digest(c.Claim)[:16] || c.Validate() != nil {
			t.Fatal("Claim lost content identity or task provenance")
		}
		if real == nil && !reflect.DeepEqual(c.Claim, claim) {
			t.Fatal("typed Claim changed during publication")
		}
		report.ClaimIDs = append(report.ClaimIDs, id)
	}
	for id, r := range lib.Reflexes {
		if !e.qualified(r) || r.Proof.Replayed != 1 || len(r.Claims) == 0 {
			t.Fatalf("published source lacks complete qualification or leaked examples: %s", jsonText(r))
		}
		for _, cid := range r.Claims {
			if _, ok := lib.Claims[cid]; !ok {
				t.Fatal("Reflex has dangling Claim membership")
			}
		}
		report.ReflexIDs = append(report.ReflexIDs, id)
		report.ProofChecks = slices.Clone(r.Proof.Checks)
	}
	libraryPath := filepath.Join(dir, "library.json")
	before, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), report.NativeReads[0].Receipt) || strings.Contains(string(before), coldTarget) || strings.Contains(string(before), jsonText(coldTarget)) || strings.Contains(string(before), `"arguments":`) {
		t.Fatal("persistent library retained a task example or old answer")
	}
	report.LibraryHash = digest(lib)
	report.Stage = "reload"
	loaded, warm, _ := testInstallationWithExtensions(t, Config{Mode: "auto", Learning: "frozen", Directory: dir}, client, f.contribution())
	observePipeline(t, loaded)
	if digest(loaded.snapshot()) != report.LibraryHash {
		t.Fatal("reload changed published Claim/Reflex content")
	}
	f.mu.Lock()
	f.reusing = true
	f.counts = pipelineCounts{}
	f.mu.Unlock()
	warm.Provider, warm.Model, warm.SystemPrompt = cfg.Provider, cfg.Model, cfg.SystemPrompt
	report.Stage = "reuse"
	for i, target := range []string{"新目标 with spaces", "owner's \"quoted\" target", coldTarget} {
		warm.SessionID = fmt.Sprintf("claim-pipeline-warm-%d", i)
		result, err := agent.NewAgent(warm).Run(ctx, agent.TextInput(pipelineTask(target)))
		if err != nil {
			report.Error = err.Error()
			t.Fatal(err)
		}
		if err := loaded.WaitIdle(ctx); err != nil {
			t.Fatal(err)
		}
		report.WarmModel, report.NativeReads = f.snapshot()
		if len(report.NativeReads) != i+2 || report.NativeReads[i+1].Target != target || result == nil || !strings.Contains(result.Output, report.NativeReads[i+1].Receipt) {
			t.Fatalf("warm target/current result failed: iteration=%d reads=%+v", i, report.NativeReads)
		}
		if strings.Contains(result.Output, report.NativeReads[0].Receipt) {
			t.Fatal("warm answer reused the cold receipt")
		}
		if report.WarmModel.Ordinary != 0 || report.WarmModel.Claims != 0 || report.WarmModel.Compilation != 0 || report.WarmModel.Parameters != i+1 || report.WarmModel.Composition != i+1 {
			t.Fatalf("warm execution replanned, relearned or repeated argument extraction: %+v", report.WarmModel)
		}
	}
	after, err := os.ReadFile(libraryPath)
	if err != nil || string(after) != string(before) || digest(loaded.snapshot()) != report.LibraryHash {
		t.Fatal("frozen reuse mutated durable library", err)
	}
	report.Stage = "complete"
	t.Logf("%s: empty library -> typed Claim -> replay repair -> qualified publication -> reload -> 3 fresh parameterized tasks; cold=%+v warm=%+v native_reads=%d checks=%v", mode, report.ColdModel, report.WarmModel, len(report.NativeReads), report.ProofChecks)
}

func observePipeline(t *testing.T, e *Extension) {
	t.Helper()
	if os.Getenv("JEV_PIPELINE_REPORT_DIR") == "" {
		return
	}
	sub := e.stream.Observe(func(event *aop.Event) {
		packed := event.GetExtension()
		if packed == nil || !packed.MessageIs(&RuntimeEvent{}) {
			return
		}
		var runtime RuntimeEvent
		if err := packed.UnmarshalTo(&runtime); err != nil {
			t.Error(err)
			return
		}
		data, err := protojson.Marshal(&runtime)
		if err == nil {
			err = e.log(filepath.Join(e.config.Directory, "events.jsonl"), json.RawMessage(data))
		}
		if err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() {
		if err := sub.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
}
