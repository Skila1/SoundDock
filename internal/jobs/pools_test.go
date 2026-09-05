package jobs

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultConfigsStaySafe(t *testing.T) {
	cfg, err := Enforce(DefaultConfigs())
	if err != nil {
		t.Fatal(err)
	}
	if cfg[PoolPlayback].MinWorkers < 1 || cfg[PoolSearch].MinWorkers < 1 {
		t.Fatal("reserved pools must keep a worker")
	}
	if !cfg[PoolPlayback].Enabled || !cfg[PoolSearch].Enabled {
		t.Fatal("reserved pools must stay enabled")
	}
}

func TestEnforceRejectsZeroPlayback(t *testing.T) {
	in := DefaultConfigs()
	p := in[PoolPlayback]
	p.MinWorkers = 0
	p.MaxWorkers = 0
	p.Enabled = false
	in[PoolPlayback] = p
	_, err := Enforce(in)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Playback") {
		t.Fatalf("got %v", err)
	}
}

func TestEnforceRejectsZeroSearch(t *testing.T) {
	in := DefaultConfigs()
	s := in[PoolSearch]
	s.MinWorkers = 0
	in[PoolSearch] = s
	_, err := Enforce(in)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Search") {
		t.Fatalf("got %v", err)
	}
}

func TestSanitizeRestoresReservedCapacity(t *testing.T) {
	in := DefaultConfigs()
	p := in[PoolPlayback]
	p.Enabled = false
	p.MinWorkers = 0
	p.MaxWorkers = 0
	in[PoolPlayback] = p
	out := Sanitize(in)
	if !out[PoolPlayback].Enabled || out[PoolPlayback].MinWorkers < 1 || out[PoolPlayback].MaxWorkers < 1 {
		t.Fatalf("%+v", out[PoolPlayback])
	}
}

func TestMaintenanceMayBeDisabled(t *testing.T) {
	in := DefaultConfigs()
	m := in[PoolMaintenance]
	m.Enabled = false
	m.MinWorkers = 0
	m.MaxWorkers = 0
	in[PoolMaintenance] = m
	cfg, err := Enforce(in)
	if err != nil {
		t.Fatal(err)
	}
	if cfg[PoolMaintenance].Enabled {
		t.Fatal("maintenance should stay disabled")
	}
	if cfg[PoolPlayback].MinWorkers < 1 {
		t.Fatal("playback capacity lost")
	}
}

func TestPoolForType(t *testing.T) {
	cases := map[string]ID{
		"party.expire":             PoolPlayback,
		"radio.refresh":            PoolPlayback,
		"search.youtube":           PoolSearch,
		"scapex.fetch":             PoolAcquisition,
		"ingest.url":               PoolAcquisition,
		"external.playlist.import": PoolSync,
		"external.playlist.tick":   PoolSync,
		"library.scan":             PoolMaintenance,
		"library.merge":            PoolMaintenance,
		"library.delete":           PoolMaintenance,
		"tracks.bulk_delete":       PoolMaintenance,
		"tracks.metadata":          PoolMaintenance,
		"metadata.refresh":         PoolMaintenance,
		"unknown.job":              PoolMaintenance,
	}
	for typ, want := range cases {
		if got := PoolForType(typ); got != want {
			t.Errorf("%s: got %s want %s", typ, got, want)
		}
	}
}

func TestDoRunsInlineWhenNotStarted(t *testing.T) {
	r := New(nil, nil)
	ran := false
	err := r.Do(context.Background(), PoolSearch, func(ctx context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn not called")
	}
}

func TestClampQueueAndTimeout(t *testing.T) {
	in := DefaultConfigs()
	a := in[PoolAcquisition]
	a.QueueLimit = 1
	a.TimeoutSeconds = 5
	a.MaxWorkers = 100
	in[PoolAcquisition] = a
	out := Sanitize(in)
	if out[PoolAcquisition].QueueLimit < 8 {
		t.Fatal("queue limit floor")
	}
	if out[PoolAcquisition].TimeoutSeconds < 30 {
		t.Fatal("acquisition timeout floor")
	}
	if out[PoolAcquisition].MaxWorkers > 32 {
		t.Fatal("max workers cap")
	}
}
