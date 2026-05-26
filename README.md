<div align="center">

# prifa

**Private, fast video-call server.** Self-hosted, JWT-authenticated, HTTP/3.

[![Go Version](https://img.shields.io/badge/go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Transport](https://img.shields.io/badge/transport-HTTP%2F3%20%2B%20QUIC-FF6B35)](https://datatracker.ietf.org/doc/html/rfc9114)
[![Auth](https://img.shields.io/badge/auth-JWT%20HS256-FFC107?logo=jsonwebtokens&logoColor=black)](#authentication)
[![Status](https://img.shields.io/badge/status-production%20ready-success)](#deployment)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen)](#contributing)

[**Quick start**](#quick-start) ·
[**Why prifa**](#why-prifa) ·
[**API**](#api-reference) ·
[**JavaScript**](#javascript-integration) ·
[**Deploy**](#deployment) ·
[**Architecture**](#architecture)

</div>

---

A self-hosted, drop-in video-call backend. Your application server mints a
short-lived JWT, your client passes it to prifa, and prifa fans audio and
video bytes between participants over HTTP/3. No third-party media plane,
no user database inside prifa, no proprietary protocols on the wire — just
streaming HTTP that any client (browser, mobile, server-to-server) can drive.

```
   Your app  ──►  mints JWT  ──►  Browser  ──►  HTTP/3 (QUIC)  ──►  prifa
                                                                     ├── REST     /api/rooms, /api/.../participants
                                                                     ├── SSE      /api/rooms/{id}/events
                                                                     └── Streams  /api/.../tracks/{audio|video}
```

---

## Why prifa

|                          | prifa | Bring-your-own SFU (Janus, mediasoup, LiveKit) | SaaS (Zoom, Daily, Agora) |
| ------------------------ | :---: | :--------------------------------------------: | :-----------------------: |
| Self-hosted              |  ✓    |                       ✓                        |             —             |
| No external user DB      |  ✓    |                       —                        |             —             |
| HTTP/3 first             |  ✓    |                       —                        |             ~             |
| Stdlib-only auth         |  ✓    |                       —                        |             —             |
| No RTP / SRTP / codec parsing |  ✓ (just bytes)    |                       — (full SFU)                       |        — (full SFU)       |
| One static binary        |  ✓    |                       —                        |             —             |
| Suits 1k-viewer broadcasts | —     |                       ✓                        |             ✓             |

prifa is the right tool when you want **a small, private real-time relay
behind your own SaaS** — calls, meetings, two-way support sessions. It is
**not** an SFU: there is no congestion control, no simulcast, no codec
parsing. Media is opaque bytes (typically fragmented WebM from the browser).

---

## Features

- **JWT bearer auth** on every endpoint. Pure-stdlib HS256, no external
  deps. Tokens carry `sub`, optional `room` binding, optional `scope`
  restriction, and standard `iss` / `aud` / `exp` claims.
- **HTTP/3 (QUIC) + HTTPS** on the same port. Browsers load the page over
  HTTPS, then silently upgrade follow-up requests to h3 via `Alt-Svc`.
- **Independent audio and video tracks.** Mute, audio-only, video-only,
  and per-kind toggles fall out of the protocol for free.
- **Server-Sent Events** for room control (`participant.joined`,
  `track.started`, …). Late-joiner init-segment caching for WebM.
- **Structured logs** (`log/slog`) with per-request IDs and per-stream
  byte/duration accounting. JSON or text output.
- **Production knobs**: CORS allowlist, issuer/audience verification, log
  level, `/healthz` + `/readyz`, graceful shutdown, empty-room janitor.
- **Demo browser client** (vanilla JS, no framework) plus an offline JWT
  minter (`cmd/token`) and a self-signed cert generator (`cmd/gencert`).

---

## Quick start

```bash
# 1. Generate a self-signed cert for localhost (or use mkcert / your own).
go run ./cmd/gencert -hosts localhost,127.0.0.1,::1

# 2. Pick a JWT signing secret. Keep it in your secret store in production.
export PRIFA_JWT_SECRET=$(openssl rand -hex 32)

# 3. Run with the dev token endpoint enabled so the demo page can mint tokens.
go run . -addr :8443 \
         -cert certs/cert.pem -key certs/key.pem \
         -dev-tokens
```

Open <https://localhost:8443/> in two browser windows. In each:

1. Click **Get dev token**
2. Click **Join / Create**
3. Click **Start mic** and/or **Start camera**

In production, drop `-dev-tokens` and let your application backend mint
tokens directly with the same `PRIFA_JWT_SECRET`.

---

## Repository layout

```
.
├── main.go                       # entrypoint; loads config, builds handler
├── cmd/
│   ├── gencert/                  # self-signed cert generator
│   └── token/                    # offline JWT minter
├── internal/
│   ├── api/                      # HTTP handlers (REST, SSE, streaming media)
│   ├── auth/                     # HS256 JWT sign/verify + middleware
│   ├── config/                   # flags + env loader
│   ├── logx/                     # slog setup + access-log middleware
│   ├── room/                     # in-memory rooms, participants, tracks
│   └── server/                   # HTTP/3 + HTTPS bootstrap
├── web/                          # demo browser client (vanilla JS)
└── certs/                        # generated cert + key (gitignored)
```

---

## Configuration

Every flag has an env equivalent — env wins when set. Required values are
**bold**.

| Flag                | Env                       | Default            | Description                                                            |
| ------------------- | ------------------------- | ------------------ | ---------------------------------------------------------------------- |
| `-addr`             | `PRIFA_ADDR`              | `:8443`            | Listen address (used for both TCP/HTTPS and UDP/QUIC).                 |
| `-cert`             | `PRIFA_CERT`              | `certs/cert.pem`   | TLS certificate.                                                       |
| `-key`              | `PRIFA_KEY`               | `certs/key.pem`    | TLS private key.                                                       |
| `-web`              | `PRIFA_WEB_DIR`           | `web`              | Static client directory; empty disables.                               |
| `-no-https`         | `PRIFA_NO_HTTPS`          | `false`            | Disable TCP/HTTPS (HTTP/3 only).                                       |
| **`-jwt-secret`**   | **`PRIFA_JWT_SECRET`**    | —                  | **Required.** HS256 secret. Prefer the env variable.                   |
| `-jwt-issuer`       | `PRIFA_JWT_ISSUER`        | —                  | If set, tokens must carry this `iss` claim.                            |
| `-jwt-audience`     | `PRIFA_JWT_AUDIENCE`      | —                  | If set, tokens must carry this `aud` claim.                            |
| `-auth-optional`    | `PRIFA_AUTH_OPTIONAL`     | `false`            | Accept anonymous requests. **Dev only.**                               |
| `-dev-tokens`       | `PRIFA_DEV_TOKENS`        | `false`            | Mount `POST /api/auth/token`. **Dev only.**                            |
| `-dev-token-ttl`    | `PRIFA_DEV_TOKEN_TTL`     | `1h`               | Default TTL for dev-minted tokens.                                     |
| `-allowed-origins`  | `PRIFA_ALLOWED_ORIGINS`   | reflect any        | Comma-separated CORS allowlist.                                        |
| `-log-level`        | `PRIFA_LOG_LEVEL`         | `info`             | `debug` · `info` · `warn` · `error`.                                   |
| `-log-format`       | `PRIFA_LOG_FORMAT`        | `text`             | `text` or `json`. Use `json` in production.                            |

The server refuses to start without `PRIFA_JWT_SECRET` unless you opt in
to `-auth-optional`.

---

## Authentication

Every `/api/*` endpoint requires a bearer token signed HS256 with
`PRIFA_JWT_SECRET`. There is no user database inside prifa — your
application backend is the trust root.

### Token format

```jsonc
{
  "sub":   "user-42",                            // your external user id (-> Participant.UserID)
  "name":  "Alice",                              // display name (overrides request body)
  "room":  "abc123",                             // optional: bind token to one room
  "scope": ["track.publish","track.subscribe"],  // optional: empty == full access
  "iss":   "your-app",                           // optional, only checked if server has -jwt-issuer
  "aud":   "prifa",                              // optional, only checked if server has -jwt-audience
  "iat":   1717000000,
  "exp":   1717003600                            // short-lived; minutes-to-hours is typical
}
```

### Scopes

| Scope               | Allows                                                       |
| ------------------- | ------------------------------------------------------------ |
| `room.create`       | `POST /api/rooms`                                            |
| `room.list`         | `GET /api/rooms`                                             |
| `room.join`         | `POST /api/rooms/{id}/participants`                          |
| `track.publish`     | `POST /api/rooms/{id}/participants/{pid}/tracks/{kind}`      |
| `track.subscribe`   | `GET  /api/rooms/{id}/participants/{pid}/tracks/{kind}`      |

A token with no scopes has **full access**. Scopes are an opt-in
restriction. When `room` is set, the token only works against that one
room — use this to issue per-meeting tokens from your backend.

### How the client attaches the token

| Surface                       | Mechanism                                                                   |
| ----------------------------- | --------------------------------------------------------------------------- |
| REST + streaming `fetch`      | `Authorization: Bearer <jwt>`                                               |
| Native `EventSource` (SSE)    | append `?token=<jwt>` (EventSource cannot carry custom headers)             |
| `<video src=…>` with token    | append `?token=<jwt>`                                                       |

### Minting tokens

<details>
<summary><b>CLI (offline, for testing)</b></summary>

```bash
go run ./cmd/token \
    -secret "$PRIFA_JWT_SECRET" \
    -sub user-42 -name Alice \
    -room abc123 \
    -scope track.publish,track.subscribe \
    -ttl 1h
```

</details>

<details>
<summary><b>Node.js (jsonwebtoken)</b></summary>

```js
import jwt from 'jsonwebtoken';

const token = jwt.sign(
  {
    sub:   user.id,
    name:  user.displayName,
    room:  roomId,
    scope: ['room.join','track.publish','track.subscribe'],
  },
  process.env.PRIFA_JWT_SECRET,
  { algorithm: 'HS256', expiresIn: '1h', issuer: 'your-app', audience: 'prifa' },
);
```

</details>

<details>
<summary><b>Python (PyJWT)</b></summary>

```python
import jwt, time, os

token = jwt.encode({
  "sub":   user.id,
  "name":  user.display_name,
  "room":  room_id,
  "scope": ["room.join","track.publish","track.subscribe"],
  "iat":   int(time.time()),
  "exp":   int(time.time()) + 3600,
}, os.environ["PRIFA_JWT_SECRET"], algorithm="HS256")
```

</details>

<details>
<summary><b>Go (golang-jwt)</b></summary>

```go
tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
    "sub":   userID,
    "name":  displayName,
    "room":  roomID,
    "scope": []string{"room.join", "track.publish", "track.subscribe"},
    "iat":   time.Now().Unix(),
    "exp":   time.Now().Add(time.Hour).Unix(),
})
s, _ := tok.SignedString([]byte(os.Getenv("PRIFA_JWT_SECRET")))
```

</details>

<details>
<summary><b>Local dev (POST /api/auth/token)</b></summary>

Run the server with `-dev-tokens`, then:

```bash
curl -X POST -H 'Content-Type: application/json' \
     -d '{"sub":"me","name":"Alice","room":"abc123","scope":[],"ttlSeconds":3600}' \
     https://localhost:8443/api/auth/token
```

Returns `{ "token": "...", "expiresAt": 1717003600, "tokenType": "Bearer" }`.

</details>

---

## API reference

All paths below require `Authorization: Bearer <jwt>` unless noted.
`{roomID}` is the id returned by `POST /api/rooms`; `{pid}` is the
participant id returned by join; `{kind}` is `audio` or `video`.

### Rooms

| Method | Path                          | Body                              | Returns                                                       |
| ------ | ----------------------------- | --------------------------------- | ------------------------------------------------------------- |
| `POST` | `/api/rooms`                  | `{ "name": "string?" }`           | `{ id, name, createdAt }`                                     |
| `GET`  | `/api/rooms`                  | —                                 | `{ rooms: [...] }` (filtered to the token's room when bound)  |
| `GET`  | `/api/rooms/{roomID}`         | —                                 | room detail incl. participants and active tracks              |

### Participants

| Method   | Path                                          | Body                    | Returns                                                                    |
| -------- | --------------------------------------------- | ----------------------- | -------------------------------------------------------------------------- |
| `POST`   | `/api/rooms/{roomID}/participants`            | `{ "name": "string?" }` | `{ room, participant: { id, userId, name, joinedAt } }`                    |
| `DELETE` | `/api/rooms/{roomID}/participants/{pid}`      | —                       | `204`                                                                      |

The JWT `sub` claim becomes `participant.userId` so your application can
join the session back to its own user record.

### Events (Server-Sent Events)

```
GET /api/rooms/{roomID}/events?participant={pid}
```

Pass the token via `Authorization` (if possible) or `?token=<jwt>`. First
event is always `hello` with the current participant list. Subsequent:

| Event                  | Payload                                                                        |
| ---------------------- | ------------------------------------------------------------------------------ |
| `participant.joined`   | `{ id, name, joinedAt, … }`                                                    |
| `participant.left`     | `{ id, name }`                                                                 |
| `track.started`        | `{ participant, kind, contentType }`                                           |
| `track.ended`          | `{ participant, kind }`                                                        |

### Publish media

```
POST /api/rooms/{roomID}/participants/{pid}/tracks/{kind}
Authorization: Bearer <jwt>
Content-Type: video/webm;codecs=vp8     (or audio/webm;codecs=opus, etc)
<streaming body>
```

`{kind}` is **`audio`** or **`video`** — they are completely independent
tracks. A participant can publish:

- audio only (audio-only call),
- video only (camera-only, no mic),
- both (one streaming POST per kind, in parallel),
- start one, stop, restart, leave the other untouched
  ("mute" = stop the audio POST; "unmute" = start a new one).

Each kind emits its own `track.started` / `track.ended` event so peers
attach / detach a single stream without disturbing the other.

### Subscribe to media

```
GET /api/rooms/{roomID}/participants/{pid}/tracks/{kind}?subscriber=any-id
Authorization: Bearer <jwt>
```

Returns 200 with the publisher's `Content-Type` and a streaming body
mirroring what the publisher uploaded. The publisher's first chunk is
cached and replayed to late subscribers (so WebM init segments don't get
lost). Returns 404 if no track of that kind is currently active.

### Operations

| Method | Path                  | Auth | Purpose                                                            |
| ------ | --------------------- | :--: | ------------------------------------------------------------------ |
| `GET`  | `/healthz`            |  —   | Process liveness (load balancers, k8s).                            |
| `GET`  | `/readyz`             |  —   | Process readiness.                                                 |
| `POST` | `/api/auth/token`     |  —   | Mint a short-lived token. Mounted **only with `-dev-tokens`**.     |

---

## JavaScript integration

This is the canonical recipe a third-party web app uses to drive prifa.
The full working version is in [`web/client.js`](web/client.js).

<details open>
<summary><b>1. Get a token</b></summary>

Your backend signs a JWT and ships it to the page (sessionStorage, an
in-memory variable, an `HttpOnly` cookie proxied by your app — whatever
matches your CSRF posture).

```js
const TOKEN = await fetch('/my-app/prifa-token').then(r => r.text());
const PRIFA = 'https://meet.example.com';   // your prifa origin
```

</details>

<details open>
<summary><b>2. Helpers</b></summary>

```js
const authHeaders = (extra = {}) => ({ ...extra, Authorization: 'Bearer ' + TOKEN });

// For EventSource and <video src=…> where the browser cannot add headers.
const withToken = (url) => {
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}token=${encodeURIComponent(TOKEN)}`;
};

async function api(method, path, body) {
  const r = await fetch(PRIFA + path, {
    method,
    headers: authHeaders(body ? { 'Content-Type': 'application/json' } : {}),
    body: body ? JSON.stringify(body) : null,
  });
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${await r.text()}`);
  return r.status === 204 ? null : r.json();
}
```

</details>

<details>
<summary><b>3. Create or join a room</b></summary>

```js
const room   = await api('POST', '/api/rooms', { name: 'Standup' });
const joined = await api('POST', `/api/rooms/${room.id}/participants`, { name: 'Alice' });
const me     = joined.participant.id;
```

</details>

<details>
<summary><b>4. Subscribe to control events (SSE)</b></summary>

```js
const events = new EventSource(
  withToken(`${PRIFA}/api/rooms/${room.id}/events?participant=${me}`)
);

events.addEventListener('hello',              (e) => console.log('hello',  JSON.parse(e.data)));
events.addEventListener('participant.joined', (e) => console.log('joined', JSON.parse(e.data)));
events.addEventListener('participant.left',   (e) => console.log('left',   JSON.parse(e.data)));
events.addEventListener('track.started',      (e) => {
  const ev = JSON.parse(e.data);
  if (ev.data.participant !== me) subscribeRemote(ev.data.participant, ev.data.kind);
});
events.addEventListener('track.ended',        (e) => { /* tear down subscription */ });
```

</details>

<details open>
<summary><b>5. Publish mic and camera (independently)</b></summary>

Each kind is its own streaming POST, so the user can start them, stop
them, and mute them separately.

```js
async function publishKind(kind /* 'audio' | 'video' */) {
  const constraints = kind === 'audio' ? { audio: true } : { video: { width: 640, height: 480 } };
  const mime = kind === 'audio' ? 'audio/webm;codecs=opus' : 'video/webm;codecs=vp8';
  const ms   = await navigator.mediaDevices.getUserMedia(constraints);
  const rec  = new MediaRecorder(ms, { mimeType: mime });

  const { readable, writable } = new TransformStream();
  const writer = writable.getWriter();
  rec.ondataavailable = async (e) => {
    if (e.data?.size) await writer.write(new Uint8Array(await e.data.arrayBuffer()));
  };
  rec.onstop = () => writer.close().catch(() => {});
  rec.start(100);

  fetch(`${PRIFA}/api/rooms/${room.id}/participants/${me}/tracks/${kind}`, {
    method:  'POST',
    headers: authHeaders({ 'Content-Type': mime }),
    body:    readable,
    duplex:  'half',                       // Chrome ≥ 105, recent Firefox
  });

  return { stop: () => { rec.stop(); ms.getTracks().forEach(t => t.stop()); } };
}

const mic    = await publishKind('audio');   // mic-only, no camera
const camera = await publishKind('video');   // camera-only, no mic

// "Mute" mic without touching the camera:
mic.stop();
// "Unmute":
const mic2 = await publishKind('audio');
```

Mute, audio-only, and video-only fall out of this naturally — just
publish (or not) each kind on its own.

</details>

<details>
<summary><b>6. Play a remote participant</b></summary>

```js
async function subscribeRemote(pid, kind) {
  const url  = `${PRIFA}/api/rooms/${room.id}/participants/${pid}/tracks/${kind}?subscriber=${me}`;
  const resp = await fetch(url, { headers: authHeaders() });
  if (!resp.ok) return;
  const mime = resp.headers.get('Content-Type');

  // <video id="video-..."> for video, <audio id="audio-..."> for audio.
  const el = document.getElementById(`${kind}-${pid}`);
  const ms = new MediaSource();
  el.src   = URL.createObjectURL(ms);
  await new Promise(r => ms.addEventListener('sourceopen', r, { once: true }));
  const sb = ms.addSourceBuffer(mime);
  sb.mode = 'sequence';

  const queue = [];
  const drain = () => {
    if (sb.updating || !queue.length || ms.readyState !== 'open') return;
    sb.appendBuffer(queue.shift());
  };
  sb.addEventListener('updateend', drain);

  const reader = resp.body.getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    queue.push(value);
    drain();
  }
}
```

For autoplay handling, drift catch-up, and buffer trimming, see
[`web/client.js`](web/client.js).

</details>

<details>
<summary><b>7. Leave</b></summary>

```js
events.close();
mic?.stop();
camera?.stop();
await api('DELETE', `${PRIFA}/api/rooms/${room.id}/participants/${me}`);
```

</details>

### Browser support

| Feature                                            | Min versions                                                                                   |
| -------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `fetch` streaming POST (`duplex: 'half'`)          | Chrome 105, Edge 105, recent Firefox. Safari: not yet.                                         |
| `MediaSource` + WebM (VP8/VP9 + Opus)              | Chrome, Firefox, Edge. Safari needs MSE-in-Workers for WebM.                                   |
| HTTP/3                                             | Chrome, Edge, Firefox, Safari (all current).                                                   |

If you need Safari publishers, transcode to Annex-B H.264 / fMP4 in the
client (out of scope for this repo).

---

## TLS certificates

HTTP/3 mandates TLS 1.3 — there is no plaintext mode. Browsers refuse to
negotiate QUIC against an untrusted certificate.

<details>
<summary><b>Local development — bundled generator</b></summary>

```bash
go run ./cmd/gencert -hosts localhost,127.0.0.1,::1
```

Then trust the result so the browser accepts it:

- **macOS**
  ```bash
  sudo security add-trusted-cert -d -r trustRoot \
    -k /Library/Keychains/System.keychain certs/cert.pem
  ```
- **Linux (Debian/Ubuntu)**
  ```bash
  sudo cp certs/cert.pem /usr/local/share/ca-certificates/prifa.crt
  sudo update-ca-certificates
  ```
- **Windows** `certutil -addstore -f "ROOT" certs\cert.pem`

</details>

<details>
<summary><b>Local development — mkcert (cleanest)</b></summary>

```bash
brew install mkcert nss      # macOS; equivalents for Linux/Windows exist
mkcert -install
mkcert -cert-file certs/cert.pem -key-file certs/key.pem \
       localhost 127.0.0.1 ::1
```

`mkcert -install` adds a local root CA to your trust store, so HTTP/3
negotiates cleanly with no warnings.

</details>

<details>
<summary><b>Production</b></summary>

Use a real CA-issued certificate (Let's Encrypt, your corporate PKI,
ZeroSSL, …). Point `-cert` / `-key` at the PEM files. Cert reload
requires a process restart — gracefully rolling-deploy with `SIGTERM`
behind your LB.

</details>

---

## Deployment

### Single instance

```bash
PRIFA_JWT_SECRET=$(openssl rand -hex 32) \
PRIFA_LOG_FORMAT=json \
PRIFA_ALLOWED_ORIGINS=https://app.example.com \
./prifa \
  -addr :443 \
  -cert /etc/letsencrypt/live/meet.example.com/fullchain.pem \
  -key  /etc/letsencrypt/live/meet.example.com/privkey.pem
```

Open both **TCP/443** (HTTPS) and **UDP/443** (QUIC) on your firewall.

### Behind a load balancer

QUIC requires a UDP-aware LB (AWS NLB UDP listener, HAProxy ≥ 3.0 with
QUIC support, GCP TCP/UDP LB, …). The same balancer must forward TCP/443
for the HTTPS fallback that advertises `Alt-Svc`.

Because rooms live in process memory, **pin a meeting to one backend** —
either by hashing the room id at the LB, or by having your app server
hand the client the URL of the prifa instance that owns the room.

### Docker

```Dockerfile
FROM golang:1.23 AS build
WORKDIR /src
COPY . .
RUN go build -o /out/prifa . \
 && go build -o /out/gencert ./cmd/gencert

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/prifa /prifa
EXPOSE 8443/tcp 8443/udp
ENTRYPOINT ["/prifa"]
```

```bash
docker run --rm \
  -e PRIFA_JWT_SECRET="$(openssl rand -hex 32)" \
  -e PRIFA_LOG_FORMAT=json \
  -p 8443:8443/tcp -p 8443:8443/udp \
  -v $PWD/certs:/certs \
  prifa -cert /certs/cert.pem -key /certs/key.pem
```

### Observability

- Every response emits an access log line at the level matching its
  status code (info / warn / error). Use `-log-format json` for shippers.
- Each request gets an `X-Request-Id` header used in all log lines —
  clients may set one to thread through their stack.
- `GET /healthz` is a process liveness probe; `GET /readyz` is readiness.

---

## Architecture

```
                ┌─────────────────────────────────────────────────────────────┐
                │                          prifa process                       │
                │                                                              │
   HTTPS/h2 ────┼─►  TCP listener  ──┐                                         │
                │                    │   ┌──────────────────────────────────┐ │
   HTTP/3       │                    ├──►│  api/Handler (mux)               │ │
   (QUIC)  ────┼─►  UDP listener  ──┘   │   • auth middleware (JWT HS256)  │ │
                │                        │   • access-log middleware (slog) │ │
                │                        │   • REST + SSE + streaming media │ │
                │                        └─────────────┬────────────────────┘ │
                │                                      │                       │
                │   ┌──────────────────────────────────▼────────────────────┐ │
                │   │  internal/room                                          │ │
                │   │   Manager → Room → Participant → Track (per kind)       │ │
                │   │   - in-memory, mutex-guarded                            │ │
                │   │   - bounded subscriber channels (drop slow subscribers) │ │
                │   │   - first-chunk init-segment cache for late joiners     │ │
                │   └─────────────────────────────────────────────────────────┘ │
                └──────────────────────────────────────────────────────────────┘
```

<details>
<summary><b>Room model (<code>internal/room</code>)</b></summary>

- `Manager` — owns all rooms, lookup by ID, periodic empty-room sweep.
- `Room` — owns participants and the event fan-out.
- `Participant` — internal session id, external `userId` (JWT subject),
  up to one `audio` and one `video` track.
- `Track` — single-publisher, many-subscriber byte stream with bounded
  channels. The first chunk is retained as the init segment and
  replayed to late subscribers.
- `Event` — JSON envelope broadcast through SSE.

All maps are guarded by `sync.RWMutex`. Slow subscribers lose chunks /
events but never block the room.

</details>

<details>
<summary><b>HTTP API (<code>internal/api</code>)</b></summary>

Go 1.22 method+path routing. Each `/api/*` route is wrapped with the
auth middleware (`internal/auth`). Streaming endpoints read in 64 KiB
chunks and `Flush()` after every write so QUIC streams stay live.

</details>

<details>
<summary><b>Auth (<code>internal/auth</code>)</b></summary>

Pure-stdlib HS256 JWT (no external deps). `Authenticator.Required`
verifies signature, expiry, optional iss/aud, and attaches the parsed
claims to the request context. Handlers consult `claims.AllowsRoom(id)`
and `claims.HasScope(...)` for fine-grained checks.

</details>

<details>
<summary><b>Server (<code>internal/server</code>)</b></summary>

A wrapper around `http3.Server` and `http.Server`. Loads the cert once,
configures TLS 1.3 with `h3` / `h2` / `http/1.1` ALPN, and tears both
listeners down on context cancellation. The HTTPS handler is wrapped
to emit `Alt-Svc: h3=":<port>"; ma=...` so browsers upgrade follow-up
requests to HTTP/3.

</details>

---

## Limitations

- **In-memory rooms.** Restarting the binary drops every call. A
  multi-host deployment must hash a meeting onto a single instance.
  Persistence and clustering are out of scope.
- **No RTP / no SFU.** prifa forwards opaque bytes. No codec parsing,
  no per-subscriber pacing, no simulcast. Sufficient for low-latency
  fan-out of `MediaRecorder` output; if you need 1k-viewer broadcast,
  use a full SFU.
- **HS256 only.** RS256 / ES256 aren't implemented; they're ~100 lines
  away with `crypto/rsa` or `crypto/ecdsa` if you need asymmetric
  signing (e.g. an external IdP).
- **No rate limiting built in.** Front it with your own LB / API
  gateway if you need quotas.

---

## Roadmap

- [ ] RS256 / ES256 JWT support
- [ ] Optional Redis-backed room store for clustering
- [ ] WHIP/WHEP-style endpoints alongside the native streaming API
- [ ] WebTransport client for ultra-low-latency publishers
- [ ] Per-token rate limit + per-room quotas
- [ ] Recording sink (write each track to disk / S3)

PRs against any of these are welcome.

---

## Contributing

```bash
go test ./...      # unit tests (covers the JWT codec)
go vet ./...       # static analysis
go build ./...     # all binaries
```

Open an issue or send a PR. Keep changes focused — one feature or fix
per PR. Match the existing style: plain stdlib, no premature abstraction,
structured logs over `fmt.Printf`.

---

## License

Add a `LICENSE` file at the project root before publishing. MIT is a fair
default for a video-call relay; pick what fits your needs.
