package main

import (
	"path/filepath"
	"strings"
)

// Payana: media types downloaded as soon as a live message arrives.
// WHATSAPP_AUTO_DOWNLOAD is a comma list ("image,audio,document" by default;
// "video" is opt-in for disk reasons) or "none" to turn it off.
func autoDownloadTypes() map[string]bool {
	raw := strings.TrimSpace(getenvDefault("WHATSAPP_AUTO_DOWNLOAD", "image,audio,document"))
	types := map[string]bool{}
	if strings.EqualFold(raw, "none") {
		return types
	}
	for _, t := range strings.Split(raw, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			types[t] = true
		}
	}
	return types
}

func shouldAutoDownload(mediaType string) bool {
	return mediaType != "" && autoDownloadTypes()[mediaType]
}

// localMediaName makes the on-disk name unique per message and safe as a path
// component: the message ID first, then the original name without directories.
func localMediaName(messageID, filename string) string {
	base := filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	if base == "." || base == "/" || base == "" {
		base = "media"
	}
	id := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_").Replace(messageID)
	return id + "_" + base
}
