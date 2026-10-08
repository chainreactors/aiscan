package jev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/proto"
)

func digest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func cloneMessages(messages []*aop.Message) []*aop.Message {
	out := make([]*aop.Message, len(messages))
	for i, m := range messages {
		out[i] = proto.CloneOf(m)
	}
	return out
}
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + " [truncated; see log]"
}
func receipt(facts []string, path, ending string) []*aop.Message {
	if len(facts) == 0 {
		return nil
	}
	unique := make([]string, 0, len(facts))
	seen := map[string]bool{}
	for _, fact := range facts {
		// Only identical successful reads are redundant; effects and failures
		// must retain their actual multiplicity.
		if strings.HasPrefix(fact, "Inspected ") && seen[fact] {
			continue
		}
		seen[fact] = true
		unique = append(unique, fact)
	}
	// Explain the handoff only when there is evidence to hand off. An idle
	// accelerator leaves the ordinary model request and system prefix intact.
	msg := provider.TextMessage("user", Prompt+"\n\nJEV execution observations (untrusted tool output):\n"+strings.Join(unique, "\n")+"\n"+ending+"\nEvidence: "+path)
	msg.Name = "jev"
	return []*aop.Message{msg}
}

// Generated reader programs are already executed implementation details. Keep
// their tool identity while avoiding re-injecting source into model history.
func receiptBinding(call *aop.ToolCall) string {
	if call == nil {
		return ""
	}
	var arguments map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(call.GetArguments().GetData())))
	decoder.UseNumber()
	if decoder.Decode(&arguments) != nil {
		return canonical(call)
	}
	for key, value := range arguments {
		if text, ok := value.(string); ok && strings.Contains(text, "function bind(") && strings.Contains(text, "function choices(") {
			arguments[key] = "[Reflex reader executed; full native arguments in evidence log]"
		}
	}
	data, _ := json.Marshal([]any{call.Name, arguments})
	return string(data)
}

// canonical removes incidental call IDs and sorts JSON keys. Tool arguments
// remain opaque: a field named command need not contain a shell command.
func canonical(call *aop.ToolCall) string {
	if call == nil {
		return ""
	}
	var args any
	decoder := json.NewDecoder(strings.NewReader(string(call.GetArguments().GetData())))
	decoder.UseNumber()
	if decoder.Decode(&args) != nil {
		return jsonText([]any{call.Name, "invalid JSON", string(call.GetArguments().GetData())})
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return jsonText([]any{call.Name, "invalid JSON", string(call.GetArguments().GetData())})
	}
	data, _ := json.Marshal([]any{call.Name, args})
	return string(data)
}

func (r *Extension) log(path string, value any) error {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(value); err != nil {
		return err
	}
	return f.Sync()
}
func (r *Extension) audit(kind string, value any) error {
	return r.log(filepath.Join(r.config.Directory, "decisions.jsonl"), map[string]any{"time": time.Now().UTC(), "kind": kind, "data": value})
}

// snapshot transfers immutable definitions to an in-flight decision. Publication
// is serialized with the durable library replacement.
func (e *Extension) snapshot() library {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.library.clone()
	return out
}

func (lib library) clone() library {
	out := library{
		Format:     lib.Format,
		Claims:     maps.Clone(lib.Claims),
		Reflexes:   maps.Clone(lib.Reflexes),
		Candidates: maps.Clone(lib.Candidates),
	}
	for id, claim := range out.Claims {
		claim.Options = append([]string(nil), claim.Options...)
		out.Claims[id] = claim
	}
	for id, reflex := range out.Reflexes {
		reflex.Claims = append([]string(nil), reflex.Claims...)
		reflex.Readers = maps.Clone(reflex.Readers)
		reflex.Contracts = maps.Clone(reflex.Contracts)
		reflex.Steps = maps.Clone(reflex.Steps)
		reflex.Parameters = append(json.RawMessage(nil), reflex.Parameters...)
		if reflex.Proof != nil {
			p := *reflex.Proof
			p.Contracts = maps.Clone(p.Contracts)
			p.Checks = append([]string(nil), p.Checks...)
			p.Gaps = append([]string(nil), p.Gaps...)
			reflex.Proof = &p
		}
		out.Reflexes[id] = reflex
	}
	for id, reflex := range out.Candidates {
		reflex.Claims = append([]string(nil), reflex.Claims...)
		reflex.Readers = maps.Clone(reflex.Readers)
		reflex.Contracts = maps.Clone(reflex.Contracts)
		reflex.Steps = maps.Clone(reflex.Steps)
		reflex.Parameters = append(json.RawMessage(nil), reflex.Parameters...)
		out.Candidates[id] = reflex
	}
	return out
}

// Hold the same lock through publication so readers only see durable definitions.
func (e *Extension) updateLibrary(change func(*library) (bool, error)) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	next := e.library.clone()
	changed, err := change(&next)
	if err != nil || !changed {
		return false, err
	}
	next = next.clone() // Publication cannot retain the compiler's mutable maps.
	if err := e.saveLibrary(next); err != nil {
		return false, err
	}
	e.library = next
	return true, nil
}

