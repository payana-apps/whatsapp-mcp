package main

import "testing"

func TestExtractDirectPathKeepsSignedQuery(t *testing.T) {
	url := "https://mmg.whatsapp.net/v/t62.7117-24/851864318_n.enc?ccb=11-4&oh=01_Q5Aa&oe=6914F3A4&_nc_sid=5e03e0&mms3=true"
	want := "/v/t62.7117-24/851864318_n.enc?ccb=11-4&oh=01_Q5Aa&oe=6914F3A4&_nc_sid=5e03e0&mms3=true"
	if got := extractDirectPathFromURL(url); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShouldAutoDownload(t *testing.T) {
	t.Setenv("WHATSAPP_AUTO_DOWNLOAD", "")
	for typ, want := range map[string]bool{"image": true, "audio": true, "document": true, "video": false, "": false} {
		if got := shouldAutoDownload(typ); got != want {
			t.Errorf("default %q: got %v, want %v", typ, got, want)
		}
	}
	t.Setenv("WHATSAPP_AUTO_DOWNLOAD", "none")
	if shouldAutoDownload("image") {
		t.Error("none should disable auto-download")
	}
	t.Setenv("WHATSAPP_AUTO_DOWNLOAD", "Video, audio")
	if !shouldAutoDownload("video") || shouldAutoDownload("image") {
		t.Error("custom list not honored")
	}
}

func TestLocalMediaNameIsUniqueAndSafe(t *testing.T) {
	a := localMediaName("3EB0BA70", "image_20260803_154120.jpg")
	b := localMediaName("3EB08A24", "image_20260803_154120.jpg")
	if a == b {
		t.Fatal("same filename on two messages must not collide")
	}
	if got := localMediaName("x/../y", "../../etc/passwd"); got != "x___y_passwd" {
		t.Fatalf("unsafe name: %q", got)
	}
}
