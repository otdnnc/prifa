// prifa browser client.
//
// Protocol summary (every call carries the JWT bearer token):
//   - REST for room CRUD + join/leave        Authorization: Bearer <jwt>
//   - SSE for room events                    ?token=<jwt> (EventSource limit)
//   - Streaming POST for publishing media    Authorization header
//   - Streaming GET into MediaSource          Authorization header
//
// Audio and video are independent tracks. Each is its own MediaRecorder,
// its own streaming POST, and its own remote MediaSource. The user can
// toggle them independently (audio-only, video-only, mute, unmute, etc).
//
// When the page was served over HTTPS with Alt-Svc the browser silently
// upgrades these calls to HTTP/3.

const $ = (id) => document.getElementById(id);
const TOKEN_KEY = 'prifa.token';

const log = (msg) => {
  const el = $('log');
  el.textContent += new Date().toISOString().slice(11, 19) + ' ' + msg + '\n';
  el.scrollTop = el.scrollHeight;
};

const state = {
  token: localStorage.getItem(TOKEN_KEY) || '',
  roomId: null,
  participantId: null,
  events: null,
  // per-kind publish state. Set when the local kind is live.
  publish: {
    audio: null, // { recorder, writer, mediaStream, ctl }
    video: null,
  },
  remotes: new Map(), // `${pid}:${kind}` -> { ctl, ms, srcUrl }
};

if (state.token) $('token').value = state.token;
$('token').addEventListener('input', () => {
  state.token = $('token').value.trim();
  localStorage.setItem(TOKEN_KEY, state.token);
});

function authHeaders(extra) {
  const h = { ...(extra || {}) };
  if (state.token) h['Authorization'] = 'Bearer ' + state.token;
  return h;
}

// withToken appends ?token= to a URL for endpoints the browser can't add
// an Authorization header to (notably EventSource, and <video src=...>).
function withToken(url) {
  if (!state.token) return url;
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}token=${encodeURIComponent(state.token)}`;
}

async function api(method, path, body) {
  const r = await fetch(path, {
    method,
    headers: authHeaders(body ? { 'Content-Type': 'application/json' } : {}),
    body: body ? JSON.stringify(body) : null,
  });
  if (!r.ok) {
    const text = await r.text();
    throw new Error(`${method} ${path}: ${r.status} ${text}`);
  }
  if (r.status === 204) return null;
  return r.json();
}

(function showProtocol() {
  const entries = performance.getEntriesByType('navigation');
  const proto = entries[0]?.nextHopProtocol ?? '?';
  $('protocol').textContent = 'protocol: ' + proto;
})();

$('devToken').onclick = async () => {
  try {
    const name = $('name').value.trim() || 'guest';
    const r = await fetch('/api/auth/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sub: name, name, ttlSeconds: 3600 }),
    });
    if (!r.ok) throw new Error(`HTTP ${r.status} — server probably wasn't started with -dev-tokens`);
    const json = await r.json();
    state.token = json.token;
    $('token').value = json.token;
    localStorage.setItem(TOKEN_KEY, state.token);
  } catch (err) {
    alert('dev token failed: ' + err.message);
  }
};

$('go').onclick = async () => {
  try {
    if (!state.token) {
      alert('paste a JWT token first (or use Get dev token if the server allows it)');
      return;
    }
    const name = $('name').value.trim() || 'guest';
    let roomId = $('roomId').value.trim();
    if (!roomId) {
      const room = await api('POST', '/api/rooms', { name: `${name}'s room` });
      roomId = room.id;
      log(`created room ${roomId}`);
    }
    const joined = await api('POST', `/api/rooms/${roomId}/participants`, { name });
    state.roomId = roomId;
    state.participantId = joined.participant.id;
    $('rid').textContent = roomId;
    $('me').textContent = `${joined.participant.name} (${state.participantId})`;
    $('join').hidden = true;
    $('call').hidden = false;
    openEventStream();
  } catch (err) {
    alert(err.message);
  }
};

// EventSource can't carry custom headers, so we pass the token as a query
// parameter. The server accepts ?token=<jwt> as an alternative.
function openEventStream() {
  const url = withToken(`/api/rooms/${state.roomId}/events?participant=${state.participantId}`);
  const es = new EventSource(url);
  state.events = es;
  for (const type of ['hello', 'participant.joined', 'participant.left', 'track.started', 'track.ended']) {
    es.addEventListener(type, (e) => onEvent(type, JSON.parse(e.data)));
  }
  es.onerror = () => log('event stream error (will auto-retry)');
}

