# Manual device matrix (Playback Session Pass §40 / §30)

Mark each row **PASS**, **FAIL**, or **NOT RUN**. Do not mark PASS unless a human actually exercised the device.

This agent environment cannot open a phone, PWA install, laptop sleep cycle, or two real browser profiles against a running SoundDock. Automated coverage for the same *logic* lives in Vitest (`sessionReducer.test.ts`, `device.test.ts`, `player.output.test.ts`) and Go (`discord_voice_test.go`, `listener_test.go`).

| Case | Result | Notes |
|---|---|---|
| Browser background tab idle >1 minute | NOT RUN | Needs a real tab left in background while audio continues |
| Two tabs (renderer steal + BroadcastChannel stop) | NOT RUN | Logic covered by `web/src/lib/device.test.ts` and `sessionReducer.test.ts`; not a two-process browser |
| PWA background / foreground | NOT RUN | No installed PWA in this environment |
| Phone screen lock / unlock | NOT RUN | No mobile device |
| Laptop sleep / wake | NOT RUN | Not exercised |
| Stale Browser wakes after Discord took renderer | NOT RUN | Reducer: `shouldStopHtmlAudio` when `output_pref=discord` |
| Stale Browser wakes after another Browser took renderer | NOT RUN | Reducer + BroadcastChannel unit tests |
| Browser → Discord → Browser | NOT RUN | Bind keep + `applySwitchToBrowser` unit tests |
| Discord disconnect while playing | NOT RUN | Server unbind/leave covered in Go; no live bot here |

Last updated: 2026-08-29. All rows remain **NOT RUN** until a person fills them in against a running app.
