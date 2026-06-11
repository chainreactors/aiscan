package client

import (
	"testing"

	"github.com/chainreactors/sdk/pkg/association"
)

func TestNewWithoutOptions(t *testing.T) {
	c := New()
	defer c.Close()

	if len(c.opts.providers) != 0 {
		t.Fatal("expected empty providers with no options")
	}
}

func TestClientEnginesAreTypeSafeAndCached(t *testing.T) {
	c := New()
	defer c.Close()

	fingers1, err := c.Fingers()
	if err != nil {
		t.Fatalf("get fingers engine: %v", err)
	}
	fingers2, err := c.Fingers()
	if err != nil {
		t.Fatalf("get fingers engine again: %v", err)
	}
	if fingers1 != fingers2 {
		t.Fatal("expected Fingers() to return cached engine")
	}

	gogoEngine, err := c.Gogo()
	if err != nil {
		t.Fatalf("get gogo engine: %v", err)
	}
	if gogoEngine.Name() != "gogo" {
		t.Fatalf("unexpected gogo engine name: %s", gogoEngine.Name())
	}

	sprayEngine, err := c.Spray()
	if err != nil {
		t.Fatalf("get spray engine: %v", err)
	}
	if sprayEngine.Name() != "spray" {
		t.Fatalf("unexpected spray engine name: %s", sprayEngine.Name())
	}

	neutronEngine, err := c.Neutron()
	if err != nil {
		t.Fatalf("get neutron engine: %v", err)
	}
	if neutronEngine.Name() != "neutron" {
		t.Fatalf("unexpected neutron engine name: %s", neutronEngine.Name())
	}

	zombieEngine, err := c.Zombie()
	if err != nil {
		t.Fatalf("get zombie engine: %v", err)
	}
	if zombieEngine.Name() != "zombie" {
		t.Fatalf("unexpected zombie engine name: %s", zombieEngine.Name())
	}
}

func TestGogoResolvesDependencies(t *testing.T) {
	c := New()
	defer c.Close()

	if c.fingers != nil || c.neutron != nil {
		t.Fatal("engines should be nil before first access")
	}

	_, err := c.Gogo()
	if err != nil {
		t.Fatalf("get gogo engine: %v", err)
	}

	c.mu.Lock()
	hasFinger := c.fingers != nil
	hasNeutron := c.neutron != nil
	c.mu.Unlock()

	if !hasFinger {
		t.Fatal("expected fingers engine to be created as gogo dependency")
	}
	if !hasNeutron {
		t.Fatal("expected neutron engine to be created as gogo dependency")
	}
}

func TestSprayResolvesFingersDependency(t *testing.T) {
	c := New()
	defer c.Close()

	_, err := c.Spray()
	if err != nil {
		t.Fatalf("get spray engine: %v", err)
	}

	c.mu.Lock()
	hasFinger := c.fingers != nil
	c.mu.Unlock()

	if !hasFinger {
		t.Fatal("expected fingers engine to be created as spray dependency")
	}
}

func TestCloseResetsEngines(t *testing.T) {
	c := New()

	_, _ = c.Fingers()
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	c.mu.Lock()
	isNil := c.fingers == nil
	c.mu.Unlock()

	if !isNil {
		t.Fatal("expected engines to be nil after Close")
	}
}

func TestIndexDisabledByDefault(t *testing.T) {
	c := New()
	defer c.Close()

	idx, err := c.Index()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != nil {
		t.Fatal("expected nil index when WithIndex not set")
	}
}

func TestIndexEnabledWithOption(t *testing.T) {
	c := New(WithIndex(nil))
	defer c.Close()

	idx, err := c.Index()
	if err != nil {
		t.Fatalf("get index: %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index when WithIndex is set")
	}

	c.mu.Lock()
	hasFinger := c.fingers != nil
	hasNeutron := c.neutron != nil
	c.mu.Unlock()

	if !hasFinger {
		t.Fatal("expected fingers to be created as index dependency")
	}
	if !hasNeutron {
		t.Fatal("expected neutron to be created as index dependency")
	}
}

func TestIndexCached(t *testing.T) {
	c := New(WithIndex(nil))
	defer c.Close()

	idx1, _ := c.Index()
	idx2, _ := c.Index()

	if idx1 != idx2 {
		t.Fatal("expected Index() to return the same cached instance")
	}
}

func TestIndexAndGogoAreIndependent(t *testing.T) {
	c := New(WithIndex(nil))
	defer c.Close()

	idx, _ := c.Index()
	if idx == nil {
		t.Fatal("expected client index to be built")
	}

	_, err := c.Gogo()
	if err != nil {
		t.Fatalf("get gogo: %v", err)
	}

	idx2, _ := c.Index()
	if idx2 != idx {
		t.Fatal("expected client index to remain the same after Gogo init")
	}
}

func TestLookupWithoutIndex(t *testing.T) {
	c := New()
	defer c.Close()

	_, err := c.Lookup(association.NewQuery().WithFingers("test"))
	if err == nil {
		t.Fatal("expected error when index is not enabled")
	}
}

func TestLookupByFinger(t *testing.T) {
	c := New(WithIndex(nil))
	defer c.Close()

	result, err := c.LookupByFinger("nonexistent")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestLookupByCVE(t *testing.T) {
	c := New(WithIndex(nil))
	defer c.Close()

	result, err := c.LookupByCVE("CVE-9999-0001")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