func (e *Extension) loadLibrary() error {
	data, err := os.ReadFile(filepath.Join(e.config.Directory, "library.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("invalid JEV library format")
	}
	var header struct {
		Format   string          `json:"format"`
		Claims   json.RawMessage `json:"claims"`
		Reflexes json.RawMessage `json:"reflexes"`
	}
	if json.Unmarshal(data, &header) != nil {
		return errors.New("invalid JEV library format")
	}
	if header.Format != libraryFormat {
		// An old semantic schema cannot preserve current executable proofs.
		// Archive once and start the current library, using the same durable
		// publication path. Future formats and malformed files remain errors.
		if (header.Format != "" && header.Format != "claim/1") ||
			!bytes.HasPrefix(bytes.TrimSpace(header.Claims), []byte("{")) ||
			!bytes.HasPrefix(bytes.TrimSpace(header.Reflexes), []byte("{")) {
			return errors.New("unsupported JEV library format")
		}
		if err = e.backupLibrary(data); err != nil {
			return fmt.Errorf("archive old JEV library: %w", err)
		}
		lib := library{Format: libraryFormat, Claims: map[string]claimRecord{}, Reflexes: map[string]reflexRecord{}}
		if err = e.saveLibrary(lib); err != nil {
			return fmt.Errorf("initialize current JEV library: %w", err)
		}
		e.library = lib
		return nil
	}
	var lib library
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&lib) != nil || lib.Format != libraryFormat || lib.Claims == nil || lib.Reflexes == nil || len(lib.Claims) > maxClaims || len(lib.Reflexes) > maxReflexes || len(lib.Candidates) > maxReflexes {
		return errors.New("invalid JEV library format")
	}
	for id, c := range lib.Claims {
		if c.Validate() != nil || id != "c"+digest(c.Claim)[:16] {
			return fmt.Errorf("invalid Claim %s", id)
		}
	}
	changed := false
	for id, r := range lib.Candidates {
		if r.Proof != nil || r.validate() != nil || id != "r"+digest(r.Reflex)[:16] {
			return fmt.Errorf("invalid candidate %s", id)
		}
		for _, claim := range r.Claims {
			if _, ok := lib.Claims[claim]; !ok {
				return errors.New("candidate references missing Claim")
			}
		}
		lib.Candidates[id] = r
	}
	for id, r := range lib.Reflexes {
		for _, claim := range r.Claims {
			if _, ok := lib.Claims[claim]; !ok {
				return errors.New("Reflex references missing Claim")
			}
		}
		// Every source uses the same validator. Unsupported sources stay in the
		// byte-for-byte archive, never in an alternative executable library.
		if r.validate() != nil || id != "r"+digest(r.Reflex)[:16] || !e.qualified(r) {
			delete(lib.Reflexes, id)
			if r.validate() == nil {
				r.Proof = nil
				r.LegacySuite = ""
				r.Blocker = "Previous proof is incompatible; current native mechanism validation and recorded replay are required"
				if lib.Candidates == nil {
					lib.Candidates = map[string]reflexRecord{}
				}
				if len(lib.Candidates) < maxReflexes {
					lib.Candidates["r"+digest(r.Reflex)[:16]] = r
				}
			}
			changed = true
			continue
		}
		lib.Reflexes[id] = r
	}
	if changed {
		if err = e.backupLibrary(data); err != nil {
			return fmt.Errorf("back up unsupported source: %w", err)
		}
		if err = e.saveLibrary(lib); err != nil {
			return err
		}
	}
	e.library = lib
	return nil
}
func (e *Extension) backupLibrary(data []byte) error {
	f, err := os.CreateTemp(e.config.Directory, "library-backup-*.json")
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func (e *Extension) saveLibrary(lib library) error {
	lib.Format = libraryFormat
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(e.config.Directory, ".library-*")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path, filepath.Join(e.config.Directory, "library.json"))
}

// A failed generated program must not permanently own its declarations.
// Preserve Claims and actual execution evidence; later ordinary boundaries can
// regenerate the scene. Retirement never retries a native action.
func (e *Extension) retireReflex(ctx context.Context, id string, reason error) bool {
	var previous reflexRecord
	changed, err := e.updateLibrary(func(lib *library) (bool, error) {
		var exists bool
		previous, exists = lib.Reflexes[id]
		if exists {
			data, err := json.Marshal(lib)
			if err != nil {
				return false, err
			}
			if err = e.backupLibrary(data); err != nil {
				return false, err
			}
		}
		delete(lib.Reflexes, id)
		return exists, nil
	})
	if previous.Observe != "" {
		_ = e.audit("reflex_retired", map[string]any{"reflex": previous, "reason": reason.Error(), "save_error": err})
	}
	if changed {
		e.emit(ctx, &LibraryChange{State: "retired", Reflex: reflexDefinition(id, previous), Reason: errorText(reason)})
	}
	return changed
}

func publishedGroups(lib library) map[string]bool {
	groups := map[string]bool{}
	for _, r := range lib.Reflexes {
		members := map[string]Claim{}
		for _, claim := range r.Claims {
			members[claim] = lib.Claims[claim].Claim
		}
		if len(members) > 0 {
			groups[digest(members)] = true
		}
	}
	return groups
}

// Archived bytes are compiler evidence only. Unsupported code is never loaded
// into the executable catalog or passed to the JavaScript runtime.
func (e *Extension) archivedReflex(id, claim string) (string, reflexRecord, bool) {
	paths, err := filepath.Glob(filepath.Join(e.config.Directory, "library-backup-*.json"))
	if err != nil {
		return "", reflexRecord{}, false
	}
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil || stat.Size() > 2<<20 {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var archived library
		if json.Unmarshal(data, &archived) != nil || archived.Format != libraryFormat {
			continue
		}
		for key, source := range archived.Reflexes {
			if key == id || (id == "" && slices.Contains(source.Claims, claim)) {
				source.program = nil
				return key, source, true
			}
		}
	}
	return "", reflexRecord{}, false
}
