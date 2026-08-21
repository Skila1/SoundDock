package external

import "time"

type Capabilities struct {
	OAuth            bool `json:"oauth"`
	ManualToken      bool `json:"manual_token"`
	ListUser         bool `json:"list_user_playlists"`
	PublicPlaylists  bool `json:"public_playlists"`
	PrivatePlaylists bool `json:"private_playlists"`
	Snapshot         bool `json:"snapshot"`
	ISRC             bool `json:"isrc"`
	Album            bool `json:"album"`
	Artwork          bool `json:"artwork"`
	Duration         bool `json:"duration"`
	Incremental      bool `json:"incremental"`
}

type Track struct {
	Provider   string         `json:"provider"`
	ID         string         `json:"provider_track_id"`
	SourceURL  string         `json:"source_url"`
	Title      string         `json:"title"`
	Artists    []string       `json:"artists"`
	Album      string         `json:"album"`
	DurationMS int            `json:"duration_ms"`
	ISRC       string         `json:"isrc"`
	Artwork    string         `json:"artwork"`
	Explicit   bool           `json:"explicit"`
	Extra      map[string]any `json:"provider_metadata,omitempty"`
}

type Playlist struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Owner       string    `json:"owner"`
	Artwork     string    `json:"artwork"`
	TrackCount  int       `json:"track_count"`
	Snapshot    string    `json:"snapshot"`
	Modified    time.Time `json:"modified,omitempty"`
	Public      bool      `json:"public"`
}

type Token struct {
	Access    string
	Refresh   string
	Expiry    time.Time
	AccountID string
	Name      string
	Scopes    []string
}

var Caps = map[string]Capabilities{
	"spotify":     {OAuth: true, ListUser: true, PublicPlaylists: true, PrivatePlaylists: true, Snapshot: true, ISRC: true, Album: true, Artwork: true, Duration: true, Incremental: true},
	"youtube":     {OAuth: true, ListUser: true, PublicPlaylists: true, PrivatePlaylists: true, Artwork: true, Duration: true},
	"soundcloud":  {OAuth: true, ListUser: true, PublicPlaylists: true, PrivatePlaylists: true, Artwork: true, Duration: true},
	"apple_music": {ManualToken: true, ListUser: true, PublicPlaylists: true, PrivatePlaylists: true, ISRC: true, Album: true, Artwork: true, Duration: true, Incremental: true},
}

func Known(p string) bool {
	_, ok := Caps[p]
	return ok
}