function onEvent(type, ev) {
  log(`${type} ${JSON.stringify(ev.data ?? {})}`);
  const data = ev.data || {};
  switch (type) {
    case 'hello':
      for (const p of data.participants || []) {
        if (p.id === state.participantId) continue;
        addRemoteTile(p.id, p.name);
        for (const k of p.activeTracks || []) subscribeRemote(p.id, k);
      }
      break;
    case 'participant.joined':
      if (data.id !== state.participantId) addRemoteTile(data.id, data.name);
      break;
    case 'participant.left':
      removeRemoteTile(data.id);
      break;
    case 'track.started':
      if (data.participant !== state.participantId) subscribeRemote(data.participant, data.kind);
      break;
    case 'track.ended':
      stopRemoteSubscription(data.participant, data.kind);
      break;
  }
}

function addRemoteTile(pid, name) {
  if ($(`tile-${pid}`)) return;
  const div = document.createElement('div');
  div.className = 'tile';
  div.id = `tile-${pid}`;
  // One <video> for the video track, one <audio> for the audio track.
  // Each gets its own MediaSource attached as kind goes live.
  div.innerHTML = `
    <b>${name} <small>${pid}</small></b>
    <video id="video-${pid}" autoplay playsinline></video>
    <audio id="audio-${pid}" autoplay></audio>
  `;
  $('tiles').appendChild(div);
}

function removeRemoteTile(pid) {
  $(`tile-${pid}`)?.remove();
  for (const kind of ['audio', 'video']) stopRemoteSubscription(pid, kind);
}

$('leave').onclick = async () => {
  try {
    state.events?.close();
    await stopLocal('audio');
    await stopLocal('video');
    await api('DELETE', `/api/rooms/${state.roomId}/participants/${state.participantId}`);
  } catch (_) { /* ignore */ }
  location.reload();
};

// --- Local publishing (audio/video independent) ---------------------------

$('toggleMic').onclick = () => toggleKind('audio', $('toggleMic'));
$('toggleCam').onclick = () => toggleKind('video', $('toggleCam'));

async function toggleKind(kind, btn) {
  btn.disabled = true;
  try {
    if (state.publish[kind]) {
      await stopLocal(kind);
      btn.dataset.on = 'false';
      btn.textContent = kind === 'audio' ? '🎤 Start mic' : '📷 Start camera';
    } else {
      await startLocal(kind);
      btn.dataset.on = 'true';
      btn.textContent = kind === 'audio' ? '🎤 Mute mic' : '📷 Stop camera';
    }
  } catch (err) {
    log(`${kind} toggle failed: ${err.message}`);
  } finally {
    btn.disabled = false;
  }
}

const MIME = {
  audio: ['audio/webm;codecs=opus', 'audio/webm'],
  video: ['video/webm;codecs=vp8', 'video/webm;codecs=vp9', 'video/webm'],
};

function pickMime(kind) {
  for (const c of MIME[kind]) {
    if (MediaRecorder.isTypeSupported(c)) return c;
  }
  return null;
}

async function startLocal(kind) {
  const mime = pickMime(kind);
  if (!mime) throw new Error(`browser cannot record ${kind}/webm`);

  const constraints = kind === 'audio'
    ? { audio: true }
    : { video: { width: 640, height: 480 } };
  const ms = await navigator.mediaDevices.getUserMedia(constraints);

  if (kind === 'video') {
    // Show local preview tile.
    if (!$('local-tile')) {
      const div = document.createElement('div');
      div.className = 'tile';
      div.id = 'local-tile';
      div.innerHTML = `<b>you (local preview)</b><video id="local-video" autoplay playsinline muted></video>`;
      $('tiles').prepend(div);
    }
    $('local-video').srcObject = ms;
  }

  const recorder = new MediaRecorder(ms, {
    mimeType: mime,
    ...(kind === 'audio' ? { audioBitsPerSecond: 64_000 } : { videoBitsPerSecond: 800_000 }),
  });

  // Bridge MediaRecorder output into a ReadableStream we hand to fetch.
  const { readable, writable } = new TransformStream();
  const writer = writable.getWriter();
  recorder.ondataavailable = async (e) => {
    if (!e.data?.size) return;
    try {
      await writer.write(new Uint8Array(await e.data.arrayBuffer()));
    } catch (_) { /* aborted */ }
  };
  recorder.onstop = async () => { try { await writer.close(); } catch (_) {} };
  recorder.start(100);

  const ctl = new AbortController();
  state.publish[kind] = { recorder, writer, mediaStream: ms, ctl };

  const url = `/api/rooms/${state.roomId}/participants/${state.participantId}/tracks/${kind}`;
  fetch(url, {
    method: 'POST',
    headers: authHeaders({ 'Content-Type': mime }),
    body: readable,
    duplex: 'half',
    signal: ctl.signal,
  }).then((r) => log(`publish ${kind}: HTTP ${r.status}`))
    .catch((err) => { if (err.name !== 'AbortError') log(`publish ${kind} failed: ${err.message}`); });

  log(`started ${kind}`);
}

