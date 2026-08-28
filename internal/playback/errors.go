package playback

import "errors"

var (
	ErrCommandConflict  = errors.New("command_conflict")
	ErrBindConflict     = errors.New("bind_conflict")
	ErrLeaseConflict    = errors.New("lease_conflict")
	ErrInstanceMismatch = errors.New("instance_mismatch")
	ErrUndoStale        = errors.New("undo_stale")
)

const (
	RendererNone    = "none"
	RendererBrowser = "browser"
	RendererDiscord = "discord"
	OutputBrowser   = "browser"
	OutputDiscord   = "discord"
	OriginUser      = "user"
	OriginAutoplay  = "autoplay"
	OriginRadio     = "radio"
)
