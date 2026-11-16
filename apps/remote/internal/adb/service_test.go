package adb

import "testing"

func TestParsePNGSize(t *testing.T) {
	data := []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x04, 0x38,
		0x00, 0x00, 0x07, 0x80,
	}
	width, height, err := parsePNGSize(data)
	if err != nil {
		t.Fatalf("parsePNGSize returned error: %v", err)
	}
	if width != 1080 || height != 1920 {
		t.Fatalf("unexpected size: got %dx%d", width, height)
	}
}

func TestSuggestedAction(t *testing.T) {
	if got := suggestedAction(Status{
		Devices: []Device{{Serial: "emulator-5554", State: "device"}},
	}); got != "debug" {
		t.Fatalf("expected debug, got %q", got)
	}

	if got := suggestedAction(Status{
		EmulatorAvailable: true,
		AvailableAVDs:     []string{"Pixel_8_API_35"},
	}); got != "start" {
		t.Fatalf("expected start, got %q", got)
	}
}
