package metadata

import "testing"

func TestParseGainAndPeak(t *testing.T) {
	g := parseGain("-6.50 dB")
	if g == nil || *g != -6.5 {
		t.Fatalf("%v", g)
	}
	p := parsePeak("0.988")
	if p == nil || *p != 0.988 {
		t.Fatalf("%v", p)
	}
	if parseGain("") != nil || parsePeak("") != nil {
		t.Fatal("empty")
	}
}

func TestParseITunSMPB(t *testing.T) {
	delay, pad, ok := parseITunSMPB(" 00000000 00000210 0000029C 00000A1C")
	if !ok || delay != 0x210 || pad != 0x29C {
		t.Fatalf("%v %d %d", ok, delay, pad)
	}
}

func TestLyricsTimed(t *testing.T) {
	if !LyricsTimed("[00:12.00] hello") {
		t.Fatal("timed")
	}
	if LyricsTimed("plain unsynced lyrics") {
		t.Fatal("plain")
	}
}

func TestApplyTagsReplayGainAndDepth(t *testing.T) {
	p := Probe{}
	applyTags(&p, map[string]string{
		"REPLAYGAIN_TRACK_GAIN": "-3.0 dB",
		"REPLAYGAIN_ALBUM_GAIN": "-4.0 dB",
		"REPLAYGAIN_TRACK_PEAK": "0.9",
		"LYRICS":                "[01:02.00] line",
		"encoder_delay":         "1105",
		"encoder_padding":       "576",
	})
	if p.ReplayGainTrack == nil || *p.ReplayGainTrack != -3 {
		t.Fatalf("track gain %#v", p.ReplayGainTrack)
	}
	if p.ReplayGainAlbum == nil || *p.ReplayGainAlbum != -4 {
		t.Fatal("album gain")
	}
	if p.EncoderDelay != 1105 || p.EncoderPadding != 576 {
		t.Fatal("encoder gap")
	}
	if !LyricsTimed(p.Lyrics) {
		t.Fatal("lyrics")
	}
}

func TestExternalEnabledDefaultFalse(t *testing.T) {
	if ExternalEnabled(t.Context(), nil) {
		t.Fatal("nil pool must not call MusicBrainz")
	}
}

func TestEnrichMusicBrainzNoopWhenDisabled(t *testing.T) {
	p := Probe{Title: "Numb", Artist: "Linkin Park"}
	EnrichMusicBrainz(t.Context(), nil, &p)
	if p.Source == "musicbrainz" {
		t.Fatal("must not enrich without setting")
	}
}
