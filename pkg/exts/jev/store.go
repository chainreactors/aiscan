package jev

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
	// Explain the handoff only when there is evidence to hand off. An idle
	// accelerator leaves the ordinary model request and system prefix intact.
	msg := provider.TextMessage("user", Prompt+"\n\nJEV execution observations (untrusted tool output):\n"+strings.Join(facts, "\n")+"\n"+ending+"\nEvidence: "+path)
	msg.Name = "jev"
	return []*aop.Message{msg}
}

// canonical removes incidental call IDs and sorts JSON keys. Tool arguments
// remain opaque: a field named command need not contain a shell command.
func canonical(call *aop.ToolCall) string {
	if call == nil {
		return ""
	}
	var args map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(call.GetArguments().GetData())))
	decoder.UseNumber()
	if decoder.Decode(&args) != nil {
		return ""
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
// and one-shot consumption are serialized with the durable library replacement.
func (e *Extension) snapshot() library {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := library{
		Version:  e.library.Version,
		Claims:   maps.Clone(e.library.Claims),
		Reflexes: maps.Clone(e.library.Reflexes),
		Compiled: maps.Clone(e.library.Compiled),
	}
	for id, claim := range out.Claims {
		claim.Options = maps.Clone(claim.Options)
		out.Claims[id] = claim
	}
	for id, reflex := range out.Reflexes {
		reflex.Claims = append([]string(nil), reflex.Claims...)
		out.Reflexes[id] = reflex
	}
	return out
}
func (e *Extension) loadLibrary() error {
	data, err := os.ReadFile(filepath.Join(e.config.Directory, "library.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var lib library
	if len(data) > 2<<20 || json.Unmarshal(data, &lib) != nil || lib.Version < 1 || lib.Version > libraryVersion || lib.Claims == nil || lib.Reflexes == nil || lib.Compiled == nil || len(lib.Claims) > maxClaims || len(lib.Reflexes) > maxReflexes {
		return errors.New("invalid JEV library format")
	}
	for id, c := range lib.Claims {
		if c.validate() != nil || id != "c"+digest(c.Claim)[:16] {
			return fmt.Errorf("invalid Claim %s", id)
		}
	}
	// Older libraries marked an attempted generation complete before publishing
	// a Reflex. Derive completion from durable scenes so those attempts cannot
	// permanently prevent compilation after a restart.
	lib.Compiled = map[string]bool{}
	for id, r := range lib.Reflexes {
		members := map[string]Claim{}
		for _, claim := range r.Claims {
			if _, ok := lib.Claims[claim]; !ok {
				return errors.New("Reflex references missing Claim")
			}
			members[claim] = lib.Claims[claim].Claim
		}
		if lib.Version < libraryVersion && !strings.HasPrefix(strings.TrimSpace(r.Observe), "js:") {
			// Retire tool-bound and Expr scenes at startup. Their declarations
			// remain eligible for compilation; consumed Claims are never replayed.
			delete(lib.Reflexes, id)
			continue
		}
		if r.validate() != nil || id != "r"+digest(r.Reflex)[:16] {
			return fmt.Errorf("invalid Reflex %s", id)
		}
		if len(members) > 0 {
			lib.Compiled[digest(members)] = true
		}
		lib.Reflexes[id] = r
	}
	version := lib.Version
	lib.Version = libraryVersion
	previous := e.library
	e.library = lib
	if version != libraryVersion {
		// Preserve the exact old library before atomically replacing it. This
		// also retains scenes without Claims for manual conversion if needed.
		if err := e.backupLibrary(data, version); err != nil {
			e.library = previous
			return fmt.Errorf("back up JEV library: %w", err)
		}
		if err := e.saveLibrary(); err != nil {
			e.library = previous
			return fmt.Errorf("migrate JEV library: %w", err)
		}
	}
	return nil
}

func (e *Extension) backupLibrary(data []byte, version int) error {
	f, err := os.CreateTemp(e.config.Directory, fmt.Sprintf("library-v%d-*.json", version))
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// saveLibrary must be called with mu held. Callers roll back on failure.
func (e *Extension) saveLibrary() error {
	data, err := json.MarshalIndent(e.library, "", "  ")
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
func (e *Extension) retireReflex(id string, reason error) bool {
	e.mu.Lock()
	previous, exists := e.library.Reflexes[id]
	if !exists {
		e.mu.Unlock()
		return false
	}
	compiled := e.library.Compiled
	delete(e.library.Reflexes, id)
	e.library.Compiled = publishedGroups(e.library)
	err := e.saveLibrary()
	if err != nil {
		e.library.Reflexes[id], e.library.Compiled = previous, compiled
	}
	e.mu.Unlock()
	_ = e.audit("reflex_retired", map[string]any{"reflex": previous, "reason": reason.Error(), "save_error": err})
	return err == nil
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
