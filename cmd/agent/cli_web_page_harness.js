// cli_web_page_harness.js — runs the REAL chat page's JavaScript outside a
// browser, so the page-side increments (B1/B2/B3/B5/B8/B9/B12) can be DRIVEN and
// OBSERVED instead of merely asserted about in Go.
//
// Why this exists: the page is an embedded Go raw string (cmd/agent/cli_web.go,
// `const webChatPage`), so a Go test can only check that a substring is present
// — it cannot run the code. This harness extracts the real <script> from that
// very constant, evaluates it against a minimal DOM shim, and lets the scenario
// file (cli_web_page_scenarios.js) inject fake fetch responses and drive the
// page's own functions. The page under test is therefore the SHIPPED one, never
// a copy: the only thing faked is the browser.
//
// Usage: node cmd/agent/cli_web_page_harness.js [--page <cli_web.go>]
// Exit 0 = every scenario passed; 1 = at least one failed.
'use strict';

const fs = require('fs');
const path = require('path');

// ---------------------------------------------------------------- DOM shim

// ClassList is the minimal class-name set the page uses (add/remove/toggle/
// contains) — enough for the queued/pending/selected states the scenarios read.
class ClassList {
  constructor(owner) { this.owner = owner; this.set = new Set(); }
  add(...names) { names.forEach((n) => this.set.add(n)); }
  remove(...names) { names.forEach((n) => this.set.delete(n)); }
  contains(name) { return this.set.has(name); }
  toggle(name, force) {
    const want = force === undefined ? !this.set.has(name) : !!force;
    if (want) { this.set.add(name); } else { this.set.delete(name); }
    return want;
  }
  toString() { return [...this.set].join(' '); }
}

// El is one shim element. textContent is the concatenation of its own text and
// its children's (so a scenario can read what the page rendered), and the
// innerHTML setter is RECORDED — a scenario asserts the markdown/replay paths
// never write markup through it (that is the XSS contract, not a style rule).
class El {
  constructor(tag, doc, id) {
    this.tagName = String(tag || 'div').toUpperCase();
    this.doc = doc;
    this.id = id || '';
    this.children = [];
    this.parentNode = null;
    this.classList = new ClassList(this);
    this.style = {};
    this.hidden = false;
    this._value = '';
    this.selectionStart = 0;
    this.scrollTop = 0;
    this.scrollHeight = 1000;
    this.clientHeight = 400;
    this.files = [];
    this._text = '';
    this._listeners = {};
    this.attrs = {};
  }
  get className() { return this.classList.toString(); }
  set className(v) {
    this.classList = new ClassList(this);
    String(v || '').split(/\s+/).filter(Boolean).forEach((n) => this.classList.add(n));
  }
  appendChild(child) { child.parentNode = this; this.children.push(child); return child; }
  removeChild(child) {
    const i = this.children.indexOf(child);
    if (i >= 0) { this.children.splice(i, 1); child.parentNode = null; }
    return child;
  }
  remove() { if (this.parentNode) { this.parentNode.removeChild(this); } }
  get firstChild() { return this.children.length ? this.children[0] : null; }
  get textContent() {
    return this._text + this.children.map((c) => c.textContent).join('');
  }
  set textContent(v) { this.children = []; this._text = v == null ? '' : String(v); }
  get innerHTML() { return this._markup || ''; }
  set innerHTML(v) {
    // Recorded, never parsed: a scenario fails if content is ever fed through
    // this path (the page builds every node from textContent).
    this.doc.markupWrites.push({ id: this.id, html: String(v) });
    this._markup = String(v);
    if (String(v) === '') { this.children = []; this._text = ''; }
  }
  addEventListener(type, fn) { (this._listeners[type] = this._listeners[type] || []).push(fn); }
  dispatch(type, ev) {
    const list = this._listeners[type] || [];
    const event = Object.assign({ type, preventDefault() {}, stopPropagation() {} }, ev || {});
    for (const fn of list) { fn(event); }
    return event;
  }
  // value is a LIVE property, not a stored string, and assigning it moves the
  // caret to the END — that is what the HTML spec says a textarea does when its
  // value is replaced from script, and the voice path depends on it: micDraftSet
  // reads selectionStart to decide where the interim region begins, so a shim
  // that left a stale index there would place the draft somewhere the browser
  // never would. Storing the string and leaving selectionStart untouched made the
  // shim MORE permissive than the browser in exactly the dimension the voice
  // settle depends on — the same class of defect the Doc's id registry comment
  // above already names it for.
  get value() { return this._value; }
  set value(v) {
    this._value = v == null ? '' : String(v);
    this.selectionStart = this._value.length;
    this.selectionEnd = this._value.length;
  }
  querySelector(sel) {
    const want = String(sel).replace(/^\./, '');
    for (const child of this.children) {
      if (child.classList.contains(want)) { return child; }
      const found = child.querySelector(sel);
      if (found) { return found; }
    }
    return null;
  }
  querySelectorAll(sel) {
    const out = [];
    const want = String(sel).replace(/^\./, '');
    for (const child of this.children) {
      if (child.classList.contains(want)) { out.push(child); }
      out.push(...child.querySelectorAll(sel));
    }
    return out;
  }
  // THE ATTRIBUTE API the page actually uses. data-theme / data-state /
  // data-tool ARE the page's state model — the CSS keys on them and the status
  // widget's contract is written as attributes — so a shim without
  // setAttribute/getAttribute cannot observe the thing under test, and the
  // page's own `if (el && el.setAttribute)` guards turn a missing method into a
  // SILENT no-op (which is how the theme first "passed" here: nothing wrote,
  // nothing read). Values are strings, as in the DOM.
  setAttribute(name, value) { this.attrs[name] = String(value); }
  getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null; }
  removeAttribute(name) { delete this.attrs[name]; }
  hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name); }
  focus() { this.focused = true; }
  click() { this.dispatch('click', {}); }
  // requestSubmit is the form's own submit entry (the page's Enter path calls
  // it), so the shim must route it exactly like a real form.
  requestSubmit() { this.dispatch('submit', {}); }
  setSelectionRange(a, b) { this.selectionStart = a; this.selectionEnd = b; }
}

