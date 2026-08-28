yaml
routes:
  - method: GET
    path: /api/v1/me/queue
    handler: Server.getQueue
    auth: requireAuth
  - method: PUT
    path: /api/v1/me/queue
    handler: Server.putQueue
    auth: requireAuth
  - method: POST
    path: /api/v1/me/queue/add
    handler: Server.queueAdd
    auth: requireAuth
  - method: POST
    path: /api/v1/me/queue/control
    handler: Server.queueControl
    auth: requireAuth
  - method: POST
    path: /api/v1/me/listen
    handler: Server.postListen
    auth: requireAuth
  - method: GET
    path: /api/v1/me/party
    handler: Server.getParty
    auth: requireAuth
  - method: POST
    path: /api/v1/me/party
    handler: Server.postParty
    auth: requireAuth
  - method: POST
    path: /api/v1/me/party/votes
    handler: Server.postPartyVote
    auth: requireAuth
jobs:
  - name: party.expire
    payload:
      session_id: "00000000-0000-4000-8000-000000000070"
settings: []
dependencies: []
frontend_routes: []
nav: []
api_types:
  - name: queue.device_id
    fields: { device_id: "string|null" }
  - name: queue.shuffle_mode
    fields: { shuffle_mode: "random|smart|album" }
  - name: queue.stop_after_current
    fields: { stop_after_current: boolean }
  - name: control.reorder
    fields: { extra.from: int, extra.to: int }
  - name: control.ended
    fields: { extra.ended: boolean }
  - name: party.session_id
    fields: { session_id: uuid }
  - name: home.continue.position_ms
    fields: { position_ms: int }
notes:
  - P1 is the only mutator of playback queues; radio enqueues via POST /me/queue/add.
  - New web owner_key is userID:deviceID; migrate the legacy userID row; never change discord_guild keys.
  - Control reorder is additive; do not remove remove/shuffle/repeat.
  - Repeat-one loops when extra.ended=true; extra.position_ms persists on pause.
  - Listen play counts at first of 30s or 50%; skip is next/prev before threshold; stop-after-current is not a skip.
  - party.expire payload is {session_id}; handler is playback.ExpireHandler.
  - Do not apply ReplayGain in ffmpeg stream.
