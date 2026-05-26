# prifa — HTTP/3 video call server

A small, opinionated video-call server written in Go on top of
[`quic-go`](https://github.com/quic-go/quic-go). Rooms and participants are
held in process memory; audio and video are forwarded as opaque byte streams
through HTTP/3 (QUIC). The server ships with a demo browser client and a
self-signed certificate generator.

> **Scope.** This is a learning / demo project, not an SFU. It does no
> codec parsing, RTP, congestion control, or DTLS/SRTP. Media is treated as
> a fragmented WebM byte-stream that the publisher uploads with a streaming
> POST and that subscribers read back with a streaming GET. Late joiners
> may miss the WebM init segment (see *Known limitations* below).

---

## Features

- **REST** for room lifecycle, join, and leave (in-memory store — no DB).
- **HTTP/3 (QUIC)** transport using `quic-go/http3`, plus a parallel HTTPS
  listener that advertises the QUIC endpoint via `Alt-Svc`.
- **Server-Sent Events** stream per participant for room events
  (`participant.joined`, `participant.left`, `track.started`, `track.ended`).
- **Streaming media forwarding**: each track is a single-publisher,
  many-subscriber fan-out backed by buffered Go channels (slow subscribers
  get chunks dropped, never block the publisher).
- **Demo browser client** under `web/` using `getUserMedia`,
  `MediaRecorder`, `fetch` streaming uploads, and `MediaSource` playback.
- **Built-in TLS cert generator** at `cmd/gencert` so you can run without
  installing OpenSSL.

---

## Layout

```
.
├── main.go                       # CLI entrypoint
├── cmd/gencert/main.go           # self-signed cert generator
├── internal/
│   ├── api/                      # HTTP handlers (REST, SSE, media)
│   ├── room/                     # in-memory rooms, participants, tracks
│   └── server/                   # HTTP/3 + HTTPS bootstrap
├── web/                          # demo browser client
│   ├── index.html
│   └── client.js
└── certs/                        # generated cert + key (gitignored)
```

---

## Quick start

```bash
# 1. Fetch dependencies
go mod tidy

# 2. Generate a self-signed cert valid for localhost
go run ./cmd/gencert -hosts localhost,127.0.0.1,::1

# 3. Run the server (HTTPS + HTTP/3 on :8443)
go run . -addr :8443 -cert certs/cert.pem -key certs/key.pem
```

Then open <https://localhost:8443/> in two browser windows, accept the cert
warning the first time, join the same room ID, and click **Start camera +
mic** in each window.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-addr` | `:8443` | Address to listen on (used for both UDP and TCP). |
| `-cert` | `certs/cert.pem` | TLS certificate path. |
| `-key`  | `certs/key.pem` | TLS private key path. |
| `-web`  | `web` | Static client directory. Set empty to disable. |
| `-no-https` | `false` | Disable the TCP HTTPS listener (HTTP/3 only). |

---

## TLS certificates

HTTP/3 mandates TLS 1.3 — there is no plaintext mode. Browsers also refuse
to negotiate QUIC against a certificate they do not trust, so you have two
choices for local development.

### Option A — built-in generator (no extra tools)

```bash
go run ./cmd/gencert -hosts localhost,127.0.0.1,::1
```

This writes `certs/cert.pem` and `certs/key.pem`. The resulting cert is
**self-signed** and Chrome / Firefox will refuse the initial connection.
You have three escape hatches:

1. **Trust the cert manually** (recommended).
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
     For Chromium-based browsers on Linux, also import via
     `chrome://settings/certificates` → *Authorities*.
   - **Windows:** `certutil -addstore -f "ROOT" certs\cert.pem`.
2. **Run Chrome with the cert pinned** (no global trust change):
   ```bash
   # Get the SPKI fingerprint of the cert
   openssl x509 -in certs/cert.pem -pubkey -noout \
     | openssl pkey -pubin -outform der \
     | openssl dgst -sha256 -binary | base64

   # Launch Chrome with that fingerprint allow-listed
   google-chrome \
     --user-data-dir=/tmp/prifa-chrome \
     --ignore-certificate-errors-spki-list=<PASTE_FINGERPRINT> \
     --origin-to-force-quic-on=localhost:8443 \
     https://localhost:8443/
   ```
3. **Bypass for the session only** (Chrome `--ignore-certificate-errors`)
   — works but disables certificate checking for the whole session, so use
   a throwaway profile.

### Option B — `mkcert` (cleanest if you already have it)

```bash
brew install mkcert nss      # macOS; equivalents for Linux/Windows exist
mkcert -install
mkcert -cert-file certs/cert.pem -key-file certs/key.pem \
  localhost 127.0.0.1 ::1
```

`mkcert -install` adds a local root CA to your trust store, so the browser
will accept the cert with no warnings and HTTP/3 will negotiate cleanly.

### Option C — `openssl`

```bash
mkdir -p certs
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
  -keyout certs/key.pem -out certs/cert.pem \
  -sha256 -days 365 -nodes \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1"
```

Then trust the cert as in Option A.

---

## REST + streaming API

All endpoints are mounted under `/api`. Response bodies are JSON unless
otherwise noted. CORS is permissive by default — restrict it before
exposing this anywhere public.

### Room lifecycle

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| `POST` | `/api/rooms` | `{ "name": "string?" }` | `{ id, name, createdAt }` |
| `GET`  | `/api/rooms` | — | `{ rooms: [...] }` |
| `GET`  | `/api/rooms/{roomID}` | — | room detail incl. participants and active tracks |

### Participant lifecycle

| Method | Path | Body | Returns |
| --- | --- | --- | --- |
| `POST`   | `/api/rooms/{roomID}/participants` | `{ "name": "string?" }` | `{ room, participant: { id, name, joinedAt } }` |
| `DELETE` | `/api/rooms/{roomID}/participants/{pid}` | — | `204` |

> The participant `id` returned from join is the credential for every
> subsequent operation — there is no separate token. This is a demo, not
> a production access model.

### Event stream

```
GET /api/rooms/{roomID}/events?participant={pid}
```

Server-Sent Events. The first event is always `hello` with the current
participant list. Subsequent events:

- `participant.joined` — `{ id, name, joinedAt }`
- `participant.left` — `{ id, name }`
- `track.started` — `{ participant, kind, contentType }`
- `track.ended` — `{ participant, kind }`

The stream stays open for the life of the participant; closing the request
(or calling `DELETE …/participants/{pid}`) ends it.

### Media publish

```
POST /api/rooms/{roomID}/participants/{pid}/tracks/{kind}
Content-Type: video/webm;codecs=vp8,opus     (or whatever you want to fan out)
<streaming body>
```

`kind` is `audio` or `video`. The request body stays open for as long as
the publisher is sending; the server fans every chunk out to subscribers.
When the body ends (or the connection drops) the track is closed and a
`track.ended` event is fired.

The first 200 response carries `{"status":"streaming"}` so clients can
confirm the track is live.

### Media subscribe

```
GET /api/rooms/{roomID}/participants/{pid}/tracks/{kind}?subscriber={anything}
```

Returns `200` with the publisher's `Content-Type` and a streaming body that
mirrors what the publisher uploaded. The response stays open until the
publisher disconnects or the subscriber cancels. Returns `404` if no track
of that kind is currently active.

---

## How HTTP/3 is wired up

`internal/server` runs two listeners on the same `host:port`:

1. **UDP / QUIC** via `http3.Server.ListenAndServe()`.
2. **TCP / TLS** via `http.Server.ListenAndServeTLS()`. Every HTTPS
   response is wrapped to include the `Alt-Svc: h3=":<port>"; ma=…` header
   produced by `http3.Server.SetQUICHeaders`. That header is how Chrome,
   Firefox, and Safari discover the QUIC endpoint and upgrade subsequent
   navigations to HTTP/3.

The browser will load the page the first time over HTTPS/h2 and then
silently switch to h3 for the API calls. The little protocol pill in the
demo page shows what the browser ended up using.

If you only want HTTP/3 (e.g., testing with a raw HTTP/3 client), pass
`-no-https` to skip the TCP listener.

---

## Internals

### Room model (`internal/room`)

- `Manager` — owns all rooms, lookup by ID.
- `Room` — owns participants and the event fan-out for that room.
- `Participant` — owns up to one `audio` and one `video` `Track`.
- `Track` — single-publisher, many-subscriber byte stream with bounded
  per-subscriber channels (drops on overflow so slow subscribers do not
  back-pressure the publisher).
- `Event` — JSON envelope broadcast through SSE.

All maps are guarded by `sync.RWMutex`. Slow subscribers lose chunks /
events but never block the room.

### HTTP API (`internal/api`)

Uses Go 1.22 method+path routing (`POST /api/rooms`, etc.). Each handler is
a thin adapter between HTTP and the room model. Streaming endpoints read
the body in 64 KiB chunks and `Flush()` writes immediately so QUIC streams
stay live.

### Server bootstrap (`internal/server`)

A small wrapper around `http3.Server` and `http.Server` that loads the cert
once, configures TLS 1.3 with `h3` / `h2` / `http/1.1` ALPN, and tears
both listeners down on `Run(ctx)` cancellation.

---

## Known limitations

- **Init-segment problem.** `MediaRecorder` emits a WebM file whose
  EBML/init segment is in the first chunk only. A subscriber that joins
  *after* the publisher started will not see that segment and cannot
  decode the stream. Production SFUs solve this by re-parsing the
  container — out of scope here. For the demo, start your camera *after*
  the other peer has joined.
- **No congestion or pacing across subscribers.** A subscriber that cannot
  keep up will see chunks dropped at the channel boundary, which can
  desync audio/video.
- **No authentication / authorization.** The participant ID is the only
  credential. Fine for a demo; do not expose this server to the public
  Internet as-is.
- **Single process, single host.** Rooms live in RAM. Restarting the
  binary drops every call. No clustering.
- **Browser-only client.** Native HTTP/3 clients (e.g., `curl --http3`)
  can drive every endpoint, but the media format expected by the browser
  demo is fragmented WebM.

---

## Development tips

- Tail the server's stdout for routing logs.
- In Chrome, `chrome://net-export/` then `chrome://net-internals/#events`
  is the canonical way to confirm that traffic actually went over h3.
- `nghttp3` and `quiche-client` are good for testing the server without a
  browser.
- `go test ./...` — there are no tests yet; PRs welcome.