// Doc is the minimal document: an id registry (the page's getElementById calls)
// plus element creation.
//
// THE REGISTRY IS THE PAGE'S OWN MARKUP, NOT A LIST THIS FILE KEEPS. It is built
// by scanning the id="…" attributes of the HTML the server actually ships, so an
// element the page binds but the markup does not carry reads as null — exactly
// what a browser does. The previous version carried a hand-kept list of ids, which
// made the shim MORE capable than the real page: deleting a button from the HTML
// left the shim still serving it, so a scenario couldn't tell the button had gone
// (measured 2026-10-25: removing the mic button from webChatPage left the voice
// scenario at PASS 17/17).
class Doc {
  constructor(pageHtml) {
    this.byId = {};
    this.markupWrites = [];
    this._listeners = {};
    // The ids are read OUT of the HTML the server ships (shippedElementIDs),
    // not kept in a hand-maintained list: a list had to be edited every time a
    // control was added (the 📷 and 🎤 buttons each did), and a forgotten edit
    // made the shim — not the page — the failure point. A missing id now binds
    // null and the harness still fails loudly on the null binding.
    for (const id of shippedElementIDs(pageHtml || '')) {
      this.byId[id] = new El(id === 'input' || id === 'form' ? 'textarea' : 'div', this, id);
    }
    // documentElement is where the THEME lives: the page sets data-theme on it,
    // and the CSS selector that swaps palettes keys on that attribute. Without
    // it the shim's `document.documentElement` is undefined, the page's own
    // `if(rootEl&&rootEl.setAttribute)` guard swallows the write, and a theme
    // scenario would have to assert about a state nothing ever produced.
    this.documentElement = new El('html', this, 'html');
    this.byId.log.scrollHeight = 2000;
    this.byId.log.clientHeight = 400;
  }
  getElementById(id) { return this.byId[id] || null; }
  createElement(tag) { return new El(tag, this); }
  createTextNode(text) {
    const el = new El('#text', this);
    el.textContent = text;
    return el;
  }
  addEventListener(type, fn) { (this._listeners[type] = this._listeners[type] || []).push(fn); }
  dispatch(type, ev) {
    const list = this._listeners[type] || [];
    const event = Object.assign({ type, preventDefault() {}, stopPropagation() {} }, ev || {});
    for (const fn of list) { fn(event); }
  }
}

// --------------------------------------------------- page extraction

