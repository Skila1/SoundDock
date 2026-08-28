package scapex

import "strings"

const DefaultMediaPolicy = "m4a-0"

const (
	policyOpus = "opus-0"
	policyM4A  = "m4a-0"
	policyMP3  = "mp3-0"
)

// NormalizePolicy maps a media_policy_id onto an allowlisted id.
// Unknown values become m4a-0. The raw string is never used as a CLI argument.
func NormalizePolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "opus", "opus-0":
		return policyOpus
	case "mp3", "mp3-0":
		return policyMP3
	case "m4a", "m4a-0", "best", "bestaudio", "":
		return policyM4A
	default:
		return policyM4A
	}
}

// FormatArgs returns yt-dlp extract flags for a policy. Values are constants only.
func FormatArgs(policy string) []string {
	switch NormalizePolicy(policy) {
	case policyOpus:
		return []string{"-x", "--audio-format", "opus", "--audio-quality", "0"}
	case policyMP3:
		return []string{"-x", "--audio-format", "mp3", "--audio-quality", "0"}
	default:
		return []string{"-x", "--audio-format", "m4a", "--audio-quality", "0"}
	}
}

// RankedPolicies is best-to-worst among the built-in audio policies.
func RankedPolicies() []string {
	return []string{policyOpus, policyM4A, policyMP3}
}

// BestAllowed picks the highest-ranked policy in allowed, or m4a-0.
func BestAllowed(allowed []string) string {
	set := map[string]bool{}
	for _, a := range allowed {
		set[NormalizePolicy(a)] = true
	}
	for _, p := range RankedPolicies() {
		if set[p] {
			return p
		}
	}
	return DefaultMediaPolicy
}

// CoalesceKey is provider + source_ref + dest_library_id + media_policy_id.
func CoalesceKey(provider, sourceRef, destLibraryID, mediaPolicyID string) string {
	if provider == "" {
		provider = "youtube"
	}
	return strings.Join([]string{
		provider,
		strings.TrimSpace(sourceRef),
		strings.TrimSpace(destLibraryID),
		NormalizePolicy(mediaPolicyID),
	}, "|")
}
