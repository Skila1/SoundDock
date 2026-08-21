package scan

import (
	"net/url"
	"path"
	"strings"
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