// shippedElementIDs returns every id="…" the page's HTML carries, in document
// order. It is the shim's ONLY source of ids, so a missing element is null here
// for the same reason it is null in a browser.
function shippedElementIDs(pageHtml) {
  const out = [];
  const re = /\sid="([^"]+)"/g;
  let m;
  const body = pageHtml.indexOf('<body>') >= 0 ? pageHtml.slice(pageHtml.indexOf('<body>')) : pageHtml;
  while ((m = re.exec(body)) !== null) { out.push(m[1]); }
  return out;
}

// extractPageScript reads the REAL page out of cli_web.go: the raw string after
// `const webChatPage = ` up to the line that is exactly a backtick, then the
// <script> body from it. Nothing is copied or rewritten — the harness runs the
// code the server actually serves.
function extractPageScript(goFile) {
  const src = fs.readFileSync(goFile, 'utf8');
  const marker = 'const webChatPage = `';
  const start = src.indexOf(marker);
  if (start < 0) { throw new Error('webChatPage constant not found in ' + goFile); }
  const lines = src.slice(start + marker.length).split('\n');
  const body = [];
  let closed = false;
  for (const line of lines) {
    if (line === '`') { closed = true; break; }
    body.push(line);
  }
  if (!closed) { throw new Error('webChatPage raw string is unterminated'); }
  const page = body.join('\n');
  const open = page.indexOf('<script>');
  const close = page.lastIndexOf('</script>');
  if (open < 0 || close < 0) { throw new Error('no <script> block in the page'); }
  return { page, script: page.slice(open + '<script>'.length, close) };
}

// The shim browser's default window geometry. Deliberately NOT a round desktop
// size, so a scenario that reads back 1440x2800 (the server's default) cannot
// be confused with the page having reported its own window.
// THE CHAT ROUTE NAMES, as the harness counts them.
//
// These are the harness's OWN copy of routes the server registers
// (cmd/agent/cli_web.go + cli_web_queue.go) and the page POSTs to. They exist
// because the counter below must be able to tell them APART, which a prefix
// test cannot: every one of them starts with '/api/chat', so counting
// `indexOf('/api/chat') === 0` as "a turn ran" counted the queue REGISTRATION
// as a turn too (measured — see probe_b12_turncount in this task's session
// directory: a single mid-turn message produced 2 counted posts before it was
// delivered and 3 after, which is exactly the +1 B12 reported at BOTH of its
// counting points).
//
// The agreement with the server is pinned Go-side by
// TestPageHarnessCountsTurnsOnTheRouteNotAPrefix, which reads these literals
// out of this file and compares them with the Go constants — the harness cannot
// import Go, so that test is the only thing between a rename and a silent
// desync (the same split TestWebPageCarriesTheIdentityRoute covers for the page).
const CHAT_TURN_ROUTE = '/api/chat';
const CHAT_QUEUE_ROUTE = '/api/chat/queue';
const CHAT_QUEUE_CANCEL_ROUTE = '/api/chat/queue/cancel';
const CHAT_RESET_ROUTE = '/api/chat/reset';
const CHAT_INTERRUPT_ROUTE = '/api/chat/interrupt';
// WEB_RESET_BODY_MESSAGE mirrors the Go constant of the same name: the reset
// route's ack body, which the page reads to decide it may wipe its transcript.
const WEB_RESET_BODY_MESSAGE = 'conversation reset — the transcript and per-turn state were cleared';

const HARNESS_WINDOW_WIDTH = 1280;
const HARNESS_WINDOW_HEIGHT = 720;

