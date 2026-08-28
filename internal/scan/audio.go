package scan

import (
	"net/url"
	"path"
	"strings"

	"github.com/sounddock/sounddock/internal/metadata"
)

var audioExt = map[string]bool{
	".mp3": true, ".flac": true, ".aac": true, ".m4a": true, ".alac": true,
	".ogg": true, ".opus": true, ".wav": true, ".oga": true, ".aif": true, ".aiff": true,
}

func IsAudioExt(ext string) bool {
	return audioExt[strings.ToLower(ext)]
}

func IsAudioName(name string) bool {
	return IsAudioExt(path.Ext(strings.TrimSpace(name)))
}

func IsZipName(name string) bool {
	return strings.ToLower(path.Ext(strings.TrimSpace(name))) == ".zip"
}

func IsZipContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch ct {
	case "application/zip", "application/x-zip", "application/x-zip-compressed", "multipart/x-zip":
		return true
	default:
		return false
	}
}

func IsUploadName(name string) bool {
	return IsAudioName(name) || IsZipName(name)
}

func ExtFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(path.Ext(raw))
	}
	return strings.ToLower(path.Ext(u.Path))
}

func ExtFromContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch ct {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return ".m4a"
	case "audio/ogg", "application/ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/aiff", "audio/x-aiff":
		return ".aiff"
	default:
		return ""
	}
}

func SkipScanKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "\\", "/"))
	k = strings.TrimPrefix(k, "/")
	if strings.HasPrefix(k, "trash/") || strings.Contains(k, "/trash/") {
		return true
	}
	if strings.HasPrefix(k, "compressed/") || strings.Contains(k, "/compressed/") {
		return true
	}
	return false
}

func HashStorageKey(prefix, hash, ext string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	ext = strings.ToLower(ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if len(hash) < 2 {
		return path.Join(prefix, "uploads", hash+ext)
	}
	return path.Join(prefix, "uploads", hash[:2], hash+ext)
}

func CompanionStorageKey(originalKey, hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) < 2 {
		hash = hash + "00"
	}
	k := strings.ReplaceAll(originalKey, "\\", "/")
	prefix := ""
	switch {
	case strings.HasPrefix(k, "uploads/") || strings.HasPrefix(k, "imports/") || strings.HasPrefix(k, "migrated/"):
		prefix = ""
	default:
		for _, marker := range []string{"/uploads/", "/imports/", "/migrated/"} {
			if i := strings.Index(k, marker); i >= 0 {
				prefix = k[:i]
				break
			}
		}
	}
	return path.Join(prefix, "compressed", hash[:2], hash+".flac")
}

func InboxVideoID(key, kind string) string {
	k := strings.ReplaceAll(strings.TrimSpace(key), "\\", "/")
	k = strings.TrimPrefix(k, "/")
	if kind != "inbox" && !strings.HasPrefix(k, "inbox/") && !strings.Contains(k, "/inbox/") {
		return ""
	}
	base := strings.TrimSuffix(path.Base(k), path.Ext(k))
	if len(base) == 11 {
		for _, c := range base {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
				continue
			}
			return ""
		}
		return base
	}
	return ""
}

func IsHashStorageKey(key string) bool {
	k := strings.ReplaceAll(key, "\\", "/")
	base := path.Base(k)
	dir := path.Base(path.Dir(k))
	if len(dir) != 2 {
		return false
	}
	name := strings.TrimSuffix(base, path.Ext(base))
	return metadata.LooksLikeHash(name) && strings.HasPrefix(strings.ToLower(name), dir)
}

func ResolveAudioExt(name, rawURL, contentType string) string {
	if IsAudioName(name) {
		return strings.ToLower(path.Ext(name))
	}
	if ext := ExtFromURL(rawURL); IsAudioExt(ext) {
		return ext
	}
	if ext := ExtFromContentType(contentType); IsAudioExt(ext) {
		return ext
	}
	return ""
}
