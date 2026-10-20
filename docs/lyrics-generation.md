# Lyrics generation

SoundDock can write timed lyrics for songs that no lyrics service knows about, such as music you or your friends made. It listens to the song with [whisper.cpp](https://github.com/ggml-org/whisper.cpp) (MIT licence) and saves what it hears as synced `.lrc` lyrics, so the player can highlight each line and word.

whisper.cpp is not built into SoundDock. It runs as its own optional container, and SoundDock talks to it over its HTTP API (`GET /health`, `POST /inference`). The client lives in `internal/whisper` and uses only the Go standard library.

## Turn it on

1. Start the container with the `lyrics` profile:

   ```bash
   docker compose --profile lyrics up -d
   ```

   The first start downloads the model into the `whisper_models` volume (466 MB for `small`).

2. Add this to `.env` and restart SoundDock:

   ```
   SD_WHISPER_URL=http://whisper:8080
   ```

3. In **Admin > Media Settings > Lyrics generation**, check that the status says **Connected**, then turn on **Allow generating lyrics**.

4. Play a song with no lyrics, open the lyrics panel and press **Generate lyrics**. With the `small` model and 4 threads, a four-minute song took about 20 seconds on a modern desktop CPU; expect a minute or two on a small home server. The panel updates by itself when it is done.

## How it fits with other lyrics

Lookup order is:

1. lyrics you edited by hand
2. embedded tags and `.lrc` files
3. generated lyrics
4. LRCLIB, when turned on

Generation never replaces hand-edited or locked lyrics. Generated lyrics show even when LRCLIB is off. If they are wrong, edit them in the track's metadata editor; your edit then wins. **Regenerate** at the bottom of the lyrics panel runs it again, for example after you change the model or language.

Only administrators can generate lyrics, as with other lyrics edits. Each run is a `lyrics.generate` job in the Maintenance pool, shown under **Admin > Activity > Jobs & Workers**, and can be cancelled there.

## Choosing a model

| `SD_WHISPER_MODEL` | Download | Notes |
|---|---|---|
| `base` | 142 MB | Fast, misses a lot in sung vocals |
| `small` (default) | 466 MB | Good balance for a home server |
| `medium` | 1.5 GB | Better on accents and dense mixes, about 3x slower |
| `large-v3-turbo` | 1.5 GB | Most accurate; best with 8+ cores |

Add `.en` (for example `small.en`) for English-only models, which are a little more accurate for English. Set `SD_WHISPER_THREADS` to the number of CPU cores you want it to use.

Pick the song language in Admin when most of your music is in one language. **Detect for each song** works for mixed libraries.

## Limits

Speech recognition is trained mostly on speech, so loud instruments, heavy effects and screamed vocals lower accuracy. Treat the result as a first draft you can correct. Audio is sent only to the whisper container on your own Docker network; nothing leaves the server.