// loadPage evaluates the page script against a fresh shim and returns the
// handles a scenario needs (its functions + live state accessors).
function loadPage(goFile) {
  const { page, script } = extractPageScript(goFile);
  const doc = new Doc(page);
  const state = { posts: [], fetches: [], streamQueue: [], locationSearch: '',
    // The turn counter's siblings. `posts` counts TURNS (POSTs to
    // CHAT_TURN_ROUTE); these count the other things the page says to the
    // server, so a scenario can assert each fact separately instead of
    // inferring it from one number.
    queueRegistrations: [], queueCancels: [], resets: [], interrupts: [],
    turnRoute: CHAT_TURN_ROUTE };
  // The page reads the per-run access token out of window.location.search (the
  // ?t= the banner prints). The shim carries a LIVE location object — the
  // getter reads state.locationSearch on every access, so a scenario can change
  // the URL the "browser" was opened with mid-run (exactly what a fresh ?t=
  // pasted into the same tab does).
  // The shim window also carries a LIVE geometry, because the page reports its
  // own window size to /api/shot (R6): the capture must match the operator's
  // window, so the page sends window.innerWidth/innerHeight. Live getters (not
  // values) let a scenario RESIZE the window mid-run and then assert what the
  // page actually sent — the page is driven, never asserted about.
  state.innerWidth = HARNESS_WINDOW_WIDTH;
  state.innerHeight = HARNESS_WINDOW_HEIGHT;
  // THE THEME'S TWO BROWSER CHANNELS, both live.
  //
  // localStorage is a plain Map-backed store: a STORED preference is what makes
  // the page stop following the system, so a scenario must be able to set, read
  // and clear it — and to make the read THROW (a private window), which is the
  // state the page's `try/catch` treats as "follow the system".
  // matchMedia answers with a live `matches` getter over state.systemLight and
  // keeps the registered listeners, so the scenario can fire the `change` event
  // the real browser fires when the OS palette flips at sunset.
  state.themeStore = new Map();
  state.systemLight = false;
  state.themeListeners = [];
  state.themeReadsThrow = false;
  const localStorageMock = {
    getItem(k) {
      if (state.themeReadsThrow) { throw new Error('storage denied (private window)'); }
      return state.themeStore.has(k) ? state.themeStore.get(k) : null;
    },
    setItem(k, v) {
      if (state.themeReadsThrow) { throw new Error('storage denied (private window)'); }
      state.themeStore.set(k, String(v));
    },
    removeItem(k) {
      if (state.themeReadsThrow) { throw new Error('storage denied (private window)'); }
      state.themeStore.delete(k);
    },
  };
  const win = {
    location: { get search() { return state.locationSearch; } },
    get innerWidth() { return state.innerWidth; },
    get innerHeight() { return state.innerHeight; },
    localStorage: localStorageMock,
    matchMedia(query) {
      return {
        media: query,
        get matches() { return state.systemLight; },
        addEventListener(type, fn) { if (type === 'change') { state.themeListeners.push(fn); } },
        removeEventListener(type, fn) {
          const i = state.themeListeners.indexOf(fn);
          if (i >= 0) { state.themeListeners.splice(i, 1); }
        },
      };
    },
  };
  const fetchMock = (url, opts) => {
    opts = opts || {};
    state.fetches.push({ url: String(url), opts });
    // THE ROUTE, NOT A PREFIX — this is the counter's ONE definition, and it was
    // wrong in the only way that matters: a turn is what the page POSTs to MAKE a
    // turn, and A PREFIX TEST CANNOT TELL THE ROUTES APART. All of
    // '/api/chat', '/api/chat/queue', '/api/chat/queue/cancel',
    // '/api/chat/reset' and '/api/chat/interrupt' start with '/api/chat', so when
    // the page grew the registration route (1a2883c) every mid-turn message
    // began arriving here TWICE — once as a registration, once as the turn it
    // later becomes — and `state.posts` silently gained one entry per typed
    // message. That is exactly the +1 the B12 scenario reported at both of its
    // counting points (got 2 want 1, got 3 want 2): the page was right and the
    // observation was double-counting. Every bucket below is keyed on the route
    // the page actually has, so a future route under the same prefix cannot
    // become a turn by accident.
    const route = String(url).split('?')[0];
    if (route === CHAT_TURN_ROUTE) {
      let body = {};
      try { body = JSON.parse(opts.body || '{}'); } catch (e) { body = {}; }
      state.posts.push(body);
      return state.streamQueue.length
        ? state.streamQueue.shift()(body)
        : Promise.resolve(sseResponse([['done', '{}']]));
    }
    // THE WAITING-MESSAGE REGISTRATION (cli_web_queue.go). It is NOT a turn: the
    // message is on screen and has an id, and its turn arrives later through the
    // route above. Counted in its own bucket so a scenario can tell "the page
    // told the server" (this) from "the page ran a turn" (posts) — those are the
    // two different facts B12's sequential-drain assertion is made of.
    if (route === CHAT_QUEUE_ROUTE) {
      let body = {};
      try { body = JSON.parse(opts.body || '{}'); } catch (e) { body = {}; }
      // The entry records the body the page SENT *and* the id the server HANDED
      // OUT, because the two facts answer different questions: body.placement
      // says how the operator typed it, id says what the delivered turn must
      // carry back (queue_id). Recording only the body left `queueRegs[0].id`
      // undefined, and an assertion comparing it with the delivery proved
      // nothing.
      const id = state.queueRegistrations.length + 1;
      state.queueRegistrations.push(Object.assign({ id: id }, body));
      return Promise.resolve(jsonResponse({
        id: id,
        state: body.placement || '',
        placement: body.placement || '',
        position: id,
        pending: state.queueRegistrations.map((it, i) => ({ id: it.id, state: it.placement || '', position: i + 1, preview: it.message || '' })),
      }));
    }
    if (route === CHAT_QUEUE_CANCEL_ROUTE) {
      let body = {};
      try { body = JSON.parse(opts.body || '{}'); } catch (e) { body = {}; }
      state.queueCancels.push(body);
      return Promise.resolve(jsonResponse({ ok: true, cancelled: body.id || 0, pending: 0 }));
    }
    // The two /api/chat siblings that are answers to a control gesture, never a
    // turn. They had no handler at all before (they fell through to the empty
    // JSON below) and the prefix test above had been counting them as turns.
    if (route === CHAT_RESET_ROUTE) {
      state.resets.push({ url: String(url), opts: opts });
      return Promise.resolve(jsonResponse({ ok: true, message: WEB_RESET_BODY_MESSAGE, history_messages: 0 }));
    }
    if (route === CHAT_INTERRUPT_ROUTE) {
      state.interrupts.push({ url: String(url), opts: opts });
      return Promise.resolve(jsonResponse({ ok: true, interrupted: true }));
    }
    if (String(url).indexOf('/api/transcript') === 0) {
      return Promise.resolve(jsonResponse(state.transcript || { messages: [] }));
    }
    if (String(url).indexOf('/api/shot') === 0) {
      // R6: the page-capture endpoint. A scenario installs state.shotResponse
      // (a Response-like object, or a function returning one) — the page's
      // capture path is exercised against the SHAPE the server really sends,
      // including its refusal shape (ok:false + {error}).
      const r = state.shotResponse;
      if (!r) {
        return Promise.resolve({ ok: false, status: 502, json: () => Promise.resolve({ error: 'no shot handler installed' }), text: () => Promise.resolve('{"error":"no shot handler installed"}') });
      }
      return Promise.resolve(typeof r === 'function' ? r() : r);
    }
    if (String(url).indexOf('/api/identity') === 0) {
      // The identity banner is OPT-IN per scenario: the page calls
      // identifyEngine() at load, so a scenario that counts log children must
      // not be polluted by a node it never asked for. Unset → the route reads
      // as unavailable and the page renders nothing (which is also what an
      // older/gated server does).
      if (!state.identity) {
        return Promise.resolve({ ok: false, status: 404, json: () => Promise.resolve({}), text: () => Promise.resolve('') });
      }
      return Promise.resolve(jsonResponse(state.identity));
    }
    // THE LIVE CANCEL is matched before the bare stream prefix for the same
    // reason the stop route is: '/api/voice/live/cancel' starts with
    // '/api/voice/live'.
    if (String(url).indexOf('/api/voice/live/cancel') === 0) {
      state.voiceCalls = state.voiceCalls || [];
      state.voiceCalls.push('/api/voice/live/cancel');
      const r = state.voiceLiveCancel;
      if (!r) { return Promise.resolve(jsonResponse({ ok: true, cancelled: true })); }
      return Promise.resolve(r.status && r.status !== 200
        ? { ok: false, status: r.status, json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) }
        : jsonResponse(r));
    }
    if (String(url).indexOf('/api/voice/cancel') === 0) {
      state.voiceCalls = state.voiceCalls || [];
      state.voiceCalls.push('/api/voice/cancel');
      const r = state.voiceCancel;
      if (!r) { return Promise.resolve(jsonResponse({ ok: true, cancelled: true })); }
      return Promise.resolve(r.status && r.status !== 200
        ? { ok: false, status: r.status, json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) }
        : jsonResponse(r));
    }
    // THE LIVE MICROPHONE (web_voice_live.go). The stop route is matched FIRST
    // because '/api/voice/live/stop' also starts with '/api/voice/live' — the
    // page's two URLs share a prefix and only one of them is the stream.
    if (String(url).indexOf('/api/voice/live/stop') === 0) {
      state.voiceCalls = state.voiceCalls || [];
      state.voiceCalls.push('/api/voice/live/stop');
      const r = state.voiceLiveStop;
      if (!r) { return Promise.resolve(jsonResponse({ ok: true, stopped: true })); }
      return Promise.resolve(r.status && r.status !== 200
        ? { ok: false, status: r.status, json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) }
        : jsonResponse(r));
    }
    if (String(url).indexOf('/api/voice/live') === 0) {
      // A streamed Response (the harness's sseResponse) or a REFUSAL object —
      // state.voiceLive selects which, so the fallback path is exercised against
      // the shape the server really sends for an engine with no live arm.
      state.voiceCalls = state.voiceCalls || [];
      state.voiceCalls.push('/api/voice/live');
      const r = state.voiceLive;
      if (!r) {
        return Promise.resolve(sseResponse([
          [state.voiceLiveReadyEvent || 'ready', '{"ok":true,"device":"pulse:test-mic"}'],
          ['final', '{"ok":true,"draft":"","transcript":""}'],
        ]));
      }
      return Promise.resolve(r);
    }
    if (String(url).indexOf('/api/voice/start') === 0) {
      // THE MICROPHONE (web_voice.go). The start/stop answers are per-scenario
      // state, so a scenario can hand the page a draft, an empty transcript or a
      // refusal without a device or an engine existing anywhere.
      state.voiceCalls = state.voiceCalls || [];
      state.voiceCalls.push('/api/voice/start');
      const r = state.voiceStart;
      if (!r) { return Promise.resolve(jsonResponse({ ok: true, device: 'pulse:test-mic', notes: [] })); }
      return Promise.resolve(r.status && r.status !== 200
        ? { ok: false, status: r.status, json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) }
        : jsonResponse(r));
    }
    if (String(url).indexOf('/api/voice/stop') === 0) {
      state.voiceCalls = state.voiceCalls || [];
      state.voiceCalls.push('/api/voice/stop');
      const r = state.voiceStop;
      if (!r) { return Promise.resolve(jsonResponse({ ok: true, draft: '', transcript: '', device: 'pulse:test-mic', notes: [] })); }
      return Promise.resolve(r.status && r.status !== 200
        ? { ok: false, status: r.status, json: () => Promise.resolve(r), text: () => Promise.resolve(JSON.stringify(r)) }
        : jsonResponse(r));
    }
    if (String(url).indexOf('/api/menu') === 0) {      const q = new URL('http://x' + String(url).slice(String(url).indexOf('/api/menu') + '/api/menu'.length));
      const handler = state.menuHandler;
      const payload = handler
        ? handler(q.searchParams.get('trigger'), q.searchParams.get('q') || '')
        : { trigger: '', query: '', source: '', candidates: [], total: 0 };
      return Promise.resolve(jsonResponse(payload));
    }
    return Promise.resolve(jsonResponse({}));
  };
  // The export line runs INSIDE the page's own function scope, so the live
  // bindings (menu/pendingMsgs/busy are reassigned, not mutated) are read
  // through accessors rather than copied.
  const exported = `return {detectMenu: detectMenu, markdownNodes: markdownNodes, ` +
    `codeSpanSplit: codeSpanSplit, boldSpanSplit: boldSpanSplit, renderMarkdown: renderMarkdown, renderAssistantBody: renderAssistantBody, ` +
    `atBottom: atBottom, follow: follow, scrollToBottom: scrollToBottom, submitGesture: submitGesture, ` +
    `menuRefresh: menuRefresh, menuMove: menuMove, menuComplete: menuComplete, menuClose: menuClose, ` +
    `applyCompletion: applyCompletion, fenceParity: fenceParity, stageFiles: stageFiles, ` +
    `clearNode: clearNode, renderMenu: renderMenu, replayTranscript: replayTranscript, ` +
    `identifyEngine: identifyEngine, ` +
    `stopTurn: stopTurn, menuSteerPrompt: menuSteerPrompt, ` +
    `withToken: withToken, locationToken: locationToken, ` +
    `micToggle: micToggle, micInsert: micInsert, micSetState: micSetState, ` +
    `micCancel: micCancel, micStopKeep: micStopKeep, micCaretSettle: micCaretSettle, ` +
    `micEscAct: micEscAct, micEscRun: micEscRun, micVoiceText: micVoiceText, ` +
    `micLiveRun: micLiveRun, micDraftSet: micDraftSet, micDraftClear: micDraftClear, micSettleLive: micSettleLive, ` +
    `state: () => ({menu: menu, pendingMsgs: pendingMsgs, busy: busy, images: images, lastQueued: lastQueued, micBusy: micBusy, micLiveActive: micLiveActive}), ` +
    `consts: {FOLLOW_THRESHOLD: FOLLOW_THRESHOLD, RESET_NOTICE: RESET_NOTICE, MD_FENCE: MD_FENCE, ` +
    `MD_BOLD: MD_BOLD, MD_MAX_FENCE_LINES: MD_MAX_FENCE_LINES, TRIGGER_SLASH: TRIGGER_SLASH, TRIGGER_AT: TRIGGER_AT, ` +
    `STEER_PREFIX: STEER_PREFIX, PLACEMENT_QUEUED: PLACEMENT_QUEUED, PLACEMENT_STEERING: PLACEMENT_STEERING, ` +
    `QUEUE_CANCEL_LABEL: QUEUE_CANCEL_LABEL, QUEUE_CANCELLED_LABEL: QUEUE_CANCELLED_LABEL, QUEUE_CANCEL_FAILED: QUEUE_CANCEL_FAILED, ` +
    `MENU_MAX_ITEMS: MENU_MAX_ITEMS, MENU_MORE_FLAG: MENU_MORE_FLAG, API_MENU: API_MENU, ` +
    `TOKEN_ARG: TOKEN_ARG, QUERY_SEP: QUERY_SEP, API_CHAT: API_CHAT, API_TRANSCRIPT: API_TRANSCRIPT, API_SHOT: API_SHOT, CMD_IMAGES_FIELD: CMD_IMAGES_FIELD, ` +
    `API_VOICE_START: API_VOICE_START, API_VOICE_STOP: API_VOICE_STOP, API_VOICE_LIVE: API_VOICE_LIVE, API_VOICE_LIVE_STOP: API_VOICE_LIVE_STOP, ` +
    `EV_LIVE_READY: EV_LIVE_READY, EV_LIVE_DRAFT: EV_LIVE_DRAFT, EV_LIVE_FINAL: EV_LIVE_FINAL, ` +
    `MAX_IMAGE_BYTES: MAX_IMAGE_BYTES, MAX_B64: MAX_B64, INPUT_MAX_ROWS: INPUT_MAX_ROWS, ` +
    `KEY_ARROW_DOWN: KEY_ARROW_DOWN, KEY_ARROW_UP: KEY_ARROW_UP, KEY_ENTER: KEY_ENTER, ` +
    `KEY_TAB: KEY_TAB, KEY_ESCAPE: KEY_ESCAPE, MENU_NAV_KEYS: MENU_NAV_KEYS}, ` +
    `capturePage: capturePage, shotFail: shotFail, renderStrip: renderStrip, ` +
    `themeApply: themeApply, themeStored: themeStored, themePrefersLight: themePrefersLight, ` +
    // aborted() MEASURES WHETHER THE PAGE ACTUALLY CALLED abort() ON THE RUNNING
    // TURN. The fake stream is a promise that does not honour a signal, so a
    // scenario cannot read the abort out of `busy` (it stays true until the
    // scenario releases the stream) — but the page does not guess either: it
    // reads activeCtl.signal, and the REAL AbortController flips `aborted` the
    // moment abort() is called. Exposing that flag is the difference between
    // asserting "the arbiter said turn" and asserting "the turn was stopped".
    `aborted: () => !!(activeCtl && activeCtl.signal && activeCtl.signal.aborted), ` +
    `els: {log: logEl, input: input, menu: menuEl, jump: jumpBtn, form: form, send: sendBtn, strip: strip, shot: shotBtn, voicelive: voiceLiveEl}};`;
  const fn = new Function('document', 'window', 'fetch', 'AbortController', 'FileReader',
    'setTimeout', 'clearTimeout', 'URLSearchParams', script + '\n' + exported);
  const pageExports = fn(doc, win, fetchMock, AbortController, FakeFileReader, setTimeout, clearTimeout, URLSearchParams);
  // THE BROWSER FIRES THIS, SO THE SHIM DOES TOO. The page binds its handlers
  // inside a DOMContentLoaded listener (a real page's script can run before the
  // markup is parsed), so a harness that never dispatches it leaves those
  // handlers UNBOUND — the elements exist, nothing listens, and a scenario
  // clicking them measures its own shim instead of the page.
  doc.dispatch('DOMContentLoaded', {});
  return { page, doc, state, api: pageExports };
}