async function stopLocal(kind) {
  const p = state.publish[kind];
  if (!p) return;
  state.publish[kind] = null;
  try { p.recorder.stop(); } catch (_) {}
  try { p.ctl.abort(); } catch (_) {}
  for (const t of p.mediaStream.getTracks()) {
    try { t.stop(); } catch (_) {}
  }
  if (kind === 'video') {
    const lv = $('local-video');
    if (lv) lv.srcObject = null;
    $('local-tile')?.remove();
  }
  log(`stopped ${kind}`);
}

// --- Remote subscription --------------------------------------------------

async function subscribeRemote(pid, kind) {
  const key = `${pid}:${kind}`;
  if (state.remotes.has(key)) return;
  const ctl = new AbortController();
  const entry = { ctl };
  state.remotes.set(key, entry);
  try {
    const url = `/api/rooms/${state.roomId}/participants/${pid}/tracks/${kind}?subscriber=${state.participantId}`;
    const resp = await fetch(url, { signal: ctl.signal, headers: authHeaders() });
    if (!resp.ok) { log(`subscribe ${key}: HTTP ${resp.status}`); return; }
    const mime = resp.headers.get('Content-Type') || (kind === 'audio' ? 'audio/webm;codecs=opus' : 'video/webm;codecs=vp8');
    await pipeIntoMediaSource(pid, kind, mime, resp.body, entry);
  } catch (err) {
    if (err.name !== 'AbortError') log(`subscribe ${key} ended: ${err.message}`);
  } finally {
    state.remotes.delete(key);
  }
}

function stopRemoteSubscription(pid, kind) {
  const key = `${pid}:${kind}`;
  const e = state.remotes.get(key);
  if (e) {
    try { e.ctl.abort(); } catch (_) {}
  }
  state.remotes.delete(key);
}

async function pipeIntoMediaSource(pid, kind, mimeType, bodyStream, entry) {
  const el = kind === 'audio' ? $(`audio-${pid}`) : $(`video-${pid}`);
  if (!el) return;
  if (!('MediaSource' in window) || !MediaSource.isTypeSupported(mimeType)) {
    log(`MediaSource cannot play ${mimeType}`);
    return;
  }
  const ms = new MediaSource();
  entry.ms = ms;
  el.src = URL.createObjectURL(ms);
  await new Promise((r) => ms.addEventListener('sourceopen', r, { once: true }));
  const sb = ms.addSourceBuffer(mimeType);
  sb.mode = 'sequence';

  // Browsers block autoplay for media with audio. Try to play; if blocked,
  // fall back to a one-tap "click to start" handler on the tile.
  const tryPlay = () => el.play().catch((err) => {
    log(`autoplay blocked for ${pid}:${kind} (${err.name}); click the tile to start`);
    const tap = () => {
      el.removeEventListener('click', tap);
      el.play().catch((e) => log(`manual play failed: ${e.message}`));
    };
    el.style.cursor = 'pointer';
    el.title = 'click to start playback';
    el.addEventListener('click', tap, { once: true });
  });
  el.addEventListener('loadedmetadata', tryPlay, { once: true });

  const queue = [];
  let detached = false;

  const stop = (why) => {
    if (detached) return;
    detached = true;
    queue.length = 0;
    try { el.playbackRate = 1.0; } catch (_) {}
    log(`stream ${pid}:${kind} stopped: ${why}`);
  };

  const trim = () => {
    if (sb.updating || sb.buffered.length === 0) return;
    const start = sb.buffered.start(0);
    const safeEnd = el.currentTime - 5.0;
    if (safeEnd > start + 1.0) {
      try { sb.remove(start, safeEnd); } catch (_) {}
    }
  };

  const drain = () => {
    if (detached || queue.length === 0) return;
    if (ms.readyState !== 'open') { stop('mediasource closed'); return; }
    if (sb.updating) return;
    try { sb.appendBuffer(queue.shift()); }
    catch (e) { stop(`appendBuffer: ${e.message}`); }
  };

  sb.addEventListener('updateend', () => { trim(); drain(); });
  sb.addEventListener('error', () => stop('source-buffer error'));
  ms.addEventListener('sourceclose', () => stop('mediasource closed'));

  // Catch up to live edge without seeking — just nudge playbackRate.
  const catchup = setInterval(() => {
    if (detached || el.paused || sb.buffered.length === 0) return;
    const end = sb.buffered.end(sb.buffered.length - 1);
    const lag = end - el.currentTime;
    let rate = 1.0;
    if (lag > 2.0) rate = 1.15;
    else if (lag > 0.8) rate = 1.05;
    if (el.playbackRate !== rate) el.playbackRate = rate;
  }, 500);

  const reader = bodyStream.getReader();
  try {
    while (!detached) {
      const { value, done } = await reader.read();
      if (done) break;
      queue.push(value);
      drain();
    }
  } finally {
    clearInterval(catchup);
    try { if (!detached && ms.readyState === 'open') ms.endOfStream(); } catch (_) {}
  }
}
