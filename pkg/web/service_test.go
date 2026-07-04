package web

import (
	"reflect"
	"testing"
)

func TestScanRequestAnalysisOptions(t *testing.T) {
	verify, sniper, deep := ScanRequest{Verify: true, Deep: true}.AnalysisOptions()
	if !verify || sniper || !deep {
		t.Fatalf("new analysis options = verify:%v sniper:%v deep:%v", verify, sniper, deep)
	}

	verify, sniper, deep = ScanRequest{AI: true}.AnalysisOptions()
	if !verify || !sniper || deep {
		t.Fatalf("legacy AI options = verify:%v sniper:%v deep:%v", verify, sniper, deep)
	}
}

func TestScanArgsForSelectedAnalysisOptions(t *testing.T) {
	job := &ScanJob{
		Target: "127.0.0.1",
		Mode:   "full",
		Verify: true,
		Sniper: true,
		Deep:   true,
	}

	got := scanArgsForJob(job)
	want := []string{"-i", "127.0.0.1", "--mode", "full", "--verify=high", "--sniper", "--deep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scan args = %#v, want %#v", got, want)
	}
}

func TestServiceStatusReportsLLMAvailability(t *testing.T) {
	service := NewService(ServiceConfig{})
	if service.Status().LLMAvailable {
		t.Fatal("LLMAvailable = true, want false without provider")
	}
}
