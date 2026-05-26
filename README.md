# prifa — private, fast video call server

A self-hosted, JWT-authenticated, HTTP/3 video-call backend written in Go.
Clone it, point a domain at it, and you have a private video-call service
that any front-end (web, mobile, server-to-server) can drive over a small
REST + streaming API.

```
  Browser / mobile / backend ──►  prifa (HTTP/3 + HTTPS)
                                    │
                                    ├── REST     /api/rooms, /api/.../participants
                                    ├── SSE      /api/rooms/{id}/events
                                    └── Streaming media  POST / GET /tracks/{kind}
```

Designed to be embedded as the media plane behind your own SaaS: your
application service mints a short-lived JWT, hands it to the client, and
prifa enforces room/scope/expiry on every call.

---

## Why prifa

- **Private by default.** No third-party media relay, no analytics
  pipeline. Your traffic terminates at a binary you run.
- **Fast by default.** HTTP/3 (QUIC) for media and control. No
  TCP head-of-line blocking; one lost packet does not freeze every track.
- **Drop-in for your SaaS.** JWT bearer auth on every endpoint. Your
  existing auth service signs HS256 tokens; prifa validates them. No
  user database lives inside prifa.
- **Operable.** Structured `log/slog` access logs with request IDs,
  `/healthz` and `/readyz` probes, graceful shutdown, JSON or text logs.
- **Small surface.** One binary, in-memory rooms, no external services
  required. Stick it behind a load balancer or run a single instance.

> Media note: prifa forwards opaque media bytes (e.g. fragmented WebM the
> browser produces with `MediaRecorder`); it does not parse RTP, do
> congestion control, or transcode. It is a real-time bytestream relay,
> not a full SFU. See *Limitations* at the bottom.

---

## Layout

```
.
├── main.go                       # entrypoint; loads config, builds handler
├── cmd/
│   ├── gencert/main.go           # self-signed cert generator
│   └── token/main.go             # offline JWT minter
├── internal/
│   ├── api/                      # HTTP handlers (REST, SSE, streaming media)
│   ├── auth/                     # HS256 JWT sign/verify + middleware
│   ├── config/                   # flags + env loader
│   ├── logx/                     # slog setup + access log middleware
│   ├── room/                     # in-memory rooms, participants, tracks
│   └── server/                   # HTTP/3 + HTTPS bootstrap
├── web/                          # demo browser client (vanilla JS)
└── certs/                        # generated cert + key (gitignored)
```

---

## Quick start

```bash
# 1. Generate a self-signed cert (or use mkcert / your own)
go run ./cmd/gencert -hosts localhost,127.0.0.1,::1

# 2. Pick a JWT signing secret (32+ random bytes recommended)
export PRIFA_JWT_SECRET=$(openssl rand -hex 32)

# 3. Run the server — HTTP/3 + HTTPS on :8443
go run . -addr :8443 \
         -cert certs/cert.pem -key certs/key.pem \
         -dev-tokens                    # exposes /api/auth/token for local dev
```

Open <https://localhost:8443/> in two browser windows, accept the cert,
click *Get dev token*, then *Join / Create*, then *Start camera + mic*.

For production, omit `-dev-tokens` and mint tokens from your own backend.

---

## Configuration

Every flag has an env equivalent. Env wins when set.

