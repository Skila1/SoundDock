package httpapi

import (
	"encoding/json"
	"strconv"
	"strings"
)

// explainJobError turns raw provider or worker errors into admin-readable text.
func explainJobError(jobType, raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "Something went wrong. Check the job and try again."
	}
	low := strings.ToLower(s)
	code := httpStatusIn(s)

	switch {
	case strings.Contains(low, "queue is full") || strings.Contains(low, "queue_full"):
		return "The Sync job queue is full. Idle workers do not add queue space. Open Administration, Workers, raise the Sync queue limit, then retry."
	case strings.Contains(low, "pool is disabled") || strings.Contains(low, "pool_disabled"):
		return "The Sync worker pool is turned off. Enable it under Administration, Workers."
	case code == 401 || strings.Contains(low, "unauthorized"):
		return "The connected account token expired or was revoked. Reconnect the provider in Connected Services and try again."
	case code == 403 || strings.Contains(low, "forbidden"):
		if strings.Contains(jobType, "playlist") || strings.Contains(low, "spotify") || strings.Contains(low, "playlist") {
			return "Spotify refused access to this playlist. Reconnect Spotify in Connected Services, or the playlist may be private, collaborative, or region-locked."
		}
		return "The remote service refused this request. Reconnect the account or check provider settings."
	case code == 404:
		return "The remote playlist or track was not found. It may have been deleted or the link is wrong."
	case code == 429:
		return "The remote service rate-limited SoundDock. Wait a minute and try again."
	case code >= 500:
		return "The remote service had an error. Try again in a few minutes."
	}

	if msg := jsonErrorMessage(s); msg != "" && !looksLikeRawJSON(msg) {
		return capitalizeSentence(msg)
	}
	if i := strings.Index(s, "{"); i >= 0 {
		prefix := strings.TrimSpace(s[:i])
		if prefix != "" && !looksLikeRawJSON(prefix) {
			if c := httpStatusIn(prefix); c > 0 {
				return explainJobError(jobType, prefix)
			}
			return capitalizeSentence(prefix)
		}
	}
	if looksLikeRawJSON(s) {
		return "The remote service returned an error. Try again or reconnect the provider."
	}
	return s
}

func httpStatusIn(s string) int {
	for _, part := range strings.Fields(s) {
		part = strings.Trim(part, ":,")
		if len(part) == 3 {
			if n, err := strconv.Atoi(part); err == nil && n >= 400 && n <= 599 {
				return n
			}
		}
	}
	return 0
}

func jsonErrorMessage(s string) string {
	i := strings.Index(s, "{")
	if i < 0 {
		return ""
	}
	var wrap struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(s[i:]), &wrap) != nil || len(wrap.Error) == 0 {
		return ""
	}
	var obj struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	}
	if json.Unmarshal(wrap.Error, &obj) == nil && strings.TrimSpace(obj.Message) != "" && !strings.EqualFold(obj.Message, "Forbidden") && !strings.EqualFold(obj.Message, "Unauthorized") {
		return obj.Message
	}
	var asString string
	if json.Unmarshal(wrap.Error, &asString) == nil {
		return asString
	}
	return ""
}

func looksLikeRawJSON(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

func capitalizeSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 32
	}
	return string(r)
}
