package cloud

import "testing"

// TestSortImagesPrefersBareOverGPU guards the deploy default: a plain Ubuntu LTS
// image must outrank an otherwise-identical GPU/CUDA bundle, since the agent
// crawls with katana's standard engine and needs no browser or GPU.
func TestSortImagesPrefersBareOverGPU(t *testing.T) {
	imgs := []Image{
		// GPU image listed first on purpose — API ordering must not decide the default.
		{ID: "gpu", OSName: "Ubuntu 24.04 64位 预装NVIDIA GPU驱动和CUDA", Arch: "x86_64"},
		{ID: "bare", OSName: "Ubuntu 24.04 64位", Arch: "x86_64"},
	}
	sortImages(imgs)
	if imgs[0].ID != "bare" {
		t.Fatalf("default image = %q, want plain Ubuntu %q", imgs[0].ID, "bare")
	}
	if got := imageScore(imgs[0]) - imageScore(imgs[1]); got <= 0 {
		t.Fatalf("bare image should outscore GPU image, got delta %d", got)
	}
}

// TestImageScorePenalizesGPU pins the GPU penalty across the common markers.
func TestImageScorePenalizesGPU(t *testing.T) {
	bare := imageScore(Image{OSName: "Ubuntu 24.04 64位", Arch: "x86_64"})
	for _, marker := range []string{"GPU驱动", "CUDA 12.4", "NVIDIA Tesla"} {
		heavy := imageScore(Image{OSName: "Ubuntu 24.04 " + marker, Arch: "x86_64"})
		if heavy >= bare {
			t.Errorf("image with %q scored %d, want below bare %d", marker, heavy, bare)
		}
	}
}