| Flag | Env | Default | Description |
| --- | --- | --- | --- |
| `-addr` | `PRIFA_ADDR` | `:8443` | Listen address (used for both TCP/HTTPS and UDP/QUIC). |
| `-cert` | `PRIFA_CERT` | `certs/cert.pem` | TLS certificate. |
| `-key`  | `PRIFA_KEY`  | `certs/key.pem`  | TLS private key. |
| `-web`  | `PRIFA_WEB_DIR` | `web` | Static client directory; empty to disable. |
| `-no-https` | `PRIFA_NO_HTTPS` | `false` | Disable TCP/HTTPS (HTTP/3 only). |
| `-jwt-secret` | `PRIFA_JWT_SECRET` | *(none)* | **Required.** HS256 secret. Prefer the env variable. |
| `-jwt-issuer` | `PRIFA_JWT_ISSUER` | *(none)* | If set, tokens must carry this `iss` claim. |
| `-jwt-audience` | `PRIFA_JWT_AUDIENCE` | *(none)* | If set, tokens must carry this `aud` claim. |
| `-auth-optional` | `PRIFA_AUTH_OPTIONAL` | `false` | Accept anonymous requests. **Dev only.** |
| `-dev-tokens` | `PRIFA_DEV_TOKENS` | `false` | Mount `POST /api/auth/token`. **Dev only.** |
| `-dev-token-ttl` | `PRIFA_DEV_TOKEN_TTL` | `1h` | Default TTL for dev-minted tokens. |
| `-allowed-origins` | `PRIFA_ALLOWED_ORIGINS` | *(reflect any)* | Comma-separated CORS allowlist. |
| `-log-level` | `PRIFA_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |
| `-log-format` | `PRIFA_LOG_FORMAT` | `text` | `text` or `json`. Use `json` in production. |

The server refuses to start if `PRIFA_JWT_SECRET` is missing, unless you
opt in to `-auth-optional`.

---

## JWT authentication

Every `/api/*` endpoint requires a bearer token signed HS256 with your
`PRIFA_JWT_SECRET`. There is no built-in user database — your application
backend is the trust root.

### Token format

```jsonc
{
  "sub":   "user-42",                            // your external user id (-> Participant.UserID)
  "name":  "Alice",                              // display name (overrides request body)
  "room":  "abc123",                             // optional: bind token to one room
  "scope": ["track.publish","track.subscribe"],  // optional: empty = full access
  "iss":   "your-app",                           // optional, only checked if server has -jwt-issuer
  "aud":   "prifa",                              // optional, only checked if server has -jwt-audience
  "iat":   1717000000,
  "exp":   1717003600                            // short-lived; minutes-to-hours is typical
}
```

Scopes recognised by the server (a token without scopes has full access):

| Scope | Allows |
| --- | --- |
| `room.create` | `POST /api/rooms` |
| `room.list`   | `GET /api/rooms` |
| `room.join`   | `POST /api/rooms/{id}/participants` |
| `track.publish`   | `POST /api/rooms/{id}/participants/{pid}/tracks/{kind}` |
| `track.subscribe` | `GET  /api/rooms/{id}/participants/{pid}/tracks/{kind}` |

When `room` is set, the token only works against that room id. Use this
to issue per-meeting tokens from your backend.

### How to attach the token

| Surface | Mechanism |
| --- | --- |
| REST + streaming `fetch` | `Authorization: Bearer <jwt>` |
| Native `EventSource` (SSE) | append `?token=<jwt>` (EventSource cannot carry custom headers) |
| `<video src=…>` with token | append `?token=<jwt>` |

### Minting tokens

**Locally for testing**

```bash
go run ./cmd/token \
    -secret "$PRIFA_JWT_SECRET" \
    -sub user-42 -name Alice \
    -room abc123 \
    -scope track.publish,track.subscribe \
    -ttl 1h
```

**From your backend (Node.js, jsonwebtoken)**

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

**From your backend (Python, PyJWT)**

```python
import jwt, time
token = jwt.encode({
  "sub":   user.id,
  "name":  user.display_name,
  "room":  room_id,
  "scope": ["room.join","track.publish","track.subscribe"],
  "iat":   int(time.time()),
  "exp":   int(time.time()) + 3600,
}, os.environ["PRIFA_JWT_SECRET"], algorithm="HS256")
```

**For local development**, run the server with `-dev-tokens` and call
`POST /api/auth/token` (no auth required) with the body
`{"sub":"...","name":"...","room":"...","scope":[],"ttlSeconds":3600}`.

---

## REST + streaming API

All endpoints below require a valid bearer token unless noted. Bodies are
JSON. Path parameters: `{roomID}` is a 12-hex-char id returned by
`POST /api/rooms`; `{pid}` is the 16-hex-char participant id returned by
`POST /api/rooms/{roomID}/participants`; `{kind}` is `audio` or `video`.

### Rooms

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| `POST` | `/api/rooms` | `{ "name": "string?" }` | `{ id, name, createdAt }` |
| `GET`  | `/api/rooms` | — | `{ rooms: [...] }` (filtered to the token's room when room-bound) |
| `GET`  | `/api/rooms/{roomID}` | — | room detail incl. participants and active tracks |

### Participants

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| `POST`   | `/api/rooms/{roomID}/participants` | `{ "name": "string?" }` | `{ room, participant: { id, userId, name, joinedAt } }` |
| `DELETE` | `/api/rooms/{roomID}/participants/{pid}` | — | `204` |

The JWT `sub` claim becomes `participant.userId` so your application can
join the session back to its own user record.

### Event stream (Server-Sent Events)

```
GET /api/rooms/{roomID}/events?participant={pid}
```

Pass the token via `Authorization` (if you can) or `?token=<jwt>`. First
event is always `hello` with the current participant list. Subsequent
events: `participant.joined`, `participant.left`, `track.started`,
`track.ended`. The stream stays open until the participant leaves or the
request is cancelled.

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
- both (publish each as its own streaming POST in parallel),
- start one, stop, restart, leave the other untouched ("mute" = stop the
  audio POST; "unmute" = start a new one).

Each kind emits its own `track.started` / `track.ended` event so peers
can attach / detach a single stream without disturbing the other. The
body stays open for as long as the publisher is sending. Every chunk
is fanned out to subscribers.

### Subscribe to media

```
GET /api/rooms/{roomID}/participants/{pid}/tracks/{kind}?subscriber=any-id-you-like
Authorization: Bearer <jwt>
```

Returns 200 with the publisher's `Content-Type` and a streaming body that
mirrors what the publisher uploaded. The first chunk a publisher sent is
cached and replayed to late subscribers (so WebM init segments don't
get lost). Returns 404 if no track of that kind is currently active.

### Health

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| `GET` | `/healthz` | no | Process is alive. |
| `GET` | `/readyz`  | no | Process is ready to serve. |

### Dev-only

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| `POST` | `/api/auth/token` | no | Mint a short-lived token. Mounted only with `-dev-tokens`. |

---

## JavaScript / browser usage

This is the canonical recipe a third-party web app uses to drive prifa.
The full working version lives in [`web/client.js`](web/client.js).

### 1. Get a token

Your backend signs a JWT (see *JWT authentication* above) and ships it to
the page. Store it however you want — `sessionStorage`, an in-memory
variable, a `HttpOnly` cookie that your app proxies, anything that
matches your CSRF posture.

```js
const TOKEN = await fetch('/my-app/prifa-token').then(r => r.text());
const PRIFA = 'https://meet.example.com';   // your prifa origin
```

### 2. Helpers

```js
const authHeaders = (extra = {}) => ({ ...extra, Authorization: 'Bearer ' + TOKEN });

// For EventSource / <video src=...> (no custom headers possible).
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

### 3. Create or join a room

```js
// Either create a new room, or skip this step if your backend already did.
const room = await api('POST', '/api/rooms', { name: "Standup" });

// Join — returns the per-session participant id (used in every later call).
const joined = await api('POST', `/api/rooms/${room.id}/participants`, { name: 'Alice' });
const me     = joined.participant.id;
```

### 4. Subscribe to control events (SSE)

```js
const events = new EventSource(withToken(`${PRIFA}/api/rooms/${room.id}/events?participant=${me}`));

events.addEventListener('hello',              (e) => console.log('hello',  JSON.parse(e.data)));
events.addEventListener('participant.joined', (e) => console.log('joined', JSON.parse(e.data)));
events.addEventListener('participant.left',   (e) => console.log('left',   JSON.parse(e.data)));
events.addEventListener('track.started',      (e) => {
  const ev = JSON.parse(e.data);
  if (ev.data.participant !== me) subscribeRemote(ev.data.participant, ev.data.kind);
});
events.addEventListener('track.ended',        (e) => { /* tear down subscription */ });
```

### 5. Publish mic and camera (independently)

Each kind is its own streaming POST, so the user can start them, stop
them, and mute them separately. The general recipe (see `web/client.js`
for the full implementation, including a toggle that stops/restarts the
upload on mute/unmute):

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

  // Returned handle lets you mute/leave: stop the recorder + the local
  // tracks, and the server emits track.ended automatically.
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

### 6. Play a remote participant

Each `track.started` event tells you what kind became available. Attach
the streaming GET into a `<video>` (for `video`) or `<audio>` (for
`audio`) element via `MediaSource`. The skeleton:

```js
async function subscribeRemote(pid, kind) {
  const url  = `${PRIFA}/api/rooms/${room.id}/participants/${pid}/tracks/${kind}?subscriber=${me}`;
  const resp = await fetch(url, { headers: authHeaders() });
  if (!resp.ok) return;
  const mime = resp.headers.get('Content-Type');

  const el = document.getElementById(`${kind}-${pid}`); // <video id="video-..."> or <audio id="audio-...">
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

// Wire it to the event stream:
events.addEventListener('track.started', (e) => {
  const { participant, kind } = JSON.parse(e.data).data;
  if (participant !== me) subscribeRemote(participant, kind);
});
events.addEventListener('track.ended', (e) => {
  // tear down just this kind; the other one keeps playing
});
```

For a production-quality version with autoplay handling, drift catch-up,
and buffer trimming, see [`web/client.js`](web/client.js).

### 7. Leave

```js
events.close();
rec.stop();
await api('DELETE', `${PRIFA}/api/rooms/${room.id}/participants/${me}`);
```

### Browser support cheat-sheet

| Feature | Min versions |
| --- | --- |
| `fetch` streaming POST (`duplex: 'half'`) | Chrome 105, Edge 105, recent Firefox (behind `dom.fetch.requestBody.upload.streams.enabled` until lately). Safari: not yet. |
| `MediaSource` + WebM (VP8/VP9 + Opus) | Chrome, Firefox, Edge. Safari needs MSE-in-Workers for WebM. |
| HTTP/3 | Chrome, Edge, Firefox, Safari, all current. |

If you need Safari publishers, you can transcode to Annex-B H.264 / fMP4
in the client (out of scope for this README).

---

## TLS certificates

HTTP/3 mandates TLS 1.3 — there is no plaintext mode. Browsers also
refuse to negotiate QUIC against a certificate they do not trust.

### Local development

```bash
# bundled generator, ECDSA P-256, no extra tools
go run ./cmd/gencert -hosts localhost,127.0.0.1,::1
```

Then either trust it (recommended):

- **macOS:**
  ```bash
  sudo security add-trusted-cert -d -r trustRoot \
    -k /Library/Keychains/System.keychain certs/cert.pem
  ```
- **Linux (Debian/Ubuntu):**
  ```bash
  sudo cp certs/cert.pem /usr/local/share/ca-certificates/prifa.crt
  sudo update-ca-certificates
  ```
- **Windows:** `certutil -addstore -f "ROOT" certs\cert.pem`.

Or use [`mkcert`](https://github.com/FiloSottile/mkcert) which manages a
local root for you:

```bash
mkcert -install
mkcert -cert-file certs/cert.pem -key-file certs/key.pem localhost 127.0.0.1 ::1
```

### Production

Use a real CA-issued certificate (Let's Encrypt, your corporate PKI,
ZeroSSL, etc.). Point `-cert` / `-key` (or `PRIFA_CERT` / `PRIFA_KEY`)
at the PEM files. Reload requires a process restart — gracefully
re-deploy with `SIGTERM` and a rolling instance behind your LB.

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

Open both TCP/443 (HTTPS) and UDP/443 (QUIC) on your firewall.

### Behind a load balancer

QUIC requires a UDP-aware load balancer (e.g. AWS NLB UDP listener,
HAProxy ≥ 3.0 with QUIC support, GCP TCP/UDP LB). The same balancer
must forward TCP/443 for the HTTPS fallback that advertises Alt-Svc.

Because rooms live in process memory (see *Limitations*), pin a meeting
to a single backend — either by hashing the room id at the LB, or by
having your app server hand the client the URL of the prifa instance
that owns the room.

### Container

```Dockerfile
FROM golang:1.23 AS build
WORKDIR /src
COPY . .
RUN go build -o /out/prifa . && go build -o /out/gencert ./cmd/gencert

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/prifa /prifa
EXPOSE 8443/tcp 8443/udp
ENTRYPOINT ["/prifa"]
```

Run with:

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
- Each request gets an `X-Request-Id` header, used in all log lines so
  you can correlate. Clients may set one to thread through their stack.
- `GET /healthz` is a process liveness probe; `GET /readyz` is readiness.

---

## Internals

### Room model (`internal/room`)

- `Manager` — owns all rooms, lookup by ID, periodic empty-room sweep.
- `Room` — owns participants and the event fan-out.
- `Participant` — has an internal session id, an external `userId`
  (from the JWT subject), and up to one `audio` and one `video` track.
- `Track` — single-publisher / many-subscriber byte stream with bounded
  channels. The first chunk is retained as the "init segment" and
  replayed to late subscribers.
- `Event` — JSON envelope broadcast through SSE.

All maps are guarded by `sync.RWMutex`. Slow subscribers lose chunks /
events but never block the room.

### HTTP API (`internal/api`)

Go 1.22 method+path routing. Each `/api/*` route is wrapped with the
auth middleware (`internal/auth`). Streaming endpoints read in 64 KiB
chunks and `Flush()` after every write so QUIC streams stay live.

### Auth (`internal/auth`)

Pure-stdlib HS256 JWT (no external deps). `Authenticator.Required`
verifies signature, expiry, optional iss/aud, and attaches the parsed
claims to the request context. Handlers consult `claims.AllowsRoom(id)`
and `claims.HasScope(...)` for fine-grained checks.

### Server (`internal/server`)

A wrapper around `http3.Server` and `http.Server`. Loads the cert once,
configures TLS 1.3 with `h3` / `h2` / `http/1.1` ALPN, and tears both
listeners down on context cancellation. The HTTPS handler is wrapped to
emit `Alt-Svc: h3=":<port>"; ma=...` so browsers upgrade follow-up
requests to HTTP/3.

---

## Limitations

- **In-memory rooms.** Restarting the binary drops every call. A
  multi-host deployment must hash a meeting onto a single instance.
  Persistence and clustering are out of scope.
- **No RTP / no SFU.** prifa forwards opaque bytes. No codec parsing,
  no per-subscriber pacing, no simulcast. Sufficient for low-latency
  fan-out of `MediaRecorder` output; if you need 1k-viewer broadcast,
  use an SFU.
- **Late-joiner init segments.** Covered automatically for the common
  fragmented-WebM case (`Track` caches and replays the first chunk),
  but other container formats may need extra logic.
- **HS256 only.** RS256 / ES256 are not implemented; they are 100 lines
  away with `crypto/rsa` or `crypto/ecdsa` if you need asymmetric
  signing (e.g. an external IdP).
- **No rate limiting / per-room quotas built in.** Front it with your
  own LB / API gateway if you need them.

---

## Development tips

- `go test ./...` runs the unit tests (currently covering the JWT codec).
- Tail the server's stdout for structured logs; pipe through `jq` if you
  use `-log-format json`.
- In Chrome, `chrome://net-export/` → `chrome://net-internals/#events`
  is the canonical way to confirm a request went over h3.
- `nghttp3` / `quiche-client` are useful for testing the server without
  a browser.
