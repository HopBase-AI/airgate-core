package plugin

import "testing"

func TestAssetContentTypeCoversReferenceMedia(t *testing.T) {
	keys := map[string]string{
		"task-input/7/202609/a.mp4": "video/mp4",
		"task-input/7/202609/a.MOV": "video/quicktime",
		"task-input/7/202609/a.mp3": "audio/mpeg",
		"task-input/7/202609/a.wav": "audio/wav",
		"task-input/7/202609/a.bin": "application/octet-stream",
	}
	for key, want := range keys {
		if got := contentTypeForAssetKey(key); got != want {
			t.Errorf("contentTypeForAssetKey(%q) = %q, want %q", key, got, want)
		}
	}

	types := map[string]string{
		"video/mp4":       ".mp4",
		"video/quicktime": ".mov",
		"audio/mpeg":      ".mp3",
		"audio/mp3":       ".mp3",
		"audio/wav":       ".wav",
		"audio/x-wav":     ".wav",
		"audio/wave":      ".wav",
	}
	for contentType, want := range types {
		if got := extensionForContentType(contentType); got != want {
			t.Errorf("extensionForContentType(%q) = %q, want %q", contentType, got, want)
		}
	}
}
