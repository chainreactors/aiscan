package engine

import "testing"

func TestNormalizeAniOptionsUsesAiscanDefaults(t *testing.T) {
	got := normalizeAniOptions(ReconOptions{})

	if got.AniDepth != DefaultAniDepth || !got.AniDepthSet {
		t.Fatalf("AniDepth = %d set=%v, want %d true", got.AniDepth, got.AniDepthSet, DefaultAniDepth)
	}
	if got.AniPercent != DefaultAniPercent || !got.AniPercentSet {
		t.Fatalf("AniPercent = %v set=%v, want %v true", got.AniPercent, got.AniPercentSet, DefaultAniPercent)
	}
}

func TestNormalizeAniOptionsPreservesExplicitZero(t *testing.T) {
	got := normalizeAniOptions(ReconOptions{
		AniDepthSet:   true,
		AniPercentSet: true,
	})

	if got.AniDepth != 0 || !got.AniDepthSet {
		t.Fatalf("AniDepth = %d set=%v, want 0 true", got.AniDepth, got.AniDepthSet)
	}
	if got.AniPercent != 0 || !got.AniPercentSet {
		t.Fatalf("AniPercent = %v set=%v, want 0 true", got.AniPercent, got.AniPercentSet)
	}
}

func TestMergeReconOptionsRequiresExplicitAniSetFlags(t *testing.T) {
	base := ReconOptions{AniDepth: DefaultAniDepth, AniDepthSet: true, AniPercent: DefaultAniPercent, AniPercentSet: true}

	got := mergeReconOptions(base, ReconOptions{AniDepth: 2, AniPercent: 0.8})
	if got.AniDepth != DefaultAniDepth || got.AniPercent != DefaultAniPercent {
		t.Fatalf("implicit ani values changed defaults: %#v", got)
	}

	got = mergeReconOptions(base, ReconOptions{AniDepth: 0, AniDepthSet: true, AniPercent: 0, AniPercentSet: true})
	if got.AniDepth != 0 || got.AniPercent != 0 {
		t.Fatalf("explicit ani zero not preserved: %#v", got)
	}
}