// jsonResponse builds the Response-like object the page's fetch handlers read.
function jsonResponse(payload) {
  return {
    ok: true,
    status: 200,
    json: () => Promise.resolve(payload),
    text: () => Promise.resolve(JSON.stringify(payload)),
  };
}

// sseResponse turns frames ([[event, data], …]) into the streamed Response the
// page's reader consumes — the SAME wire shape /api/chat sends.
function sseResponse(frames) {
  const text = frames.map(([ev, data]) => `event: ${ev}\ndata: ${data}\n\n`).join('');
  const bytes = Buffer.from(text, 'utf8');
  let sent = false;
  return {
    ok: true,
    status: 200,
    body: {
      getReader: () => ({
        read: () => {
          if (sent) { return Promise.resolve({ done: true, value: undefined }); }
          sent = true;
          return Promise.resolve({ done: false, value: new Uint8Array(bytes) });
        },
      }),
    },
    text: () => Promise.resolve(text),
  };
}

// streamedTurn builds a streamQueue entry: a turn whose frames are produced by
// `frames` (possibly after a delay the scenario controls), so a test can hold a
// turn open and submit another message into the queue.
function streamedTurn(frames) {
  return () => sseResponse(frames);
}

// FakeFileReader is the shim the upload path needs (stageFiles reads a file as
// a data URL); the scenarios only use it to prove the byte budget is the ONE
// decoded-bytes budget.
class FakeFileReader {
  readAsDataURL(file) {
    this.result = 'data:' + (file.type || 'image/png') + ';base64,' + (file.b64 || '');
    if (this.onload) { this.onload(); }
  }
}

