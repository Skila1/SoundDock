# Hardware media keys

SoundDock responds to keyboard media keys (Play/Pause, Next, Previous, Stop), headset buttons, the Windows media flyout, macOS Now Playing, and phone lock screens through the browser's Media Session API. Nothing extra needs installing.

Code: `web/src/stores/mediaSession.ts` (registration, dedupe, key mapping) and the `mediaRemote` handlers in `web/src/stores/player.ts`, which call the same `control()` and `seek()` as the player bar, so queue, shuffle, repeat, party and Discord routing behave exactly as they do for on-screen buttons.

## What is implemented

| Feature | Detail |
|---|---|
| Media Session actions | `play`, `pause`, `nexttrack`, `previoustrack`, `stop`, `seekto`, and `seekforward`/`seekbackward` (±10 s, or the offset the OS asks for). Each is registered separately so an engine that rejects one keeps the rest. |
| Metadata | Title, artists, album, and artwork at 96, 300 and 640 px. Set when a track loads. Cleared (`playbackState = "none"`) when nothing is loaded. |
| State and position | `playbackState` follows the player. `setPositionState` is sent on play/pause, rate, duration or seek changes and on drift over 1 s; browsers extrapolate in between. |
| Foreground key fallback | `keydown` for `MediaPlayPause`, `MediaTrackNext`, `MediaTrackPrevious`, `MediaStop` (by `key` or `code`). Works with player shortcuts turned off and inside text fields. Auto-repeat is ignored. |
| No double actions | A command reaching SoundDock both as a keydown and as a Media Session action within 400 ms runs once. Stamps are shared over `BroadcastChannel`, so this holds across tabs. Repeated presses of the same key still all count. |
| Background play | The Play handler starts the audio element immediately inside the key's task before the server confirms, so a background tab is not refused by autoplay rules. |
| iOS / iPadOS | Seek actions are left unset there because they replace the lock-screen Next/Previous buttons. |
| Page navigation | Handlers are registered once from `AppShell` and live in the player store, so moving between pages never drops them. |

## How the browser routes keys

Chromium browsers (Chrome, Edge, Brave, Opera, Vivaldi) and Firefox register with the Windows System Media Transport Controls (SMTC). When you press a media key, Windows sends it to the current SMTC session, and the browser forwards it to the tab whose Media Session is active, **whether or not the tab or window has focus**. This is the same path YouTube Music and Spotify Web use. A page never sees a global key event; it only receives the action the browser chooses to forward.

Consequences:

- **The SoundDock tab must be playing (or have recently played) audio itself.** A tab that has never played audio has no active session and gets no keys.
- **Discord output:** see "Keys and the Discord bot" below.
- **Other media apps compete.** Spotify desktop, another tab playing YouTube, VLC and so on can all hold the Windows media session. Windows sends keys to whichever was active most recently. Pause or close the other player, or start playback in SoundDock last.
- **Several SoundDock tabs:** only the tab that owns the audio lease plays sound, and the browser routes keys to it. Other SoundDock tabs control the same shared session through the server, so even if a key lands on a different SoundDock tab, it acts on the same queue. The 400 ms dedupe across tabs stops one press from acting twice.

## Browser notes (Windows 11)

| Browser | Background media keys | Notes |
|---|---|---|
| Chrome, Edge | Yes, default | Global media controls show artwork and the seek buttons. |
| Brave | Yes, default | Same Chromium path. The old `brave://flags/#hardware-media-key-handling` flag was removed upstream, so newer builds do not show it. If keys never reach Brave, check that it was not launched with `--disable-features=HardwareMediaKeyHandling` (shortcut Target field), and close other media apps. |
| Opera / Opera GX | Yes, default | Opera's sidebar "Player" (Spotify/Apple Music/YouTube Music panel) can hold the media session while it plays; pause it if keys go there instead. |
| Firefox | Yes, default | Controlled by `media.hardwaremediakeys.enabled` in `about:config` (default `true`). If it is `false`, keys only work while the SoundDock tab is focused (the keydown fallback). |
| Safari (macOS/iOS) | Yes | Uses Now Playing and the lock screen; `audioSession.type = "playback"` is set. |

No flag or extension is needed in a default install of any of these browsers. Installing SoundDock as a PWA does not change media key routing, but it keeps the player in its own window, which makes it less likely another tab steals the session.

## Keys and the Discord bot

Media keys are only ever read by the web app in the browser. The bot never listens for keys. A key press does exactly what the matching player bar button does, so when the web app's session is playing in a Discord voice channel, the keys control that playback through the same controls.

Browser note: the browser only forwards background key presses to a tab that is playing audio itself. While output is set to Discord the tab plays no sound, so the keys reach it when the SoundDock tab is focused, but not from the background.

## Manual test (Windows 11, Brave or another Chromium)

Confirmed keys: `MediaPlayPause` (VK 179 / 0xB3), `MediaNextTrack` (VK 176 / 0xB0), `MediaPreviousTrack` (VK 177 / 0xB1). In the browser these arrive as `KeyboardEvent.key` `MediaPlayPause`, `MediaTrackNext` and `MediaTrackPrevious`.

1. Close or pause Spotify desktop and any other tab playing audio.
2. Open SoundDock, choose Browser output, play a queue of at least three songs.
3. Press **Play/Pause**: playback pauses; press again: it resumes from the same position.
4. Press **Next**, then **Previous**: the queue advances and goes back, matching the player bar's buttons.
5. Switch to another tab, then minimise the window. Repeat steps 3 and 4. All must still work.
6. Press the volume key to open the Windows media flyout: title, artist and artwork are shown, and the position moves. Use the flyout's buttons and seek bar.
7. Turn on shuffle and repeat-one; check that Next and Previous behave the same as the player bar buttons.
8. Navigate between Home, Library and an album page while playing; the keys keep working.
9. Open a second SoundDock tab. Press Play/Pause once: it pauses once (not pause then resume). Next skips exactly one song.
10. Switch output to Discord while in a voice channel, keep the SoundDock tab focused, and press the keys: the web app's playback in the voice channel responds the same as with the player bar buttons.
11. Firefox only: set `media.hardwaremediakeys.enabled` to `false`, restart, and confirm the keys work only while the tab is focused.

Record results in `device-matrix.md`.
