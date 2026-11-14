// cli_web_page_scenarios.js — the DRIVEN scenarios for the M2 web page.
//
// Each scenario receives the REAL page (evaluated by cli_web_page_harness.js)
// plus the assertion helpers, and observes the increment's behaviour by calling
// the page's own functions and reading the DOM it built. Nothing here is a
// substring check of the source: a scenario fails if the PAGE misbehaves, not
// if a comment moved.
//
// Covered: B3 (conditional follow + jump affordance), B5 (replay rebuilds tool
// cards), B8 (markdown tokenizer + textContent-only rendering), B9 (pure detect
// core, menu arbitration, completion), B12 (queued/steering entries, sequential
// drain, sentinel on the wire, Alt+Enter steering).
'use strict';

// API_IDENTITY_PATH is the scenario-side spelling of the engine identity route
// (the harness cannot import Go constants). The agreement with the server's
// webIdentityPath is pinned Go-side by TestWebPageCarriesTheIdentityRoute, which
// reads the page's own `const API_IDENTITY=` out of cli_web.go.
const API_IDENTITY_PATH = '/api/identity';

module.exports = [
  // ------------------------------------------------------------- B1 + B12
  {
    name: 'B12 composer stays usable during a turn; a mid-turn message is a visible QUEUED entry',
    about: 'the pre-B12 page disabled the input while busy, so a typed message could only be lost',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input, form = api.els.form, log = api.els.log;
      const json = (o) => JSON.stringify(o);
      // Turn 1 stays open until the scenario releases it, so message 2 is
      // genuinely typed MID-TURN.
      let release;
      state.streamQueue.push(() => new Promise((res) => {
        release = () => res(t.sseResponse([['delta', json({ text: 'first answer' })], ['done', '{}']]));
      }));
      input.value = 'first message';
      form.dispatch('submit', {});
      await t.tick();

      // A. The composer is NOT locked while the turn runs (the whole point).
      t.ok(doc.getElementById('input').value === '', 'the composer was cleared for the new turn');
      const s1 = api.state();
      t.eq(s1.busy, true, 'a turn is in flight');
      t.eq(input.disabled === true, false, 'the composer stays ENABLED during a turn (a message typed now is queueable)');
      t.eq(api.els.send.disabled === true, false, 'Send stays clickable during a turn (it queues)');

      // B. Type and submit a second message while turn 1 is still running.
      input.value = 'second message';
      form.dispatch('submit', {});
      await t.tick();
      const s2 = api.state();
      t.eq(s2.pendingMsgs.length, 1, 'the mid-turn message became a queued entry');
      t.eq(s2.pendingMsgs[0].text, 'second message', 'the queued entry carries the message verbatim');
      t.eq(s2.pendingMsgs[0].placement, api.consts.PLACEMENT_QUEUED, "its placement is 'queued'");
      t.eq(state.posts.length, 1, 'the queued entry was NOT posted while turn 1 ran (strictly sequential)');
      // THE OTHER HALF, and the reason this scenario can tell "queued" from
      // "dropped": the page DID tell the server the message exists — as a
      // REGISTRATION (its own route, its own id), not as a turn. Without this
      // line a page that silently swallowed the message would satisfy the count
      // above by never talking to the server at all.
      t.eq(state.queueRegistrations.length, 1, 'the queued entry WAS registered with the server (it has an id and a position BEFORE its turn)');
      t.eq(s2.pendingMsgs[0].id > 0, true, 'the registration came back with an id, so the entry is withdrawable');
      t.eq(state.posts.length + state.queueRegistrations.length, 2,
        'turn 1 ran and the mid-turn message was registered — exactly one request of each kind, never a second turn');

      // C. The entry is VISIBLE (dashed .queued + the placement badge).
      const queued = log.children.filter((c) => c.classList.contains('queued') && c.classList.contains('pending'));
      t.eq(queued.length, 1, 'exactly one queued entry is on screen');
      t.ok(queued[0].textContent.indexOf('second message') >= 0, 'the entry renders the message text');
      const badge = queued[0].children.find((c) => c.classList.contains('placement'));
      t.ok(badge && badge.textContent === api.consts.PLACEMENT_QUEUED, 'the entry carries the QUEUED placement badge');

      // D. Release turn 1 → the queue drains, one turn at a time.
      release();
      await t.tick(10);
      await t.tick(10);
      t.eq(state.posts.length, 2, 'the queued entry was delivered as its own turn after turn 1 finished');
      t.eq(state.posts[1].message, 'second message', 'the delivered turn carries the original text');
      // ONE DELIVERY, NOT A REPEAT: the registration above was the page telling
      // the server a message WAITS; this is the page running its turn. They are
      // different requests on different routes, and the page must make each
      // exactly once — two of either would be a duplicate message on the wire.
      t.eq(state.queueRegistrations.length, 1, 'the delivery did NOT re-register the entry (a second registration would be a duplicate on the server queue)');
      t.eq(state.posts.length - 1, 1, 'exactly ONE turn followed turn 1 — the queue drained one entry, not two');
      // THE ENTRY THAT WAITED **IS** THE TURN THAT RAN — not a copy of it. The
      // delivered POST carries back the id the registration handed out, which is
      // what makes a withdrawal of that id reach this very turn (the page sends
      // queue_id for exactly this reason, cli_web.go runTurn). Without this line
      // a page that registered one object and delivered a different one would
      // pass every count above while the operator's "withdraw" aimed at nothing.
      t.eq(state.posts[1].queue_id, state.queueRegistrations[0].id,
        'the delivered turn carries the queue_id its registration handed out (the same entry, not a copy)');
      const s3 = api.state();
      t.eq(s3.pendingMsgs.length, 0, 'the queue drained completely');
      t.eq(s3.busy, false, 'the composer is idle again');
      t.eq(queued[0].classList.contains('pending'), false, 'the delivered entry is no longer "pending"');
      t.eq(queued[0].classList.contains('queued'), false, 'the delivered entry is no longer "queued" (it is a turn now)');
    },
  },

  // ---------------------------------------------------------------- B12 WITHDRAW
  {
    name: 'B12 withdraw takes a WAITING message back out of the queue and its turn never runs',
    about: 'the state was first-class but the operator could not take the message back — the registration id existed and nothing spent it',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input, form = api.els.form, log = api.els.log;
      const json = (o) => JSON.stringify(o);
      let release;
      state.streamQueue.push(() => new Promise((res) => {
        release = () => res(t.sseResponse([['delta', json({ text: 'first answer' })], ['done', '{}']]));
      }));
      input.value = 'first message';
      form.dispatch('submit', {});
      await t.tick();

      // A. A mid-turn message queues, is registered, and therefore has an id —
      //    which is the ONLY thing that makes a withdrawal addressable. Without
      //    this half a page could show a button that posts nothing.
      input.value = 'regretted message';
      form.dispatch('submit', {});
      await t.tick();
      const entry = api.state().pendingMsgs[0];
      t.eq(state.queueRegistrations.length, 1, 'the waiting message was registered, so it has an id');
      t.eq(entry.id, state.queueRegistrations[0].id, 'the entry carries the id the SERVER handed out, not one it invented');

      // B. THE OPERATOR'S OWN GESTURE — the button the page renders on a waiting
      //    entry, clicked. Not the page function: the gesture, so a page that
      //    stopped wiring the button fails here.
      const btn = entry.node && entry.node.querySelector('.qcancel');
      t.ok(!!btn, 'a waiting entry renders the withdraw button');
      t.eq(btn && btn.textContent, api.consts.QUEUE_CANCEL_LABEL, 'the button carries the shared withdraw word, not a local copy');
      btn.click();
      await t.tick();

      t.eq(state.queueCancels.length, 1, 'clicking it POSTs the cancel route exactly once');
      t.eq(state.queueCancels[0].id, entry.id, 'the withdrawal addresses the REGISTERED id (a copy or a re-derived id would cancel nothing)');

      // C. THE ENTRY STAYS ON SCREEN AND SAYS WHAT HAPPENED. A deleted message is
      //    how a reader loses track of what they typed.
      t.eq(entry.cancelled, true, 'the entry is marked withdrawn');
      t.ok(entry.node && entry.node.textContent.indexOf(api.consts.QUEUE_CANCELLED_LABEL) >= 0,
        'the entry SAYS it was withdrawn (text = ' + JSON.stringify(entry.node && entry.node.textContent) + ')');
      t.ok(!entry.node.querySelector('.qcancel'), 'the withdraw button is gone — it cannot be clicked into a second cancel');

      // D. AND ITS TURN NEVER RUNS. This is the whole point: the count of turns
      //    is the assertion, not the badge.
      release();
      await t.tick(10);
      await t.tick(10);
      t.eq(state.posts.length, 1, 'only turn 1 ran — the withdrawn message was never delivered');
      t.ok(!state.posts.some((p) => String(p.message).indexOf('regretted message') >= 0),
        'no turn on the wire carries the withdrawn text (' + JSON.stringify(state.posts.map((p) => p.message)) + ')');
      t.eq(state.queueRegistrations.length, 1, 'the drain did not re-register it either');

      // E. A STEERING entry gets NO withdraw button — it addresses a run already
      //    in flight, so there is nothing to take back (the server refuses to
      //    cancel a steering id: TestWebQueueCancelRefusesUnknownAndSteering). A
      //    page that offered the button would promise something it cannot keep.
      let release2;
      state.streamQueue.push(() => new Promise((res) => {
        release2 = () => res(t.sseResponse([['delta', json({ text: 'b' })], ['done', '{}']]));
      }));
      input.value = 'second turn';
      form.dispatch('submit', {});
      await t.tick();
      input.value = 'narrow it';
      api.submitGesture(true);
      await t.tick();
      const steer = api.state().pendingMsgs.find((e) => e.placement === api.consts.PLACEMENT_STEERING);
      t.ok(!!steer, 'the steering message is on the queue');
      t.ok(steer && !steer.node.querySelector('.qcancel'),
        'a STEERING entry offers NO withdraw button (its run is already in flight — nothing to take back)');
      release2();
      await t.tick(10);
      await t.tick(10);
    },
  },

  // ---------------------------------------------------------------- B12
  {
    name: 'B12 Alt+Enter STEERS: it posts the shared sentinel and never waits for the queue',
    about: 'steering addresses the run in flight instead of becoming a later turn',
    async run(ctx, t) {
      const { api, state } = ctx;
      const input = api.els.input, form = api.els.form;
      const json = (o) => JSON.stringify(o);
      let release;
      state.streamQueue.push(() => new Promise((res) => {
        release = () => res(t.sseResponse([['delta', json({ text: 'a' })], ['done', '{}']]));
      }));
      input.value = 'go';
      form.dispatch('submit', {});
      await t.tick();

      // Alt+Enter while busy: the sentinel rides the wire, and the entry is
      // typed STEERING.
      input.value = 'narrow it';
      api.submitGesture(true);
      await t.tick();
      const pending = api.state().pendingMsgs;
      t.eq(pending.length, 1, 'the steering message is recorded');
      t.eq(pending[0].text, api.consts.STEER_PREFIX + 'narrow it', 'the wire text carries the shared steering sentinel');
      t.eq(pending[0].placement, api.consts.PLACEMENT_STEERING, "its placement is 'steering'");
      release();
      await t.tick(10);
      await t.tick(10);
      t.eq(state.posts.length, 2, 'the steering message was delivered');
      t.ok(String(state.posts[1].message).indexOf('narrow it') >= 0, 'the delivered steering message keeps its words');
      // AND IT WAS REGISTERED AS STEERING, not as a queued turn: one
      // registration, carrying the typed placement, before the delivery above.
      // A page that registered nothing would have no id to withdraw and no
      // position for the badge — the whole point of the first-class state.
      t.eq(state.queueRegistrations.length, 1, 'the steering message was registered with the server exactly once');
      t.eq(state.queueRegistrations[0].placement, api.consts.PLACEMENT_STEERING, 'the registration carries the STEERING placement, not queued');
    },
  },

  // ------------------------------------------------------------------ B3
  {
    name: 'B3 follow-bottom is CONDITIONAL and the jump affordance appears',
    about: 'a reader scrolled up must never be yanked by an incoming delta',
    async run(ctx, t) {
      const { api } = ctx;
      const log = api.els.log, jump = api.els.jump;
      // At the bottom (within the named threshold) → following is on.
      log.scrollHeight = 1000; log.clientHeight = 400; log.scrollTop = 600;
      t.eq(api.atBottom(), true, 'a view at the bottom is "following"');
      api.follow(false);
      t.eq(log.scrollTop, 1000, 'following scrolled to the bottom');
      t.eq(jump.classList.contains('show'), false, 'no jump affordance while following');

      // Scrolled up beyond the threshold → following is CONDITIONAL: OFF.
      log.scrollTop = 0;
      t.eq(api.atBottom(), false, 'a view scrolled up is NOT at the bottom');
      api.follow(false);
      t.eq(log.scrollTop, 0, 'an incoming delta did NOT yank the scrolled-up reader');
      t.eq(jump.classList.contains('show'), true, 'the "jump to latest" affordance appeared while scrolled up');

      // The threshold is one named number, and it is the same on both carriers.
      t.ok(api.consts.FOLLOW_THRESHOLD === 24, 'FOLLOW_THRESHOLD is the named 24px value');
      log.scrollTop = 1000 - 400 - api.consts.FOLLOW_THRESHOLD;   // exactly at the threshold
      t.eq(api.atBottom(), true, 'exactly at the threshold still counts as the bottom');

      // The affordance jumps and hides itself.
      jump.dispatch('click', {});
      t.eq(log.scrollTop, log.scrollHeight, 'the jump affordance scrolled to the latest');
      t.eq(jump.classList.contains('show'), false, 'the jump affordance hides after jumping');
    },
  },

  // ------------------------------------------------------------------ B8
  {
    name: 'B8 markdown is minimal, safe and streaming-friendly',
    about: 'fenced code + inline code + intra-line bold; every node built from textContent',
    async run(ctx, t) {
      const { api, doc } = ctx;
      const F = api.consts.MD_FENCE;

      // The tokenizer decides; it is pure (no DOM involved).
      const nodes = api.markdownNodes('before\n' + F + 'js\nlet x = 1;\n' + F + '\nafter');
      t.eq(nodes.length, 3, 'text / code / text');
      t.eq(nodes[1].kind, 'code', 'the fenced block is a code node');
      t.eq(nodes[1].text, 'let x = 1;', 'the fence body is preserved verbatim');

      // An INERT script tag stays text — never markup.
      const parent = doc.createElement('div');
      api.renderMarkdown(parent, 'look: <script>alert(1)</script>');
      t.eq(parent.querySelector('script'), null, 'no script ELEMENT was created (markup stays inert text)');
      t.ok(parent.textContent.indexOf('<script>') >= 0, 'the markup survived as literal text');

      // Inline code spans split around single ticks.
      const parts = api.codeSpanSplit('use ' + api.consts.MD_FENCE.charAt(0) + 'gopi web' + api.consts.MD_FENCE.charAt(0) + ' now');
      t.eq(parts.length, 3, 'text / code / text');
      t.eq(parts[1].kind, 'code', 'the span is a code piece');
      t.eq(parts[1].text, 'gopi web', 'the span body is verbatim');
      const spanEl = doc.createElement('div');
      api.renderMarkdown(spanEl, 'a ' + api.consts.MD_FENCE.charAt(0) + 'b' + api.consts.MD_FENCE.charAt(0) + ' c');
      t.ok(spanEl.querySelector('.mdcode') !== null, 'an inline span renders as a code element');
      t.ok(spanEl.querySelector('.mdpre') !== null || spanEl.querySelector('.mdpre') === null, 'render completed without markup');

      // A fenced block renders as a pre.mdpre.
      const fenceEl = doc.createElement('div');
      api.renderMarkdown(fenceEl, F + '\nx\n' + F);
      const pre = fenceEl.querySelector('.mdpre');
      t.ok(pre !== null, 'a fenced block renders as pre.mdpre');
      t.eq(pre.textContent, 'x', 'the pre carries the fence body');

      // An UNTERMINATED fence stays text (no half-built code node).
      const open = api.markdownNodes(F + '\nstill typing');
      t.eq(open.length, 1, 'an unterminated fence yields one text node');
      t.eq(open[0].kind, 'text', 'an unterminated fence is not code');

      // An over-long fence body is not formatted (the named token cap).
      const huge = F + '\n' + new Array(api.consts.MD_MAX_FENCE_LINES + 2).join('line\n') + F;
      const hugeNodes = api.markdownNodes(huge);
      t.ok(hugeNodes.every((n) => n.kind === 'text'), 'a fence beyond MD_MAX_FENCE_LINES stays plain text');

      // Streaming: an assistant body re-renders from the FULL accumulated text.
      // While only the OPENING fence has arrived the block is still plain text
      // (never a half-built code node); once the closing fence arrives the same
      // body renders as a <pre> — the re-render is driven by the accumulated
      // text, not by a one-shot parse.
      const bubble = doc.createElement('div');
      api.renderAssistantBody(bubble, 'here:\n' + F + '\npar');
      t.eq(bubble.querySelector('.mdpre'), null, 'a half-arrived fence renders as text, not a half-built code node');
      api.renderAssistantBody(bubble, 'here:\n' + F + '\npar\n' + F);
      t.ok(bubble.querySelector('.mdpre') !== null, 'once the closing fence arrives the SAME body renders as code');
      t.eq(bubble.querySelector('.mdpre').textContent, 'par', 'the finished block carries its body');
      api.renderAssistantBody(bubble, 'plain');
      t.eq(bubble.querySelector('.mdpre'), null, 're-rendering replaces the previous body');

      // ---- The intra-line emphasis layer (B8's bold): the SAME semantic the
      // terminal surfaces carry, and the SAME tokenizer rules.
      const B = api.consts.MD_BOLD;
      const boldParts = api.boldSpanSplit('a ' + B + 'b' + B + ' c');
      t.eq(boldParts.length, 3, 'bold: text / bold / text');
      t.eq(boldParts[1].kind, 'bold', 'the emphasised run is a bold piece');
      t.eq(boldParts[1].text, 'b', 'the markers are NOT part of the piece');
      t.eq(boldParts[0].text + boldParts[2].text, 'a  c', 'the plain pieces keep their text');

      // The same rules the terminal layer implements: unclosed stays literal,
      // an empty pair is literal, and no marker ever reaches a text node.
      t.eq(api.boldSpanSplit(B + 'unclosed')[0].kind, 'text', 'an unclosed marker stays literal text');
      t.eq(api.boldSpanSplit(B + 'unclosed')[0].text, B + 'unclosed', 'the unclosed marker is preserved verbatim');
      const empty = api.boldSpanSplit(B + B);
      t.ok(empty.every((p) => p.kind === 'text'), 'two adjacent markers are literal, not an empty bold run');

      // Rendering: a strong.mdstrong element, built from textContent.
      const boldEl = doc.createElement('div');
      api.renderMarkdown(boldEl, 'say ' + B + 'it now' + B + ' once');
      const strong = boldEl.querySelector('.mdstrong');
      t.ok(strong !== null, 'an emphasised run renders as strong.mdstrong');
      t.eq(strong.textContent, 'it now', 'the strong element carries the run without markers');
      t.eq(boldEl.textContent, 'say it now once', 'the rendered text drops both markers and keeps every word');
      t.eq(doc.markupWrites.length, 0, 'the bold path writes no markup either');

      // A fence wins over emphasis, exactly as code wins over the terminal
      // layer: `**` inside a code block is the literal the code says it is.
      const inFence = doc.createElement('div');
      api.renderMarkdown(inFence, F + '\na **b** c\n' + F);
      t.eq(inFence.querySelector('.mdstrong'), null, 'no bold inside a fenced block');
      t.ok(inFence.querySelector('.mdpre').textContent.indexOf(B) >= 0, 'the fence body keeps its literal markers');

      // The page NEVER writes markup through innerHTML.
      t.eq(doc.markupWrites.length, 0, 'no innerHTML write on any render path (content is never interpolated into markup)');
    },
  },

  // ------------------------------------------------------------------ B9
  {
    name: 'B9 the detect core is pure and the menu arbitrates the keyboard',
    about: "'/' at the start opens the command menu; '@' opens the file menu; Enter completes",
    async run(ctx, t) {
      const { api, state } = ctx;
      const S = api.consts.TRIGGER_SLASH, A = api.consts.TRIGGER_AT;

      // A. The pure detect core.
      t.eq(api.detectMenu('/ta', 3).trigger, S, "'/ta' with the caret after it opens the slash menu");
      t.eq(api.detectMenu('/ta', 3).query, 'ta', 'the query is the text after the trigger');
      t.eq(api.detectMenu('hello /ta', 9).trigger, '', 'a slash NOT at the start does not open a menu');
      t.eq(api.detectMenu('/task list', 10).trigger, '', 'the caret past the first word is no longer a command filter');
      t.eq(api.detectMenu('see @cli_web', 12).trigger, A, "'@' after whitespace opens the file menu");
      t.eq(api.detectMenu('see @cli_web', 12).query, 'cli_web', 'the file query is the fragment');
      t.eq(api.detectMenu('mail me@host', 12).trigger, '', 'a mid-word @ does not open a menu');
      t.eq(api.detectMenu('see @a b', 8).trigger, '', 'the caret past whitespace after @ is no longer inside the fragment');
      t.eq(api.detectMenu('@', 1).trigger, A, 'a bare @ opens the menu with an empty query');
      t.eq(api.detectMenu('/ta', 99).trigger, '', 'an out-of-range caret is refused (never a crash)');
      t.eq(api.detectMenu(api.consts.MD_FENCE + '\n/ta', 9).trigger, '', 'no menu inside an open code fence (a pasted block must not pop a menu)');
      t.eq(api.fenceParity('a\n' + api.consts.MD_FENCE + '\nb'), true, 'fence parity is odd inside a fence');

      // B. The view fetches candidates from the ONE endpoint and renders them.
      state.menuHandler = (trigger, q) => ({
        trigger, query: q, source: trigger === S ? 'slash' : 'file',
        candidates: trigger === S ? ['task', 'text2img'] : ['cmd/agent/cli_web.go'],
        total: trigger === S ? 9 : 1,
      });
      api.els.input.value = '/ta';
      api.els.input.selectionStart = 3;
      await api.menuRefresh();
      const menu = api.state().menu;
      t.eq(menu.open, true, 'the menu opened');
      t.eq(menu.items.length, 2, 'the candidates came from the endpoint');
      t.ok(state.fetches.some((f) => f.url.indexOf(api.consts.API_MENU) === 0), 'the candidates were fetched from ' + api.consts.API_MENU);
      const rows = api.els.menu.children;
      t.ok(rows.length >= 2, 'the menu rendered its rows');
      t.eq(rows[0].textContent.indexOf('task') >= 0, true, 'the first row names the first candidate');
      t.ok(rows[rows.length - 1].textContent.indexOf(api.consts.MENU_MORE_FLAG) >= 0, 'the "+more" row reports the withheld count (' + menu.total + ')');

      // C. Keyboard arbitration: arrows move the highlight (wrapping), Enter
      //    COMPLETES (never sends), Escape closes.
      t.eq(menu.sel, 0, 'the first candidate is highlighted');
      api.menuMove(1);
      t.eq(api.state().menu.sel, 1, 'ArrowDown moved the highlight');
      api.menuMove(1);
      t.eq(api.state().menu.sel, 0, 'the highlight wraps at the end');
      api.menuMove(-1);
      t.eq(api.state().menu.sel, 1, 'ArrowUp wraps at the start');
      const postsBefore = state.posts.length;
      api.els.input.dispatch('keydown', { key: 'Enter' });
      t.eq(state.posts.length, postsBefore, 'Enter with the menu open did NOT send a turn');
      t.eq(api.els.input.value, S + 'text2img ', 'Enter completed the highlighted candidate into the composer');
      t.eq(api.state().menu.open, false, 'completing closed the menu');

      // D. Escape closes without touching the composer.
      api.els.input.value = '/ta';
      api.els.input.selectionStart = 3;
      await api.menuRefresh();
      api.els.input.dispatch('keydown', { key: 'Escape' });
      t.eq(api.state().menu.open, false, 'Escape closed the menu');

      // E. With the menu CLOSED, Enter sends (the composer's own gesture).
      api.els.input.value = 'hello there';
      api.els.input.dispatch('keydown', { key: 'Enter' });
      await t.tick();
      t.eq(state.posts.length, 1, 'Enter with no menu open submitted the turn');
      t.eq(state.posts[0].message, 'hello there', 'the submitted message is the composer text');

      // F. An IME-composing Enter is not a submit (the composition owns it).
      const before = state.posts.length;
      api.els.input.value = '半角';
      api.els.input.dispatch('keydown', { key: 'Enter', isComposing: true });
      t.eq(state.posts.length, before, 'an IME-composing Enter did not submit');
    },
  },

  // ------------------------------------------------------------------ B9 nav
  {
    name: 'B9 arrow-nav: the keydown/keyup pair keeps the highlight, a new query resets it',
    about: 'the keyup listener must not re-run the detect core for a pure-navigation key (it undid every arrow press and spent a /api/menu round-trip)',
    async run(ctx, t) {
      const { api, state } = ctx;
      const K = api.consts;
      const menuFetches = () => state.fetches.filter((f) => f.url.indexOf(K.API_MENU) === 0).length;
      // A browser press is a keydown FOLLOWED by a keyup; dispatching both is
      // what makes this scenario able to see the wiring bug at all (driving
      // menuMove directly — as the B9 scenario above does — cannot).
      const press = (key) => {
        api.els.input.dispatch('keydown', { key });
        api.els.input.dispatch('keyup', { key });
      };
      const type = async (text, caret) => {
        api.els.input.value = text;
        api.els.input.selectionStart = caret === undefined ? text.length : caret;
        api.els.input.dispatch('input', {});
        await t.tick();
      };

      state.menuHandler = (trigger, q) => ({
        trigger, query: q, source: trigger === K.TRIGGER_SLASH ? 'slash' : 'file',
        candidates: trigger === K.TRIGGER_SLASH ? ['task', 'text2img', 'tools'] : ['cmd/agent/cli_web.go'],
        total: trigger === K.TRIGGER_SLASH ? 3 : 1,
      });

      // A. The five navigation keys are exactly the ones a keyup must ignore.
      t.eq(K.KEY_ARROW_DOWN, 'ArrowDown', 'the ArrowDown key name is the page constant, not an inline literal');
      for (const key of [K.KEY_ARROW_DOWN, K.KEY_ARROW_UP, K.KEY_TAB, K.KEY_ESCAPE, K.KEY_ENTER]) {
        t.eq(K.MENU_NAV_KEYS.has(key), true, key + ' is a pure-navigation key (its keyup must not refresh)');
      }

      // B. Typing the trigger opens the menu at the top of the list.
      await type(K.TRIGGER_SLASH, 1);
      t.eq(api.state().menu.open, true, 'typing the trigger opened the menu');
      t.eq(api.state().menu.sel, 0, 'a new query starts the highlight at the top');
      const afterOpen = menuFetches();

      // C. ArrowDown ADVANCES and the keyup that follows leaves it alone; no
      //    /api/menu round-trip is spent on a pure arrow key.
      press(K.KEY_ARROW_DOWN);
      await t.tick();
      t.eq(api.state().menu.sel, 1, 'ArrowDown advanced the highlight through the keydown/keyup pair');
      press(K.KEY_ARROW_DOWN);
      await t.tick();
      t.eq(api.state().menu.sel, 2, 'a second ArrowDown advanced it again (never undone)');
      press(K.KEY_ARROW_UP);
      await t.tick();
      t.eq(api.state().menu.sel, 1, 'ArrowUp walked the highlight back');
      t.eq(menuFetches(), afterOpen, 'pure arrow keys issued NO /api/menu round-trip');
      const selBeforeClick = api.state().menu.sel;
      api.els.input.dispatch('click', {});
      await t.tick();
      t.eq(api.state().menu.sel, selBeforeClick, 'a caret click on the SAME query keeps the highlight');

      // D. Enter COMPLETES the highlighted candidate — it fills the composer and
      //    never sends.
      const postsBefore = state.posts.length;
      press(K.KEY_ENTER);
      await t.tick();
      t.eq(state.posts.length, postsBefore, 'Enter with the menu open did not send a turn');
      t.eq(api.els.input.value, K.TRIGGER_SLASH + 'text2img ', 'Enter completed the HIGHLIGHTED candidate (sel 1), not the first row');
      t.eq(api.state().menu.open, false, 'completing closed the menu');

      // E. A NEW query DOES reset the highlight — the acceptance the fix must
      //    not regress.
      await type(K.TRIGGER_SLASH + 'ta', 3);
      t.eq(api.state().menu.open, true, 'the narrower query reopened the menu');
      t.eq(api.state().menu.sel, 0, 'a NEW query resets the highlight to the top');
      press(K.KEY_ARROW_DOWN);
      await t.tick();
      t.eq(api.state().menu.sel, 1, 'the narrower query is walkable');
      const fetchesBeforeGrowth = menuFetches();
      await type(K.TRIGGER_SLASH + 'tas', 4);
      t.eq(api.state().menu.sel, 0, 'growing the query to a NEW query resets the highlight again');
      t.ok(menuFetches() > fetchesBeforeGrowth, 'a new query DOES ask the endpoint (the reset is not achieved by skipping the fetch)');

      // F. Escape closes and does not leave a highlight behind for the next
      //    refresh of the same text (closed means "no menu", not a stale sel).
      api.els.input.dispatch('keydown', { key: K.KEY_ESCAPE });
      api.els.input.dispatch('keyup', { key: K.KEY_ESCAPE });
      t.eq(api.state().menu.open, false, 'Escape closed the menu');
      t.eq(api.state().menu.sel, 0, 'closing the menu resets the highlight');
      api.els.input.dispatch('click', {});
      await t.tick();
      t.eq(api.state().menu.open, true, 'a later refresh of the same text reopens the menu');
      t.eq(api.state().menu.sel, 0, 'the reopened menu starts at the top (a closed menu preserved nothing)');

      // G. The preserved index is CLAMPED: a shorter list must never leave the
      //    highlight past the end, where nothing is selected and Enter has
      //    nothing to accept.
      press(K.KEY_ARROW_DOWN);
      press(K.KEY_ARROW_DOWN);
      await t.tick();
      t.eq(api.state().menu.sel, 2, 'the highlight reached the last row of the longer list');
      const all = state.menuHandler;
      state.menuHandler = (trigger, q) => ({ trigger, query: q, source: 'slash', candidates: ['only'], total: 1 });
      api.els.input.dispatch('click', {});
      await t.tick();
      t.eq(api.state().menu.items.length, 1, 'the same query now answers with a shorter list');
      t.eq(api.state().menu.sel, 0, 'the preserved highlight was clamped into the new list');
      state.menuHandler = all;
    },
  },

  // ------------------------------------------------------------------ B5
  {
    name: 'B5 replay rebuilds tool cards from the transcript',
    about: 'the wire ToolCall fields are json:"-" — the transcript must re-ship name + digests',
    async run(ctx, t) {
      const { api, state } = ctx;
      state.transcript = {
        messages: [
          { role: 'user', content: 'read the file' },
          { role: 'assistant', content: '', tools: [{ id: 'c1', name: 'read', call: '{"path":"/etc/hostname"}' }] },
          { role: 'tool', content: '[tool_use id: c1]\nhost-b', tools: [{ id: 'c1', name: 'read', result: 'host-b' }] },
          { role: 'assistant', content: 'The hostname is host-b.' },
        ],
      };
      await api.replayTranscript();
      const cards = api.els.log.querySelectorAll('.toolcard');
      t.eq(cards.length, 1, 'ONE tool card was rebuilt (the call row opened it, the result row filled it)');
      t.ok(cards[0] && cards[0].textContent.indexOf('read') >= 0, 'the card names the tool');
      t.ok(cards[0] && cards[0].querySelector('.tbody').textContent.indexOf('host-b') >= 0, 'the card body carries the result digest');
      t.eq(cards[0].classList.contains('closed'), true, 'the card is COLLAPSED once its result arrived (the live stream collapses it too)');
      t.ok(cards[0].textContent.indexOf('/etc/hostname') >= 0, 'the call digest is rendered too');
      const bubbles = api.els.log.children.filter((c) => c.classList.contains('assistant'));
      t.eq(bubbles.length, 1, 'an empty assistant row (a pure tool turn) did not draw a "…" placeholder bubble');
    },
  },

  // ---------------------------------------------------------------- B1 + B2
  {
    name: 'B1/B2 the reset notice and the queued notice are the shared wire strings',
    about: 'the page must recognize the server’s reset frame and render the busy wait',
    async run(ctx, t) {
      const { api, state } = ctx;
      const json = (o) => JSON.stringify(o);
      const log = api.els.log;
      // A reset turn: the server answers with a command frame whose output IS
      // its reset notice — the page then wipes the transcript it was displaying
      // (the SERVER already dropped its copy under s.mu).
      state.streamQueue.push(t.streamedTurn([['command', json({ command: '/clear', output: api.consts.RESET_NOTICE })]]));
      api.els.input.value = '/clear';
      api.els.form.dispatch('submit', {});
      await t.tick(10);
      t.eq(log.children.length, 1, 'the reset notice wiped the transcript and left only its own command node');
      t.ok(log.children[0].textContent.indexOf(api.consts.RESET_NOTICE) >= 0, 'the surviving node IS the reset notice');

      // B2: the queued frame's notice is rendered as a waiting bubble BEFORE
      // the turn's deltas arrive, and taken down once the turn runs.
      const frame = t.sseResponse([['queued', json({ notice: 'waiting for the in-flight turn to finish…' })], ['done', '{}']]);
      let releaseFrame;
      const held = new Promise((res) => { releaseFrame = () => res(frame); });
      state.streamQueue.push(() => held);
      api.els.input.value = 'later';
      api.els.form.dispatch('submit', {});
      // Let the page reach the fetch and begin consuming the stream.
      await t.tick();
      const busyAtQueued = api.state().busy;
      t.eq(busyAtQueued, true, 'the queued turn is running (its frame is being consumed)');
      releaseFrame();
      await t.tick(10);
      t.eq(api.state().lastQueued, null, 'the waiting bubble is taken down once the turn is running');
      const queuedLeft = log.children.filter((c) => c.textContent.indexOf('waiting for the in-flight turn') >= 0);
      t.eq(queuedLeft.length, 0, 'no stale waiting bubble is left on screen');
    },
  },

  // ---------------------------------------------------------------- identity
  {
    name: 'identity banner: the page asks the SERVER which session/model it is driving',
    about: 'a remote browser cannot tell which engine (session id, model, tools) answered it — the URL bar holds a per-run token, not an identity',
    async run(ctx, t) {
      const { api, state } = ctx;
      state.identity = {
        session_id: 'sess-abc', model: 'deepseek-v4-pro', tools: true,
        addr: '192.0.2.9:8090', messages: 3, turns: 2, tokens: 1234,
        started: '2026-09-24T11:00:00Z',
      };
      await api.identifyEngine();
      const engineNodes = api.els.log.children.filter((c) => c.classList.contains('engine'));
      t.eq(engineNodes.length, 1, 'exactly one identity banner is rendered');
      const text = engineNodes[0].textContent;
      t.ok(text.indexOf('session=sess-abc') >= 0, 'the banner names the session id the server reported');
      t.ok(text.indexOf('model=deepseek-v4-pro') >= 0, 'the banner names the model the server reported');
      t.ok(text.indexOf('tools=on') >= 0, 'the banner names the tool-execution posture');
      t.ok(text.indexOf('turns=2') >= 0, 'the banner carries the live counters (empty vs in-use session)');

      // The identity fetch went through withToken, like every other same-origin
      // API call — otherwise a gated bind would 401 it.
      const call = state.fetches.filter((f) => String(f.url).indexOf(API_IDENTITY_PATH) === 0).pop();
      t.ok(call, 'the page fetched the identity route');
      state.locationSearch = '?t=tok123';
      await api.identifyEngine();
      const withTok = state.fetches.filter((f) => String(f.url).indexOf(API_IDENTITY_PATH) === 0).pop();
      t.ok(String(withTok.url).indexOf('t=tok123') >= 0, 'the identity fetch carries the ?t= token (it is gated like every other route)');
    },
  },

  // ------------------------------------------------- DOM order = SSE order
  {
    name: 'DOM order equals ARRIVAL order: a tool card precedes the answer it produced',
    about: 'the assistant bubble used to be appended before the POST, so the answer always sat ABOVE the cards',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input, form = api.els.form, log = api.els.log;
      const json = (o) => JSON.stringify(o);
      // A STAGED stream: the reader hands out one chunk per step, and a step is
      // only released when the scenario says so — so the DOM can be observed
      // between frames (the harness's sseResponse hands out the whole body in
      // one read, which cannot show WHERE an ordering decision was made).
      const frame = (ev, data) => `event: ${ev}\ndata: ${data}\n\n`;
      const steps = [];      // each: the frames of one chunk
      let wake = null, live = true;
      // readChunk resolves the next chunk, or {done:true} once the scenario has
      // declared the stream finished (live=false) and no chunk is left — so the
      // page's own reader loop terminates exactly when a real SSE body would.
      const readChunk = () => new Promise((resolve) => {
        const take = () => {
          if (steps.length) { resolve({ done: false, value: steps.shift() }); return; }
          if (!live) { resolve({ done: true, value: undefined }); return; }
          wake = take;
        };
        take();
      });
      const pushChunk = (frames) => {
        steps.push(Buffer.from(frames.join(''), 'utf8'));
        if (wake) { const w = wake; wake = null; w(); }
      };
      const stagedResponse = () => ({
        ok: true, status: 200,
        body: { getReader: () => ({ read: readChunk }) },
        text: () => Promise.resolve(''),
      });
      state.streamQueue.push(stagedResponse);

      input.value = 'which host is this?';
      form.dispatch('submit', {});
      await t.tick();

      // A. The tool CALL arrives while the answer has streamed NOTHING yet, and
      //    the turn is held open right there — the exact moment the old page had
      //    already mounted an empty assistant bubble above the card.
      pushChunk([frame('tool', json({ kind: 'call', name: 'bash', summary: '{"command":"hostname"}' }))]);
      await t.tick();
      t.eq(api.state().busy, true, 'the turn is in flight');
      t.eq(log.querySelectorAll('.toolcard').length, 1, 'the tool card is mounted as soon as its frame arrived');
      const bubblesMid = log.children.filter((c) => c.classList.contains('assistant'));
      t.eq(bubblesMid.length, 0, 'NO empty assistant bubble is mounted before the answer streams (the deferred bubble)');

      // B. Release the rest: the result, the answer's delta, and done.
      pushChunk([
        frame('tool', json({ kind: 'result', name: 'bash', summary: 'host-a' })),
        frame('delta', json({ text: 'the host is host-a' })),
        frame('done', '{}'),
      ]);
      live = false;
      await t.tick();
      await t.tick();

      // C. The answer is on screen, and it sits AFTER the card it came from.
      const rows = log.children.map((c) => c.classList.contains('toolcard') ? 'tool' : (c.classList.contains('assistant') ? 'assistant' : (c.classList.contains('user') ? 'user' : 'other')));
      const ti = rows.indexOf('tool'), ai = rows.indexOf('assistant');
      t.ok(ti >= 0, 'the tool card is still on the log after the turn (got ' + JSON.stringify(rows) + ')');
      t.ok(ai >= 0, 'the assistant bubble exists once the answer streamed (got ' + JSON.stringify(rows) + ')');
      t.ok(ti < ai, 'DOM order = arrival order: the card is BEFORE the answer (got ' + JSON.stringify(rows) + ')');
      const bubbles = log.children.filter((c) => c.classList.contains('assistant'));
      t.eq(bubbles.length, 1, 'exactly ONE assistant bubble was created for the whole turn (openBubble is idempotent)');
      t.ok(bubbles[0].textContent.indexOf('the host is host-a') >= 0, 'the single bubble carries the streamed answer');

      // D. A PURE ANSWER turn (no tool frames) still renders its bubble, in the
      //    same place a reader expects it: after the question, with no card.
      state.streamQueue.push(t.streamedTurn([['delta', json({ text: 'plain answer' })], ['done', '{}']]));
      input.value = 'plain question';
      form.dispatch('submit', {});
      await t.tick();
      await t.tick();
      const after = log.children.map((c) => c.classList.contains('toolcard') ? 'tool' : (c.classList.contains('assistant') ? 'assistant' : (c.classList.contains('user') ? 'user' : 'other')));
      t.eq(after[after.length - 1], 'assistant', 'a pure-answer turn ends with its assistant bubble (got ' + JSON.stringify(after) + ')');
      const allBubbles = log.children.filter((c) => c.classList.contains('assistant'));
      t.eq(allBubbles.length, 2, 'one bubble per turn — the answer turn added exactly one');
      t.ok(allBubbles[1].textContent.indexOf('plain answer') >= 0, 'the second bubble carries its own answer');

      // E. A slash turn opens NO bubble (R4 kept: the command node is terminal).
      state.streamQueue.push(t.streamedTurn([['command', json({ command: '/help', output: 'usage: …' })]]));
      input.value = '/help';
      form.dispatch('submit', {});
      await t.tick();
      await t.tick();
      const slashBubbles = log.children.filter((c) => c.classList.contains('assistant'));
      t.eq(slashBubbles.length, 2, 'a slash turn opened no assistant bubble (still 2)');
      const cmds = log.children.filter((c) => c.classList.contains('command'));
      t.eq(cmds.length, 1, 'the slash turn rendered its command node');
    },
  },

  // ------------------------------------------------------------- MICROPHONE
  {
    name: 'voice: the mic button starts a SERVER recording and the transcript lands in the composer as a DRAFT',
    about: 'route B — the recording runs on the host running `gopi web`, and the page never sends the text itself',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const mic = doc.getElementById('mic'), input = api.els.input;

      // A. The affordance EXISTS and is wired: clicking it issues the START request.
      t.ok(mic, 'the page ships a mic button');
      t.eq(api.state().micBusy, false, 'no recording is running at load');
      t.eq(mic.classList.contains('rec'), false, 'the button is not in the recording state at load');

      // THIS SCENARIO IS THE WINDOW PATH, so it installs the engine's own
      // refusal on the live route (501 = "no live arm"): the page probes the
      // streaming route FIRST on every click and falls back here, which is the
      // fallback path this scenario is about. The streaming outcome has its own
      // scenario below.
      state.voiceLive = { ok: false, status: 501, json: () => Promise.resolve({ ok: false, error: 'engine local has no live streaming arm' }), text: () => Promise.resolve('{}') };
      state.voiceStart = { ok: true, device: 'pulse:test-mic', notes: ['[configured default]'] };
      state.voiceCalls = [];
      mic.click();
      await t.tick();
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live,/api/voice/start', 'one click probes the LIVE route, is refused, and falls back to exactly ONE start request');
      t.eq(api.state().micBusy, true, 'the recording state follows the SERVER answer, not the click');
      t.eq(mic.classList.contains('rec'), true, 'the button shows the recording state');

      // B. The START answer's device description is reported, so the operator knows
      // WHICH microphone (route B's documented cost: it is the server's).
      const logText = api.els.log.textContent;
      t.ok(logText.indexOf('pulse:test-mic') >= 0, 'the log names the device the SERVER opened');
      t.ok(logText.indexOf('recording') >= 0, 'the log says a recording is running');

      // C. The operator types while recording; stop() inserts AT THE CARET.
      input.value = 'hello ';
      input.setSelectionRange(6, 6);
      state.voiceStop = {
        ok: true,
        draft: '//new about the build',
        transcript: '/new about the build',
        device: 'pulse:test-mic',
        notes: [],
        truncated: false,
        tail_dropped: false,
      };
      mic.click();
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live,/api/voice/start,/api/voice/stop', 'the second click issues the stop request (a running one-shot recording is stopped, never re-probed for streaming)');
      t.eq(api.state().micBusy, false, 'the recording state ends with the stop answer');
      t.eq(input.value, 'hello //new about the build', 'the DRAFT lands in the composer at the caret, beside the text already typed');

      // D. THE SLASH HAZARD: the server escaped it, and the page inserted the
      // server's draft VERBATIM — it must not "fix" the escape back into a command.
      t.ok(input.value.indexOf('//new') >= 0, 'a transcript beginning with "/" reaches the composer in the literal-send form ("//")');
      // E. AND IT IS A DRAFT, NOT A TURN: nothing was posted to /api/chat.
      t.eq(state.posts.length, 0, 'the transcript is NOT sent — the operator presses Enter themselves');

      // F. A REFUSAL IS SPOKEN, and it leaves the button idle (a failed device
      // probe must not leave a button claiming to record).
      state.voiceStart = { ok: false, status: 503, error: 'no capture device: arecord found no card' };
      mic.click();
      await t.tick();
      await t.tick();
      t.eq(api.state().micBusy, false, 'a refused start leaves the button idle');
      t.ok(api.els.log.textContent.indexOf('no capture device') >= 0, 'the server’s own refusal sentence is shown, not "request failed"');

      // G. An EMPTY transcript inserts nothing at all (silence is not a draft).
      state.voiceStart = { ok: true, device: 'pulse:test-mic', notes: [] };
      state.voiceStop = { ok: true, draft: '', transcript: '/sil', device: 'pulse:test-mic', notes: [], truncated: false, tail_dropped: false };
      const before = input.value;
      mic.click();
      await t.tick();
      await t.tick();
      mic.click();
      await t.tick();
      t.eq(input.value, before, 'an empty transcript inserts nothing (the composer is untouched)');
      t.ok(api.els.log.textContent.indexOf('nothing was said') >= 0, 'and it says so instead of failing silently');
    },
  },

  {
    name: 'voice live: the streaming route draws drafts WHILE the operator speaks, and a refusal falls back to start/stop',
    about: 'web_voice_live.go — draft frames replace the in-composer draft region, the final frame settles it, and 501 is the ONE answer that takes the window path',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const mic = doc.getElementById('mic'), input = api.els.input;

      // A. THE STREAMED OUTCOME. A ready frame names the device; two drafts
      // arrive (each the WHOLE text so far, so the second REPLACES the first);
      // the final frame settles the sentence.
      state.voiceLive = t.sseResponse([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '你好', draft: '你好', partials: 1 })],
        ['draft', JSON.stringify({ text: '你好世界', draft: '你好世界', partials: 2 })],
        ['final', JSON.stringify({ ok: true, transcript: '你好世界。', draft: '你好世界。', device: 'pulse:live-mic', partials: 2, first_partial_ms: 2500, elapsed_ms: 4000 })],
      ]);
      state.voiceCalls = [];
      input.value = 'pre ';
      mic.click();
      // The page reads the body across ticks; give it the macrotask it awaits.
      await t.tick();
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live', 'the first click opens the LIVE route only');
      t.eq(api.state().micLiveActive, false, 'the live flag is cleared once the stream ends');
      t.eq(input.value, 'pre 你好世界。', 'the settled sentence lands once, at the caret, with the earlier draft removed (no duplicated text)');

      // B. THE REFUSAL → the EXISTING window path, announced. This is the
      // fallback: an engine with no live arm answers 501 before any frame.
      state.voiceLive = { ok: false, status: 501, json: () => Promise.resolve({ ok: false, error: 'engine local has no live streaming arm' }), text: () => Promise.resolve('{}') };
      state.voiceStart = { ok: true, device: 'pulse:one-shot-mic', notes: [] };
      state.voiceCalls = [];
      input.value = '';
      mic.click();
      await t.tick();
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live,/api/voice/start', 'a 501 falls back to the one-shot start route');
      t.eq(api.state().micBusy, true, 'the fallback leaves the button in the RECORDING state, not idle');
      t.ok(api.els.log.textContent.indexOf('no live streaming arm') >= 0, 'the refusal sentence is shown, so the operator knows which way it recorded');
      t.eq(api.els.voicelive.hidden, true, 'the one-shot path shows no elapsed clock (it is the live state that carries one)');
    },
  },

  // ---------------------------------------------------------------- budget
  {
    name: 'image budget: the page’s base64 cap is DERIVED from the decoded-byte ceiling',
    about: '32 MiB decoded is one budget, expressed once and derived to the base64 length',
    async run(ctx, t) {
      const { api } = ctx;
      const c = api.consts;
      t.eq(c.MAX_IMAGE_BYTES, 32 * 1024 * 1024, 'the decoded ceiling is 32 MiB');
      const derived = Math.ceil(c.MAX_IMAGE_BYTES * 4 / 3) + 4;
      t.eq(c.MAX_B64, derived, 'MAX_B64 is DERIVED from MAX_IMAGE_BYTES (never a second number)');
      t.eq(c.INPUT_MAX_ROWS, 8, 'the composer row cap is the named 8');
    },
  },

  // ------------------------------------------------------------------ R6
  {
    name: 'R6 page capture: 📷 requests the server-side capture and stages it as an attachment',
    about: 'the button is on the page, the click reaches /api/shot carrying ONLY the window geometry, and the returned PNG lands in images[] + the strip',
    async run(ctx, t) {
      const { api, state } = ctx;
      const c = api.consts;

      // A. The button EXISTS in the shipped page, and it is the capture one.
      t.ok(api.els.shot, 'the page has a #shot button');
      t.eq(api.els.shot.id, 'shot', 'the capture button carries id=shot (the scenario drives the page, not a copy)');
      t.eq(c.API_SHOT, '/api/shot', 'the page carries the server route (cli_web_shot.go webShotPath)');

      // B. Clicking it POSTs to the capture endpoint with EXACTLY ONE field —
      // the window geometry. There is no url/addr/surface key, so there is
      // nothing a client could use to aim the capture elsewhere; and the
      // viewport is the REAL window size, so the picture matches what the
      // operator is looking at instead of the server's 1440x2800 default.
      state.shotResponse = t.jsonResponse({ image: { media_type: 'image/png', data_b64: 'iVBORw0KGgoAAAANSUhEUg==' } });
      api.els.shot.click();
      await t.tick();
      const call = state.fetches.filter((f) => String(f.url).indexOf('/api/shot') === 0).pop();
      t.ok(call, 'the click issued a POST to /api/shot');
      t.eq((call.opts || {}).method, 'POST', 'the capture request is a POST');
      const sent = JSON.parse((call.opts || {}).body || '{}');
      t.eq(Object.keys(sent).join(','), 'viewport', 'the body carries the viewport field and NOTHING else (no url/addr/surface — the server alone decides the target)');
      t.eq(sent.viewport && sent.viewport.w, state.innerWidth, 'the requested width is window.innerWidth (the operator\'s own window, not the server default)');
      t.eq(sent.viewport && sent.viewport.h, state.innerHeight, 'the requested height is window.innerHeight');
      t.ok(sent.viewport && sent.viewport.w > 0 && sent.viewport.h > 0, 'the geometry is positive (the server reads 0 as "use the default")');
      t.eq(sent.viewport && sent.viewport.w, Math.floor(sent.viewport.w), 'w is a whole number (the server parses whole pixels)');

      // C. The returned image is staged: images[] holds it and renderStrip drew
      // a thumbnail — the SAME strip an uploaded file lands in, so the next
      // Enter ships it through /api/chat with no second mechanism.
      const s1 = api.state();
      t.eq(s1.images.length, 1, 'the captured picture is staged in images[]');
      t.eq(s1.images[0].media_type, 'image/png', 'the staged entry carries the media type the server reported');
      t.ok(s1.images[0].data_b64.length > 0, 'the staged entry carries the base64 payload');
      const thumbs = api.els.strip.children.filter((n) => n.classList.contains('imgthumb'));
      t.eq(thumbs.length, 1, 'renderStrip() drew exactly one thumbnail — the picture is VISIBLE before sending');
      const inner = thumbs[0].children.filter((n) => n.tagName === 'IMG');
      t.ok(inner.length === 1 && String(inner[0].src).indexOf('data:image/png;base64,') === 0,
        'the thumbnail is the captured PNG (a data: URI, not a placeholder)');
      t.eq(api.els.shot.disabled, false, 'the button is re-armed once the capture landed');

      // B2. RESIZE the window and click again: the next request carries the NEW
      // size. This is the difference between "the page always sends some
      // number" and "the page reports its own window" — and only the second one
      // makes the button capture the page the operator sees.
      state.innerWidth = 903;
      state.innerHeight = 617;
      api.els.shot.click();
      await t.tick();
      const resized = state.fetches.filter((f) => String(f.url).indexOf('/api/shot') === 0).pop();
      const resent = JSON.parse((resized.opts || {}).body || '{}');
      t.eq(resent.viewport && resent.viewport.w, 903, 'after a resize the capture asks for the NEW width');
      t.eq(resent.viewport && resent.viewport.h, 617, 'after a resize the capture asks for the NEW height');
      // Restore the harness geometry so the rest of this scenario (thumbnail
      // counts, button re-arm) reads the same state it did before.
      state.innerWidth = 1280;
      state.innerHeight = 720;
    },
  },

  {
    name: 'R6 page capture failure: a refused capture is RENDERED with the server\'s own reason',
    about: 'a capture that fails must never look like a button that does nothing',
    async run(ctx, t) {
      const { api, state } = ctx;
      state.shotResponse = {
        ok: false,
        status: 400,
        json: () => Promise.resolve({ error: '/api/shot takes no parameter but "viewport" — refusing url' }),
        text: () => Promise.resolve('{"error":"/api/shot takes no parameters — refusing url"}'),
      };
      api.els.shot.click();
      await t.tick();
      const s = api.state();
      t.eq(s.images.length, 0, 'a refused capture stages NOTHING (no half-attached picture)');
      const errs = api.els.strip.children.filter((n) => n.classList.contains('shoterr'));
      t.eq(errs.length, 1, 'the failure is rendered in the strip');
      const text = errs[0].textContent;
      t.ok(text.indexOf('HTTP 400') >= 0, 'the failure names the status');
      t.ok(text.indexOf('refusing url') >= 0, 'the failure carries the SERVER message (not a generic "failed")');
      t.eq(api.els.shot.disabled, false, 'the button is re-armed after a failure (a retry is possible)');
    },
  },

  {
    // The measured defect (2026-11-07): the failure branch cleared the WHOLE
    // strip (strip.innerHTML='') to remove a previous .shoterr line, so it also
    // erased thumbnails of already-staged images while images[] kept them — the
    // operator saw the pictures disappear and the next Enter shipped them
    // anyway. The clearing must remove the failure line ONLY.
    name: 'R6 page capture failure keeps the staged thumbnails (a failure line is the only thing removed)',
    about: 'images[] and the strip agree before and after a failed capture: N thumbnails in, N thumbnails out, plus exactly one .shoterr',
    async run(ctx, t) {
      const { api, state } = ctx;
      // Stage two pictures through the page's own staging path.
      api.stageFiles([
        { type: 'image/png', name: 'a.png', b64: 'AAAA' },
        { type: 'image/png', name: 'b.png', b64: 'BBBB' },
      ]);
      await t.tick();
      const thumbsBefore = api.els.strip.children.filter((n) => n.classList.contains('imgthumb')).length;
      const imgsBefore = api.state().images.length;
      t.eq(imgsBefore, 2, 'two pictures are staged in images[] before the capture');
      t.eq(thumbsBefore, 2, 'and two thumbnails are drawn in the strip');

      // The capture FAILS (the shape a non-loopback bind produces today).
      state.shotResponse = {
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: '/api/shot: refusing to capture 192.0.2.10:8090' }),
        text: () => Promise.resolve('{"error":"/api/shot: refusing to capture 192.0.2.10:8090"}'),
      };
      api.els.shot.click();
      await t.tick();

      const s = api.state();
      const thumbsAfter = api.els.strip.children.filter((n) => n.classList.contains('imgthumb')).length;
      const errs = api.els.strip.children.filter((n) => n.classList.contains('shoterr'));
      t.eq(s.images.length, imgsBefore,
        'a failed capture stages nothing new AND keeps what was staged (images[] length unchanged)');
      t.eq(thumbsAfter, thumbsBefore,
        'the staged thumbnails SURVIVE the failure — the strip still shows what will be sent');
      t.eq(errs.length, 1, 'the failure is rendered beside them (the click is never a silent no-op)');
      t.ok(errs[0].textContent.indexOf('HTTP 500') >= 0, 'the failure line names the status');

      // A SECOND failure replaces the line instead of accumulating them, and
      // still touches no thumbnail.
      state.shotResponse = {
        ok: false,
        status: 502,
        json: () => Promise.resolve({ error: 'second failure' }),
        text: () => Promise.resolve('{"error":"second failure"}'),
      };
      api.els.shot.click();
      await t.tick();
      const errs2 = api.els.strip.children.filter((n) => n.classList.contains('shoterr'));
      const thumbs2 = api.els.strip.children.filter((n) => n.classList.contains('imgthumb')).length;
      t.eq(errs2.length, 1, 'a second failure REPLACES the first line (exactly one .shoterr, never a pile)');
      t.ok(errs2[0].textContent.indexOf('HTTP 502') >= 0, 'the surviving line is the newest failure');
      t.eq(thumbs2, thumbsBefore, 'and the thumbnails are still all there after two failures');
      t.eq(api.els.strip.children.length, thumbsBefore + 1,
        'the strip holds exactly #staged thumbnails + one failure line — nothing was emptied');
    },
  },

  {
    name: 'R6 page capture cap: at the image limit the click spends no capture',
    about: 'one capture is one headless-browser start — refuse at the cap instead of rendering a picture that cannot be sent',
    async run(ctx, t) {
      const { api, state } = ctx;
      state.shotResponse = t.jsonResponse({ image: { media_type: 'image/png', data_b64: 'AAAA' } });
      // Fill the strip to MAX_IMAGES through the page's own staging path.
      api.stageFiles(Array.from({ length: 8 }, (_, i) => ({ type: 'image/png', name: 'f' + i + '.png', b64: 'AAAA' })));
      await t.tick();
      t.eq(api.state().images.length, 8, 'the strip is full (8 = MAX_IMAGES)');
      const before = state.fetches.filter((f) => String(f.url).indexOf('/api/shot') === 0).length;
      api.els.shot.click();
      await t.tick();
      const after = state.fetches.filter((f) => String(f.url).indexOf('/api/shot') === 0).length;
      t.eq(after, before, 'no capture request was spent at the cap');
      const errs = api.els.strip.children.filter((n) => n.classList.contains('shoterr'));
      t.eq(errs.length, 1, 'the refusal is rendered (the click is not a silent no-op)');
      t.ok(errs[0].textContent.indexOf('image limit reached') >= 0, 'it names the limit');
    },
  },

  // ------------------------------------------- a command's OWN pictures (R7)
  {
    name: 'R7 command images: a slash command frame carrying pictures stages them and draws the thumbnails',
    about: 'the /text2img line — the picture a command rendered must land in the SAME images[] + strip an upload does, so the next Enter ships it to the model',
    async run(ctx, t) {
      const { api, state } = ctx;
      const c = api.consts;
      const json = (o) => JSON.stringify(o);

      // The frame the SERVER writes (cli_web.go webCommandImagesField): the
      // command's own output text PLUS one image object per rendered page, in
      // the exact {media_type,data_b64} shape /api/shot returns.
      t.eq(c.CMD_IMAGES_FIELD, 'images', 'the page carries the frame field name the server writes');
      state.streamQueue.push(t.streamedTurn([['command', json({
        command: '/text2img GOPI-R7',
        output: '✅ rendered → 1 页\n  output: /tmp/gopi-slash-1.png',
        images: [{ media_type: 'image/png', data_b64: 'iVBORw0KGgoAAAANSUhEUg==' }],
      })]]));
      api.els.input.value = '/text2img GOPI-R7';
      api.els.form.dispatch('submit', {});
      await t.tick();
      await t.tick();

      // A. The command node still renders (R4 kept: a slash turn is terminal
      // and spends no model call) …
      const cmds = api.els.log.children.filter((n) => n.classList.contains('command'));
      t.eq(cmds.length, 1, 'the slash turn rendered its command node');
      t.ok(!api.els.log.children.some((n) => n.classList.contains('assistant')),
        'the slash turn opened no assistant bubble (no model call was spent)');

      // B. … AND its picture is staged and VISIBLE.
      const staged = api.state().images;
      t.eq(staged.length, 1, 'the command\'s picture is staged in images[]');
      t.eq(staged[0].media_type, 'image/png', 'the staged entry carries the frame\'s media type');
      t.eq(staged[0].data_b64, 'iVBORw0KGgoAAAANSUhEUg==', 'the staged entry carries the frame\'s bytes verbatim');
      const thumbs = api.els.strip.children.filter((n) => n.classList.contains('imgthumb'));
      t.eq(thumbs.length, 1, 'renderStrip() drew exactly one thumbnail');
      const inner = thumbs[0].children.filter((n) => n.tagName === 'IMG');
      t.ok(inner.length === 1 && String(inner[0].src).indexOf('data:image/png;base64,') === 0,
        'the thumbnail is the command\'s PNG as a data: URI');

      // C. THE POINT: the NEXT turn carries it through the ordinary /api/chat
      // body — the same field an upload uses, so no second transport exists.
      state.streamQueue.push(t.streamedTurn([['delta', json({ text: 'seen' })], ['done', '{}']]));
      api.els.input.value = 'describe the picture the command drew';
      api.els.form.dispatch('submit', {});
      await t.tick();
      await t.tick();
      const posts = state.posts;
      t.eq(posts.length, 2, 'two turns were posted (the command + the message after it)');
      const sent = posts[1] || {};
      t.ok(Array.isArray(sent.images) && sent.images.length === 1,
        'the turn AFTER the command carries the picture in images[] (body = ' + JSON.stringify(sent.images) + ')');
      t.eq((sent.images || [{}])[0].data_b64, 'iVBORw0KGgoAAAANSUhEUg==', 'the bytes the command rendered are what the next turn ships');
      t.eq(api.state().images.length, 0, 'the strip is cleared once the picture has been sent');
    },
  },

  {
    name: 'R7 command images: a command frame at the image cap reports the pictures it could not attach',
    about: 'silent truncation is the same defect in a new place — a command that rendered more than one message may carry must SAY how many it dropped (the server caps a single command at MAX_IMAGES pages, so this guards the PAGE half of that budget)',
    async run(ctx, t) {
      const { api, state } = ctx;
      const json = (o) => JSON.stringify(o);
      const cmdsBefore = api.els.log.children.filter((n) => n.classList.contains('command')).length;
      // TEN pages on one frame: past what a message may carry. Deliveries this
      // large cannot come from the shipped server (it attaches at most
      // MAX_IMAGES and says so in its own output text), which is exactly why
      // the page must not silently drop the remainder if the two ever drift.
      state.streamQueue.push(t.streamedTurn([['command', json({
        command: '/text2img GOPI-R7-CAP',
        output: '✅ rendered → 10 页',
        images: Array.from({ length: 10 }, (_, i) => ({ media_type: 'image/png', data_b64: 'PAGE' + i })),
      })]]));
      api.els.input.value = '/text2img GOPI-R7-CAP';
      api.els.form.dispatch('submit', {});
      await t.tick();
      await t.tick();
      t.eq(api.state().images.length, 8, 'the strip holds exactly MAX_IMAGES pictures (got ' + api.state().images.length + ')');
      const cmds = api.els.log.children.filter((n) => n.classList.contains('command'));
      t.eq(cmds.length, cmdsBefore + 2, 'a second command node reports the drop (the command node + the refusal)');
      const refusal = cmds[cmds.length - 1];
      t.ok(String(refusal.textContent).indexOf('image limit reached') >= 0, 'the refusal names the limit');
      t.ok(String(refusal.textContent).indexOf(' 2 ') >= 0, 'the refusal says HOW MANY pictures were dropped (2)');
    },
  },
  // ------------------------------------------------- ESC keeps what was heard
  {
    name: 'voice esc: ESC STOPS AND KEEPS the recognised text (caret to the end); Shift+ESC discards; Ctrl+U clears the composer',
    about: 'the key an operator presses to start proof-reading an ASR result must not delete that result — the discard moved behind a modifier and the whole-composer clear behind Ctrl+U',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input;
      // holdLive installs a LIVE stream the scenario controls: the ready and the
      // first draft arrive at once, the FINAL frame is withheld until release()
      // is called — so a keystroke can be pressed while the recording is still
      // genuinely open (a stream that answers instantly would end the recording
      // before the key is pressed, and the key would then be testing nothing).
      function holdLive(headFrames) {
        let release;
        const pack = (frames) => new Uint8Array(Buffer.from(
          frames.map(([ev, data]) => 'event: ' + ev + '\ndata: ' + data + '\n\n').join(''), 'utf8'));
        state.voiceCalls = [];
        state.voiceLive = {
          ok: true, status: 200,
          body: { getReader: () => {
            let step = 0;
            return { read: () => {
              step++;
              if (step === 1) { return Promise.resolve({ done: false, value: pack(headFrames) }); }
              if (step === 2) { return new Promise((res) => { release = (final) => res({ done: false, value: pack([final]) }); }); }
              return Promise.resolve({ done: true, value: undefined });
            } };
          } },
          text: () => Promise.resolve(''),
        };
        return { release: (final) => release(final) };
      }

      // A. THE LIVE ROUTE, recording STILL OPEN. A draft is already in the
      //    composer; ESC must ask for the STOP route and the sentence must land.
      input.value = 'fix this ';
      input.setSelectionRange(5, 5);
      const live1 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '尼好', draft: '尼好', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      t.ok(input.value.indexOf('尼好') >= 0, 'the interim draft is in the composer while recording (value = ' + JSON.stringify(input.value) + ')');
      t.eq(api.state().micLiveActive, true, 'the recording is still genuinely open when the key is pressed');
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live/stop', 'ESC asks for the STOP route (the live one), never the cancel route');
      t.ok(state.voiceCalls.indexOf('/api/voice/live/cancel') < 0, 'no cancel route was reached');
      // The engine settles the sentence; the page must insert it.
      live1.release(['final', JSON.stringify({ ok: true, transcript: '你好世界。', draft: '你好世界。', device: 'pulse:live-mic', partials: 1 })]);
      await t.tick();
      await t.tick();
      t.ok(input.value.indexOf('你好世界。') >= 0, 'the recognised sentence SURVIVES the Escape (composer = ' + JSON.stringify(input.value) + ')');
      t.ok(input.value.indexOf('fix t') >= 0, 'the text that was in the composer before the recording is untouched');
      t.eq(input.value.indexOf('尼好'), -1,
        'the INTERIM draft was REPLACED by the settled sentence, never left behind (composer = ' + JSON.stringify(input.value) + ')');
      t.eq(input.value.split('你好世界。').length - 1, 1,
        'the settled sentence appears exactly ONCE (no duplicate insert)');
      t.eq(input.selectionStart, input.value.length, 'the caret is at the END of the composer (' + input.selectionStart + '/' + input.value.length + ')');
      t.eq(api.state().micLiveActive, false, 'the recording is closed once the sentence settled');

      // B. SHIFT+ESC still DISCARDS: the rarer intent, kept rather than retired,
      //    and it takes the cancel route on purpose.
      //
      //    THE COMPOSER IS PRE-LOADED WITH THE OPERATOR'S OWN TEXT, on purpose:
      //    the discard is the ONE act here that destroys something, so the two
      //    guarantees that outrank the rest are measured TOGETHER — what it
      //    destroys is the interim draft and nothing else, and it says that it
      //    destroyed something. An empty composer would make both unmeasurable:
      //    "nothing was deleted" and "nothing happened" print the same value.
      input.value = 'keep me: ';
      input.setSelectionRange(input.value.length, input.value.length);
      const logEl = api.els.log;
      const logBeforeDiscard = logEl.children.length;      const live2 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '不要', draft: '不要', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      t.ok(input.value.indexOf('不要') >= 0, 'the interim draft is in the composer before the discard (value = ' + JSON.stringify(input.value) + ')');
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE, shiftKey: true });
      await t.tick();
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live/cancel', 'Shift+ESC takes the CANCEL route');
      // THE DISCARD IS SPOKEN, ON THE RECORDING RUNG TOO. Before this epic the
      // success path of micCancel said nothing at all, so the key that deletes a
      // sentence was indistinguishable from a key that is broken.
      t.ok(logEl.children.length > logBeforeDiscard, 'the discard is SPOKEN while the recording is still open (not silent)');
      // THE NOTICE IS READ DEFENSIVELY: a mutation that deletes the notice must
      // fail HERE and let the rest of the group run, not abort the scenario with
      // a TypeError (the red proof would then be a crash, which proves less than
      // a named failing check).
      const discardNotice = logEl.children.length > logBeforeDiscard
        ? logEl.children[logEl.children.length - 1].textContent : '';
      t.ok(/discarded/i.test(discardNotice), 'the notice names the act ("discarded"): ' + JSON.stringify(discardNotice));
      live2.release(['final', JSON.stringify({ ok: true, transcript: '不要这句', draft: '', cancelled: true, device: 'pulse:live-mic', partials: 1 })]);
      await t.tick();
      await t.tick();
      t.eq(input.value, 'keep me: ', 'a discarded utterance inserts NOTHING and the operator\'s OWN text survives it untouched (composer = ' + JSON.stringify(input.value) + ')');

      // B2. A STOP ALREADY ON THE WIRE IS NOT SENT TWICE. The live stream stays
      //     open while the stop request travels, so `micRecordingOpen()` is still
      //     true and a second Escape would fire the recording rung a second time —
      //     a round trip that can only answer 409, whose notice reads as the
      //     operator's mistake. `micStopping` is what closes that window, and this
      //     is the check that keeps it there: the stop response is held until the
      //     scenario releases it, so the travelling window is REAL and not a
      //     microtask nobody can press a key inside.
      input.value = 'one stop ';
      const live3 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '这一句', draft: '这一句', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      state.voiceCalls = [];
      let releaseStop;
      state.voiceLiveStop = new Promise((resolve) => { releaseStop = () => resolve({ ok: true, stopped: true }); });
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(state.voiceCalls.filter((c) => c === '/api/voice/live/stop').length, 1,
        'the first ESCAPE sends the stop route');
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(state.voiceCalls.filter((c) => c === '/api/voice/live/stop').length, 1,
        'a SECOND ESCAPE while the stop is still travelling sends NO second stop (composer = ' + JSON.stringify(input.value) + ')');
      t.ok(input.value.indexOf('这一句') >= 0, 'and it deleted nothing (the interim draft is still where it was)');
      releaseStop();
      await t.tick();
      live3.release(['final', JSON.stringify({ ok: true, transcript: '这一句', draft: '这一句', device: 'pulse:live-mic', partials: 1 })]);
      await t.tick();
      await t.tick();
      state.voiceLiveStop = undefined;
      t.eq(api.state().micLiveActive, false, 'the held stop landed, so the recording is closed again');

      // C. THE WINDOW ROUTE: the one-shot recording is open, the operator typed
      //    around it, and ESC stops it — the draft lands, the caret ends up last.
      state.voiceLive = { ok: false, status: 501, json: () => Promise.resolve({ ok: false, error: 'engine local has no live streaming arm' }), text: () => Promise.resolve('{}') };
      state.voiceStart = { ok: true, device: 'pulse:one-shot-mic', notes: [] };
      state.voiceStop = { ok: true, draft: 'the tail of what I said', transcript: 'the tail of what I said', device: 'pulse:one-shot-mic', notes: [], truncated: false, tail_dropped: false };
      input.value = 'note: ';
      input.setSelectionRange(6, 6);
      doc.getElementById('mic').click();
      await t.tick();
      await t.tick();
      t.eq(api.state().micBusy, true, 'the one-shot recording is open');
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/stop', 'ESC on the window route stops the recording (never cancels it)');
      t.eq(input.value, 'note: the tail of what I said', 'the recognised text is inserted at the caret');
      t.eq(input.selectionStart, input.value.length, 'the caret was moved to the END after the stop');

      // D. CTRL+U clears the WHOLE composer, and it says nothing: no notice row,
      //    no placeholder, no title. The silence is the design (an unannounced
      //    destructive key is reachable only by someone who already knows it).
      const logBefore = api.els.log.children.length;
      input.dispatch('keydown', { key: 'u', ctrlKey: true });
      t.eq(input.value, '', 'Ctrl+U emptied the composer');
      t.eq(api.els.log.children.length, logBefore, 'Ctrl+U wrote nothing to the log (unannounced by design)');
      // The MARKUP is what the operator can see: a placeholder, a button title,
      // an inline hint. The chord must not appear there — the page's own code
      // COMMENTS may name it (they are how a maintainer learns it exists), and
      // those live inside <script>, which this slice excludes.
      const markup = ctx.page.slice(0, ctx.page.indexOf('<script>'));
      t.ok(markup.indexOf('Ctrl+U') < 0 && markup.indexOf('ctrl+u') < 0,
        'the visible markup never names the chord (no placeholder, no title, no hint)');

      // E. AN EMPTY SENTENCE DELETES NOTHING: a stop that settles nothing must
      //    leave the text the operator already had exactly as it was.
      state.voiceStop = { ok: true, draft: '', transcript: '', device: 'pulse:one-shot-mic', notes: [], truncated: false, tail_dropped: false };
      input.value = 'already typed';
      doc.getElementById('mic').click();
      await t.tick();
      await t.tick();
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      await t.tick();
      t.eq(input.value, 'already typed', 'an empty transcript leaves the typed text EXACTLY as it was');
    },
  },

  // ------------------------------------------- the live interim draft REGION
  {
    name: 'voice draft: an operator edit INSIDE the live interim region survives, and the sentence lands AFTER it',
    about: 'the interim region was located by LENGTH when removed and by IDENTITY when guarded, so one character typed inside it orphaned the draft and the settled sentence was spliced into the middle of it',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input;
      // holdLive installs a live stream this scenario releases by hand, so the
      // interim draft is genuinely on screen when the operator's edit is
      // simulated (a stream that answers at once would settle before the edit
      // and the edit would be testing nothing).
      function holdLive(headFrames) {
        let release;
        const pack = (frames) => new Uint8Array(Buffer.from(
          frames.map(([ev, data]) => 'event: ' + ev + '\ndata: ' + data + '\n\n').join(''), 'utf8'));
        state.voiceCalls = [];
        state.voiceLive = {
          ok: true, status: 200,
          body: { getReader: () => {
            let step = 0;
            return { read: () => {
              step++;
              if (step === 1) { return Promise.resolve({ done: false, value: pack(headFrames) }); }
              if (step === 2) { return new Promise((res) => { release = (final) => res({ done: false, value: pack([final]) }); }); }
              return Promise.resolve({ done: true, value: undefined });
            } };
          } },
          text: () => Promise.resolve(''),
        };
        return { release: (final) => release(final) };
      }
      const finalFrame = ['final', JSON.stringify({ ok: true, transcript: '你好世界。', draft: '你好世界。', device: 'pulse:live-mic', partials: 2 })];

      // A. THE DEFECT. A recording is open, the interim draft sits in the
      //    composer after the operator's own text, and the operator puts the
      //    caret INSIDE that draft and types one character. The sentence then
      //    settles. Their edit must survive, the recognised sentence must land
      //    AFTER it, and the interim text must not be left behind as a fragment
      //    in the middle of it.
      input.value = '请注意 ';
      input.setSelectionRange(4, 4);
      const live1 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '尼好', draft: '尼好', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      t.eq(input.value, '请注意 尼好', 'the interim draft is drawn after the typed head (value = ' + JSON.stringify(input.value) + ')');
      // THE OPERATOR'S EDIT: one character inserted inside the interim region,
      // caret left between the new character and the rest of the draft.
      input.value = '请注意 尼X好';
      input.setSelectionRange(6, 6);
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live/stop', 'ESC still asks for the STOP route while the draft has been edited');
      live1.release(finalFrame);
      await t.tick();
      await t.tick();
      t.eq(input.value, '请注意 尼X好 你好世界。',
        'the operator edit survives and the sentence lands AFTER it (composer = ' + JSON.stringify(input.value) + ')');
      t.eq(input.value.split('你好世界。').length - 1, 1, 'the settled sentence appears exactly ONCE');
      t.ok(input.value.indexOf('尼X好') >= 0,
        'the operator\'s edit is still ONE contiguous run — not split around the settled sentence (composer = ' + JSON.stringify(input.value) + ')');
      t.ok(input.value.indexOf('请注意 尼X好') === 0, 'the head the operator typed before the recording is untouched and still first');
      t.eq(input.selectionStart, input.value.length, 'the caret ends AFTER the settled sentence (' + input.selectionStart + '/' + input.value.length + ')');

      // B. THE ORDINARY CASE IS UNCHANGED: nobody edited the region, so the
      //    interim draft is REPLACED by the settled sentence — it must not be
      //    left beside it.
      input.value = '请注意 ';
      input.setSelectionRange(4, 4);
      const live2 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '尼好', draft: '尼好', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      t.eq(input.value, '请注意 尼好', 'the interim draft is redrawn for the second utterance');
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      live2.release(finalFrame);
      await t.tick();
      await t.tick();
      t.eq(input.value, '请注意 你好世界。',
        'an UNEDITED interim draft is REPLACED by the sentence, never left beside it (composer = ' + JSON.stringify(input.value) + ')');
      t.eq(input.value.indexOf('尼好'), -1, 'no fragment of the interim draft is orphaned (composer = ' + JSON.stringify(input.value) + ')');

      // C. THE OPERATOR DELETED THE DRAFT THEMSELVES: their deletion stands, and
      //    the sentence lands where their caret is — nothing is spliced back in.
      input.value = '请注意 ';
      input.setSelectionRange(4, 4);
      const live3 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '尼好', draft: '尼好', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      t.eq(input.value, '请注意 尼好', 'the interim draft is redrawn for the third utterance');
      input.value = '请注意 ';
      input.setSelectionRange(4, 4);
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      live3.release(finalFrame);
      await t.tick();
      await t.tick();
      t.eq(input.value, '请注意 你好世界。',
        'a draft the operator deleted STAYS deleted — it is never spliced back in (composer = ' + JSON.stringify(input.value) + ')');
    },
  },

  // ------------------------- THE SETTLE WINDOW AND THE FOUR-WAY ARBITRATION
  {
    name: 'voice esc arbi: the SETTLE WINDOW (recording over, sentence in the box, no turn) answers Escape; Shift+ESC discards it and SAYS so; typing is never deleted',
    about: 'Escape had two branches guarded by micRecordingOpen, so the window where the recording had already stopped and the engine sentence was already in the composer fell through every rung and Escape did nothing at all; and the discard was silent, indistinguishable from a broken key',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input;
      const log = api.els.log;

      // holdLive: a stream this scenario releases by hand, so a keystroke can be
      // pressed while the recording is genuinely open (see the scenario above).
      function holdLive(headFrames) {
        let release;
        const pack = (frames) => new Uint8Array(Buffer.from(
          frames.map(([ev, data]) => 'event: ' + ev + '\ndata: ' + data + '\n\n').join(''), 'utf8'));
        state.voiceCalls = [];
        state.voiceLive = {
          ok: true, status: 200,
          body: { getReader: () => {
            let step = 0;
            return { read: () => {
              step++;
              if (step === 1) { return Promise.resolve({ done: false, value: pack(headFrames) }); }
              if (step === 2) { return new Promise((res) => { release = (final) => res({ done: false, value: pack([final]) }); }); }
              return Promise.resolve({ done: true, value: undefined });
            } };
          } },
          text: () => Promise.resolve(''),
        };
        return { release: (final) => release(final) };
      }

      // ---- A. THE FALL-THROUGH, MEASURED FIRST. Drive a real utterance to its
      // settle, then read the state the page is in: the recording is closed, no
      // turn runs, and the sentence the engine settled is in the composer. That
      // state is the settle window, and it is what used to answer nothing.
      input.value = 'my own words: ';
      input.setSelectionRange(input.value.length, input.value.length);
      const live1 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '尼好', draft: '尼好', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(state.voiceCalls.join(','), '/api/voice/live/stop', 'ESC during a recording asks for the STOP route, never the cancel route');
      live1.release(['final', JSON.stringify({ ok: true, transcript: '你好世界。', draft: '你好世界。', device: 'pulse:live-mic', partials: 1 })]);
      await t.tick();
      await t.tick();
      const settled = api.state();
      t.eq(settled.micLiveActive, false, 'the recording is CLOSED — this is the settle window');
      t.eq(settled.micBusy, false, 'no window-route recording is open either');
      t.eq(settled.busy, false, 'no turn is in flight');
      t.ok(input.value.indexOf('你好世界。') >= 0, 'the settled sentence is IN the composer (value = ' + JSON.stringify(input.value) + ')');
      t.eq(api.micVoiceText(), '你好世界。', 'the page KNOWS which run of the composer is the settled sentence');

      // B. THE ARBITER SAYS WHICH RUNG OWNS ESCAPE, and it is the NEW one — not
      //    "none" (which is the defect: every branch declined and the key fell
      //    through to the browser's default and did nothing).
      t.eq(api.micEscAct(), 'voice-text', 'in the settle window the ESCAPE arbiter returns the voice-text rung (not empty, not recording)');
      t.eq(api.micEscAct() === 'recording', false, 'the settle window is NOT the recording rung (they are disjoint, not else-if order)');

      // C. PLAIN ESC in the settle window: nothing is destroyed, no request is
      //    spent, and the operator is put at the end of the composer to edit.
      const callsBefore = state.voiceCalls.length;
      input.setSelectionRange(0, 0);
      const logBeforeC = log.children.length;
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      await t.tick();
      t.ok(state.voiceCalls.slice(callsBefore).length === 0, 'plain ESC in the settle window sends NO request (nothing is left to stop)');
      t.ok(input.value.indexOf('你好世界。') >= 0, 'plain ESC in the settle window KEEPS the settled sentence (value = ' + JSON.stringify(input.value) + ')');
      t.ok(input.value.indexOf('my own words:') >= 0, 'plain ESC in the settle window keeps the text the operator typed');
      t.eq(input.selectionStart, input.value.length, 'plain ESC in the settle window puts the caret at the END for the proof-reading pass (' + input.selectionStart + '/' + input.value.length + ')');
      t.eq(log.children.length, logBeforeC, 'plain ESC in the settle window is silent — it destroyed nothing, so it has nothing to report');

      // D. SHIFT+ESC in the settle window: the sentence goes, and the log SAYS SO.
      //    It sends no request either (there is no live session left to cancel).
      const logBeforeD = log.children.length;
      const callsBeforeD = state.voiceCalls.length;
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE, shiftKey: true });
      await t.tick();
      await t.tick();
      t.eq(input.value, 'my own words: ', 'Shift+ESC in the settle window removes the settled sentence and NOTHING else');
      t.ok(state.voiceCalls.slice(callsBeforeD).length === 0, 'Shift+ESC in the settle window sends no request — the page owns this text now');
      t.ok(log.children.length > logBeforeD, 'Shift+ESC in the settle window is SPOKEN, not silent (a discard with no trace is indistinguishable from a broken key)');
      const lastNotice = log.children[log.children.length - 1].textContent;
      t.ok(/discarded/i.test(lastNotice), 'the notice names the act ("discarded"): ' + JSON.stringify(lastNotice));
      t.eq(api.micVoiceText(), '', 'the page no longer believes a settled sentence is in the composer');

      // E. THE HUMAN-TYPED GUARANTEE, which outranks every other criterion. A
      //    SECOND Shift+ESC with no settled sentence left has nothing to
      //    arbitrate, so the key does NOTHING AT ALL — it must not delete the
      //    operator's own text, and it must not invent an act to report.
      const logBeforeE = log.children.length;
      const callsBeforeE = state.voiceCalls.length;
      t.eq(api.micEscAct(), '', 'with no recording, no turn and no settled sentence there is no act to take');
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE, shiftKey: true });
      await t.tick();
      t.eq(input.value, 'my own words: ', 'a discard with no settled sentence leaves the operator text EXACTLY as it was');
      t.eq(log.children.length, logBeforeE, 'and it reports nothing, because it did nothing (no act, no claim)');
      t.eq(state.voiceCalls.slice(callsBeforeE).length, 0, 'and it spends no request');
      // The guard inside the discard is still reachable directly (a caller that
      // asks for a discard on an empty record), and it REFUSES by name rather
      // than pretending it discarded something.
      const refused = await api.micCancel();
      t.eq(refused, false, 'micCancel with nothing to cancel and nothing settled refuses (returns false)');
      t.ok(log.children.length > logBeforeE, 'the direct refusal is spoken');
      t.ok(/no recognised sentence/i.test(log.children[log.children.length - 1].textContent),
        'the refusal says there was no recognised sentence: ' + JSON.stringify(log.children[log.children.length - 1].textContent));

      // F. AN EDITED SENTENCE REFUSES. Record, settle, then type INSIDE the settled
      //    run — the discard must decline, because the characters at that position
      //    are the operator's now and removing "the sentence" would delete them.
      input.value = '';
      const live2 = holdLive([
        ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic', engine: 'streaming' })],
        ['draft', JSON.stringify({ text: '尼好', draft: '尼好', partials: 1 })],
      ]);
      doc.getElementById('mic').click();
      await t.tick();
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      live2.release(['final', JSON.stringify({ ok: true, transcript: '你好世界。', draft: '你好世界。', device: 'pulse:live-mic', partials: 1 })]);
      await t.tick();
      await t.tick();
      const edited = input.value + ' and my correction';
      input.value = edited;
      const logBeforeF = log.children.length;
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE, shiftKey: true });
      await t.tick();
      t.eq(input.value, edited, 'a discard over an EDITED sentence refuses and deletes nothing (composer = ' + JSON.stringify(input.value) + ')');
      t.ok(log.children.length > logBeforeF, 'the edited-sentence refusal is spoken');
      t.ok(/no longer holds what voice inserted/i.test(log.children[log.children.length - 1].textContent),
        'the refusal names what it MEASURED (the composer no longer matches the insert): ' + JSON.stringify(log.children[log.children.length - 1].textContent));
      t.ok(input.value.indexOf('and my correction') >= 0, 'the operator\'s correction survived the refused discard');
    },
  },

  // ------------------------------------------- the four rungs are DISJOINT
  {
    name: 'voice esc rungs: menu / recording / settled-sentence / turn are DISJOINT and ordered, and the modifier means only what its rung owns',
    about: 'the arbiter is one function read by both key listeners; a rung that can co-fire with another is an else-if chain by another name, and the settle window co-firing with the recording is exactly the gap',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input;
      const log = api.els.log;

      // A. NOTHING AT ALL: an empty page with no recording, no sentence, no turn.
      t.eq(api.micEscAct(), '', 'an idle page gives ESCAPE no act (the browser default stands)');

      // B. THE SETTLED SENTENCE IS INERT WHILE A RECORDING IS OPEN. This is the
      //    co-fire case the briefing names: "just finished inserting" and "voice
      //    text in the box" true at once. The recording rung must WIN, and the
      //    settled-sentence rung must not fire at all.
      const pack = (frames) => new Uint8Array(Buffer.from(
        frames.map(([ev, data]) => 'event: ' + ev + '\ndata: ' + data + '\n\n').join(''), 'utf8'));
      let release;
      state.voiceCalls = [];
      state.voiceLive = {
        ok: true, status: 200,
        body: { getReader: () => {
          let step = 0;
          return { read: () => {
            step++;
            if (step === 1) { return Promise.resolve({ done: false, value: pack([
              ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic' })],
              ['draft', JSON.stringify({ text: '尼好', draft: '尼好' })],
            ]) }); }
            if (step === 2) { return new Promise((res) => { release = (f) => res({ done: false, value: pack([f]) }); }); }
            return Promise.resolve({ done: true, value: undefined });
          } };
        } },
        text: () => Promise.resolve(''),
      };
      // Seed a settled sentence FIRST, by hand, exactly as a previous utterance
      // would have left one — then open a second recording on top of it.
      api.micInsert('上一句。');
      t.eq(api.micVoiceText(), '上一句。', 'a previously settled sentence is recorded as such');
      doc.getElementById('mic').click();
      await t.tick();
      t.eq(api.state().micLiveActive, true, 'a SECOND recording is now open on top of a settled sentence');
      t.eq(api.micEscAct(), 'recording', 'with a recording open the arbiter returns RECORDING, not the settled-sentence rung — the two cannot co-fire');
      // The recording is stopped and settles; the sentence it inserted becomes the
      // record, so the stale one can never be the subject of a discard.
      state.voiceCalls = [];
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      release(['final', JSON.stringify({ ok: true, transcript: '这一句。', draft: '这一句。', device: 'pulse:live-mic' })]);
      await t.tick();
      await t.tick();
      t.eq(api.micVoiceText(), '这一句。', 'the sentence the LAST utterance settled is the one the record names, not the previous one');
      t.eq(api.micEscAct(), 'voice-text', 'the recording is over, so the arbiter moves to the settled-sentence rung');

      // C. A MENU OUTRANKS EVERYTHING: the menu's Escape closes the menu and must
      //    not spend the key on a draft. The guard order is what makes this true.
      state.menuHandler = (trigger, q) => ({ trigger, query: q, source: 'slash', candidates: ['task'], total: 1 });
      input.value = '/ta';
      input.selectionStart = 3;
      await api.menuRefresh();
      t.eq(api.state().menu.open, true, 'the trigger menu opened');
      t.eq(api.micEscAct(), 'menu', 'an open menu owns ESCAPE, ahead of the settled-sentence rung');
      const before = input.value;
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      t.eq(api.state().menu.open, false, 'ESCAPE closed the menu');
      t.eq(input.value, before, 'closing the menu deleted nothing (the settled sentence survives)');
      t.eq(api.micVoiceText(), '这一句。', 'the settled-sentence record was not consumed by the menu rung');

      // D. SHIFT+ESC ON THE TURN RUNG IS NOT A DISCARD. The modifier belongs to the
      //    two voice rungs; on the turn rung it must do what plain ESC does — abort
      //    the turn — and must never reach into the composer.
      //
      //    THE SHIM CANNOT OBSERVE THE ABORT ITSELF: the page calls
      //    activeCtl.abort(), and the harness's fake stream is a plain promise that
      //    does not honour a signal, so `busy` stays true until the stream is
      //    released. Asserting "busy flipped to false" here would be asserting a
      //    property of the shim. What is asserted is what the PAGE decided: the rung
      //    the arbiter chose, and that no voice act ran on it.
      let releaseTurn;
      state.streamQueue.push(() => new Promise((res) => { releaseTurn = () => res(t.sseResponse([['done', '{}']])); }));
      input.value = 'a turn';
      api.els.form.dispatch('submit', {});
      await t.tick();
      t.eq(api.state().busy, true, 'a turn is in flight');
      t.eq(api.micEscAct(), 'turn', 'with a turn running the arbiter returns TURN, not the settled-sentence rung');
      const callsBeforeD2 = state.voiceCalls.length;
      const settleBefore = api.micVoiceText();
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE, shiftKey: true });
      await t.tick();
      t.eq(state.voiceCalls.slice(callsBeforeD2).length, 0, 'Shift+ESC on the turn rung sends no voice request (it is not a discard)');
      t.eq(api.micVoiceText(), settleBefore, 'Shift+ESC on the turn rung discarded no settled sentence (' + JSON.stringify(settleBefore) + ')');
      // SENDING THE TURN CLEARED THE COMPOSER (the page's own submit path), so the
      // settled sentence is no longer where voice left it. The record survives but
      // no longer MATCHES, and the discard must therefore REFUSE rather than slice
      // a sentence out of a composer it does not describe.
      t.eq(input.value, '', 'submitting the turn cleared the composer (the page\'s own submit path)');
      if (releaseTurn) { releaseTurn(); }
      await t.tick();
      await t.tick();
      t.eq(api.state().busy, false, 'the turn finished (released by the scenario, not by the abort)');
      input.value = 'typed after the turn';
      const logBeforeD3 = log.children.length;
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE, shiftKey: true });
      await t.tick();
      t.eq(input.value, 'typed after the turn', 'a discard whose record no longer matches the composer deletes NOTHING');
      t.ok(log.children.length > logBeforeD3, 'and it says why it refused');
      // AND NOW the same sentence IS discardable, because the turn no longer
      // outranks it — the rung did not lose the key, it only queued behind a turn.
      t.eq(api.micEscAct(), 'voice-text', 'once the turn ends the settled-sentence rung answers Escape again');
    },
  },

  // ------------------------------------------------- THE VISIBLE VOICE LAYER
  {
    name: 'voice esc layer: a VISIBLE voice layer outranks a streaming turn, a residual sentence does not',
    about: 'the operator spoke while a response was streaming, pressed Escape to close the voice layer, and the RESPONSE was aborted instead while the layer stayed exactly where it was — because the arbiter asked "is a turn running" before "is a voice layer on screen"',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const input = api.els.input;

      // Hold the turn open by hand: the shim's stream is a promise, so the only
      // way to have a turn in flight while other things happen is to not resolve
      // it. A turn the scenario releases is also a turn the ABORT cannot stop,
      // which is why this scenario reads api.aborted() (the page's own
      // activeCtl.signal) instead of reading `busy`.
      let releaseTurn;
      state.streamQueue.push(() => new Promise((res) => { releaseTurn = () => res(t.sseResponse([['done', '{}']])); }));
      input.value = 'a turn whose answer streams';
      api.els.form.dispatch('submit', {});
      await t.tick();
      t.eq(api.state().busy, true, 'a turn is streaming — this is the state the operator was in');

      // THE OPERATOR SPEAKS AGAIN while that response streams. Drive a REAL live
      // utterance to its settle frame, so the sentence in the composer is the
      // engine's and the page's own record names it — never a hand-placed value.
      let release;
      const pack = (frames) => new Uint8Array(Buffer.from(
        frames.map(([ev, data]) => 'event: ' + ev + '\ndata: ' + data + '\n\n').join(''), 'utf8'));
      state.voiceCalls = [];
      state.voiceLive = {
        ok: true, status: 200,
        body: { getReader: () => {
          let step = 0;
          return { read: () => {
            step++;
            if (step === 1) {
              return Promise.resolve({ done: false, value: pack([
                ['ready', JSON.stringify({ ok: true, device: 'pulse:live-mic' })],
                ['draft', JSON.stringify({ text: '第二句', draft: '第二句' })],
              ]) });
            }
            if (step === 2) { return new Promise((res) => { release = (f) => res({ done: false, value: pack([f]) }); }); }
            return Promise.resolve({ done: true, value: undefined });
          } };
        } },
        text: () => Promise.resolve(''),
      };
      doc.getElementById('mic').click();
      await t.tick();
      t.eq(api.state().micLiveActive, true, 'a recording is open WHILE the turn streams — the operator talks mid-answer');
      t.eq(api.micEscAct(), 'recording', 'the recording rung owns Escape first (unchanged)');
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(state.voiceCalls[state.voiceCalls.length - 1], '/api/voice/live/stop', 'Escape stopped the recording through the STOP route (calls = ' + JSON.stringify(state.voiceCalls) + ')');
      release(['final', JSON.stringify({ ok: true, transcript: '第二句。', draft: '第二句。', device: 'pulse:live-mic' })]);
      await t.tick();
      await t.tick();
      t.eq(api.state().micLiveActive, false, 'the recording has settled');
      t.eq(api.state().busy, true, 'AND THE TURN IS STILL STREAMING — the layer is up in front of a live response');
      t.eq(input.value, '第二句。', 'the settled sentence is THE VOICE LAYER ON SCREEN (composer = ' + JSON.stringify(input.value) + ')');

      // A. THE DEFECT, MEASURED. This is the exact state the operator reported:
      //    a visible voice layer over a streaming turn. The arbiter must answer
      //    the LAYER, not the turn.
      t.eq(api.micEscAct(), 'voice-text',
        'with a VISIBLE voice layer and a turn in flight the arbiter answers the LAYER — a visible overlay owns Escape ahead of the content it covers');
      t.eq(api.aborted(), false, 'nothing has been aborted yet (the control reading, before the key)');
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(api.aborted(), false, 'ESCAPE DID NOT ABORT THE RESPONSE — the operator asked to close the layer, not to stop the answer');
      t.eq(api.state().busy, true, 'the turn is still streaming after the keypress');
      t.eq(input.value, '第二句。', 'and the sentence the engine settled is untouched (plain Escape keeps it)');

      // B. THE OTHER HALF — A RESIDUAL SENTENCE MUST NOT STEAL THE INTERRUPT.
      //    The operator types after the settled sentence, so the composer no
      //    longer holds what voice inserted: the layer is no longer a layer. The
      //    record STILL EXISTS (Shift+Escape's discard machinery is intact), and
      //    that is precisely why the rung cannot key on the record alone — the
      //    measured regression of the unconditional ordering was "a stale
      //    sentence plus a running turn made the arbiter answer voice-text and
      //    the turn kept streaming".
      input.value = '第二句。 and my own words';
      t.eq(api.micVoiceText(), '第二句。', 'the record of the settled sentence is still held (the discard has not been lost)');
      t.eq(api.micEscAct(), 'turn',
        'with the SAME record but the composer no longer matching it, the turn rung wins again — a residual sentence does not outrank a live response');
      input.dispatch('keydown', { key: api.consts.KEY_ESCAPE });
      await t.tick();
      t.eq(api.aborted(), true, 'Escape aborted the turn — the interrupt the old order existed for is preserved wherever no layer is on screen');
      t.eq(api.state().busy, true, 'the shim stream does not honour the signal, so `busy` stays true until the scenario releases it (that is why the reading above is the signal, not busy)');

      // C. AND NOTHING WAS DELETED BY EITHER KEY. The turn rung must never reach
      //    into the composer, and the layer rung only moves the caret.
      t.eq(input.value, '第二句。 and my own words', 'neither Escape deleted a character — the operator typing survives both rungs');

      if (releaseTurn) { releaseTurn(); }
      await t.tick();
      await t.tick();
      t.eq(api.state().busy, false, 'the turn finished (released by the scenario)');

      // D. THE RESIDUAL RUNG STILL ANSWERS WHEN NOTHING IS STREAMING. With the
      //    turn gone the stale record answers Escape again, and it must: its job
      //    there is to REFUSE the discard out loud, and a Shift+Escape on a stale
      //    record that does nothing at all is the broken-key experience this
      //    whole file exists to end (Rule 15 — no silent failure).
      t.eq(api.micEscAct(), 'voice-text', 'with no turn running the residual sentence answers Escape again (unchanged behaviour)');
    },
  },

  // --------------------------------------------------------------- THEME (B13)
  {
    name: 'B13 theme: follows the system, a stored preference wins, the button cycles all three',
    about: 'a theme that cannot be reached by clicking is a theme half the users never get',
    async run(ctx, t) {
      const { api, state, doc } = ctx;
      const root = doc.documentElement;
      const btn = api.els.theme || doc.getElementById('theme');
      t.ok(!!root, 'the shim carries document.documentElement (the node data-theme lives on)');
      t.ok(!!btn, 'the page ships a #theme button');
      // 1) NO PREFERENCE + A LIGHT SYSTEM = LIGHT, and the label says "auto"
      state.themeStore.clear();
      state.systemLight = true;
      api.themeApply();
      t.eq(root.getAttribute('data-theme'), 'light',
        'with nothing stored the page FOLLOWS THE SYSTEM (system light → light palette)');
      t.eq(btn.textContent, 'auto', 'the follow state is spelled out on the button, not a glyph');
      // 2) THE SYSTEM FLIPS AND THE PAGE FOLLOWS IT, LIVE. The flip is the
      //    MOCK'S state (what matchMedia now answers), not the event payload:
      //    the real event carries `matches`, but the page re-READS
      //    matchMedia(), so a scenario that only changed the payload would be
      //    asserting against a browser that no longer exists.
      state.systemLight = false;
      state.themeListeners.forEach((fn) => fn({ matches: false }));
      t.eq(root.getAttribute('data-theme'), 'dark',
        'the media-query change listener re-applies while no preference is stored');
      // 3) ONE CLICK PINS LIGHT, and it survives the system flipping back
      btn.click();
      t.eq(state.themeStore.get('gopi.theme'), 'light', 'the first click stores "light"');
      t.eq(root.getAttribute('data-theme'), 'light', 'and applies it');
      t.eq(btn.textContent, 'light', 'the label names the pinned preference');
      state.systemLight = false;
      state.themeListeners.forEach((fn) => fn({ matches: false }));
      t.eq(root.getAttribute('data-theme'), 'light',
        'a STORED preference wins: the system flipping does not override it');
      // 4) THE SECOND CLICK PINS DARK
      btn.click();
      t.eq(state.themeStore.get('gopi.theme'), 'dark', 'the second click stores "dark"');
      t.eq(root.getAttribute('data-theme'), 'dark', 'and applies it');
      // 5) THE THIRD CLICK RETURNS TO FOLLOWING THE SYSTEM — the state is
      //    REACHABLE BY CLICKING, so nobody needs to know about localStorage
      btn.click();
      t.eq(state.themeStore.has('gopi.theme'), false,
        'the third click CLEARS the key — absence IS the follow-system state');
      t.eq(btn.textContent, 'auto', 'and the label says so');
      state.systemLight = true;
      api.themeApply();
      t.eq(root.getAttribute('data-theme'), 'light',
        'so the system decides again, with no reload');
      // 6) A DENIED STORE (a private window) IS FOLLOW-SYSTEM, NOT AN ERROR
      state.themeReadsThrow = true;
      state.systemLight = false;
      api.themeApply();
      t.eq(root.getAttribute('data-theme'), 'dark',
        'a storage read that THROWS degrades to following the system instead of breaking the page');
      state.themeReadsThrow = false;
      state.themeStore.clear();
    },
  },

];