// ------------------------------------------------------------ test helpers

let failures = 0;
let checks = 0;

function ok(cond, label) {
  checks++;
  if (cond) { console.log('  ok   ' + label); return true; }
  failures++;
  console.log('  FAIL ' + label);
  return false;
}

function eq(got, want, label) {
  return ok(got === want, label + ' (got ' + JSON.stringify(got) + ', want ' + JSON.stringify(want) + ')');
}

// tick lets the page's promises settle (the shim is synchronous; the page is
// promise-based, so scenarios await a macrotask between steps).
function tick(ms) { return new Promise((r) => setTimeout(r, ms || 0)); }

// main runs the scenario file against the real page.
//
// --only <substring> runs the scenarios whose NAME contains the substring, so a
// single increment's gate can be invoked on its own (a Go test names the
// scenario it is red-proofing instead of parsing the whole suite's tally). A
// filter that matches nothing is a failure, never a silent pass.
async function main() {
  const pageFlag = process.argv.indexOf('--page');
  const goFile = pageFlag >= 0 && process.argv[pageFlag + 1]
    ? process.argv[pageFlag + 1]
    : path.join(__dirname, 'cli_web.go');
  const onlyFlag = process.argv.indexOf('--only');
  const only = onlyFlag >= 0 && process.argv[onlyFlag + 1] ? process.argv[onlyFlag + 1] : '';
  const scenarios = require('./cli_web_page_scenarios.js');
  const chosen = only ? scenarios.filter((sc) => sc.name.indexOf(only) >= 0) : scenarios;
  console.log('page harness: ' + goFile + (only ? ' (--only ' + only + ')' : ''));
  if (only && chosen.length === 0) {
    console.log('\nFAIL: no scenario name contains ' + JSON.stringify(only));
    process.exit(1);
  }
  for (const sc of chosen) {
    const ctx = loadPage(goFile);
    console.log('\n[' + sc.name + ']' + (sc.about ? ' — ' + sc.about : ''));
    try {
      await sc.run(ctx, { ok, eq, tick, streamedTurn, sseResponse, jsonResponse });
    } catch (err) {
      failures++;
      console.log('  FAIL scenario threw: ' + (err && err.stack || err));
    }
  }
  console.log('\n' + (failures === 0 ? 'PASS' : 'FAIL') + ': ' + (checks - failures) + '/' + checks + ' checks');
  process.exit(failures === 0 ? 0 : 1);
}

module.exports = { loadPage, extractPageScript, ok, eq, tick, streamedTurn, sseResponse, jsonResponse };

if (require.main === module) {
  main().catch((err) => { console.error(err); process.exit(1); });
}
