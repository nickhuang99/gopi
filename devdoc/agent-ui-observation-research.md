<!-- doc-class: historical -->
# How agent harnesses verify UI — and whether anyone exposes UI observation as a first-class model tool

Research task, 2026-09-20. **No production code changed** — this document is the
deliverable. Every outside-world claim carries a URL and the date that source
carries (or, where the source carries none, the date it was fetched). Facts and
inference are labelled separately; my own probe results are quoted as measured
output.

---

## 0. Verdict (three sentences)

**Model-initiated UI observation as a first-class tool is standard practice at
the browser layer and genuinely uncommon at the terminal/desktop layer.** Every
major harness (Claude Code, Cursor, Codex, Devin) plus both browser MCP servers
expose *observation* as its own callable tool — `read_page` (accessibility
tree), `take_snapshot`, `get_page_text`, `screenshot`, `zoom` — and this is not
a workaround: Playwright MCP states its purpose as *"enabling LLMs to interact
with web pages through structured accessibility snapshots, bypassing the need
for screenshots or visually-tuned models"* (README, fetched 2026-09-20), while
Chrome DevTools MCP instructs *"Prefer taking a snapshot over taking a
screenshot"* (tool-reference, fetched 2026-09-20). The part of our idea that is
NOT yet-visited-and-left is narrower and sharper than "UI observation as a tool":
it is **that a terminal/CLI harness exposes its OWN screen as a first-class
observation tool to the model**, so the agent can look at the surface it is
building without (a) a browser, (b) an OS screenshot, or (c) authoring a test
first — no vendor in this survey does that, and the reason is not a good one:
they model the terminal as a *thing you run commands in*, never as a surface the
agent may choose to look at.

**Classification of our idea, per the acceptance criterion: (b) known but rare —
with a specific, documented adjacent case (c).** "UI observation as a callable
model tool" is (a) standard practice for the browser surface; "observe the
*terminal* surface you are developing, as a tool" is (b)/(c) — I found no
harness that ships it, and the absence is *partly* for good reasons (a11y/DOM
trees are cheaper and more honest than pixels for web UI; and browsers were
exposed first because the agent could not otherwise see what it built at all)
and *partly* an artifact of prior framing rather than a judgement (nobody treats
the CLI as a graphical surface). The documented failure modes (Q4) are real but
they are pixel failure modes; they do not argue against a text-grid terminal
observation, which is exactly the representation the field already prefers for
web UI.

---

## 1. The five highest-value sources

| # | Source | Date | Why it is the one to read |
|---|---|---|---|
| 1 | `/home/user/work/Downloads/claude-code-system-prompts/system-prompts/tool-description-claude-in-chrome-read-page.md` (+ the whole dir) | `ccVersion: 2.1.211`, corpus README dated v2.1.238 / 2026-08-20 | The single best primary evidence: a shipped vendor tool whose *whole job* is mid-task observation, plus the tool-selection prose around it. Read-only local leak. |
| 2 | https://github.com/microsoft/playwright-mcp (README) | latest commit 2026-09-18 (`v0.0.82`, GitHub API) | States the text-over-pixels thesis outright and names the reasons (determinism, no vision model). Also documents the market moving to CLI+SKILLs for token reasons. |
| 3 | https://github.com/ChromeDevTools/chrome-devtools-mcp — `docs/tool-reference.md`, `docs/design-principles.md` | blog 2025-09-23; docs fetched 2026-09-20 | "Prefer taking a snapshot over taking a screenshot"; the `includeSnapshot` booleans show snapshot-return is a *per-action choice*; design principles state the token doctrine. |
| 4 | https://github.com/microsoft/playwright-mcp/issues/1654 (maintainer reply) | 2026-06-17 | A maintainer explicitly rejecting incremental-snapshot mode: what the field decided about observation *frequency* and token cost, in their own words. |
| 5 | https://webarena.dev / arXiv 2307.13854 · 2404.07972 | 2023-07-25 (rev. 2024-04-16) · 2024-04-11 | The measurement side: text/accessibility observation for web agents, and OSWorld's finding that screenshots+pixels is where agents fail (GUI grounding). |

Honourable mention (not a source, a precedent for the recommendation):
`internal/lineedit` in this repo — the same thesis, already implemented here as a
test harness (§7, item 1).

---

## 2. Q1 — How harnesses verify UI today, and what each cannot see

**(1) Browser automation driven by an agent.** Five distinct shapes, all shipped:

- *Accessibility-tree MCP* — Playwright MCP: "Uses Playwright's accessibility
  tree, not pixel-based input … **LLM-friendly**. No vision models needed,
  operates purely on structured data … **Deterministic tool application**. Avoids
  ambiguity common with screenshot-based approaches."
  (https://github.com/microsoft/playwright-mcp, README, fetched 2026-09-20;
  latest commit 2026-09-18.)
- *DevTools-native MCP* — `take_snapshot`: "Take a text snapshot of the target
  page based on the a11y tree. The snapshot lists page elements along with a
  unique identifier (uid). Always use the latest snapshot. **Prefer taking a
  snapshot over taking a screenshot**."
  (https://raw.githubusercontent.com/ChromeDevTools/chrome-devtools-mcp/main/docs/tool-reference.md,
  fetched 2026-09-20.)
- *In-app browser tools* — Cursor: "Agent displays browser actions like
  screenshots and actions in the chat… **Visual feedback with images**:
  Screenshots are integrated directly with the file reading tool, so Agent
  actually sees the browser state as images rather than relying on text
  descriptions."
  (https://cursor.com/docs/agent/browser, fetched 2026-09-20. **Vendor doc,
  marketing tone** — the claims are product positioning, not measurement.)

- *End-to-end recorded proof* — Devin: "records a video — starts a screen
  recording, executes the test plan in the desktop, and annotates key moments.
  … The goal is a short recording that a code reviewer watches and immediately
  thinks 'yep, it works' — then merges the PR."
  (https://docs.devin.ai/work-with-devin/testing-and-recordings.md, fetched
  2026-09-20.)

**(2) Component/unit + interaction tests.** Storybook play functions, Testing
Library. Testing Library's principle is explicitly behavioural, not
structural: "If it relates to rendering components, then it should deal with DOM
nodes rather than component instances" and "The more your tests resemble the way
your software is used, the more confidence they can give you."
(https://testing-library.com/docs/guiding-principles/, fetched 2026-09-20.)

**(3) Visual regression.** Playwright's `toHaveScreenshot()` stores golden PNGs
pinned by browser+platform ("Screenshots differ between browsers and platforms
due to different rendering, fonts and more") and warns "Browser rendering can
vary based on the host OS, version, settings, hardware, power source (battery vs
power adapter), headless mode, and other factors. For consistent screenshots,
run tests in the same environment where the baseline screenshots were generated."
(https://playwright.dev/docs/test-snapshots, fetched 2026-09-20.) Storybook
delegates this to Chromatic, a cloud service (https://storybook.js.org/docs/writing-tests/visual-testing,
fetched 2026-09-20).

**(4) Accessibility-tree assertions.** axe-core via Playwright, with an explicit
scope disclaimer: "Automated accessibility tests can detect some common
accessibility problems such as missing or invalid properties. But many
accessibility problems can only be discovered through manual testing."
(https://playwright.dev/docs/accessibility-testing, fetched 2026-09-20.)

**What each cannot see** (this is the interesting column):

| Method | Blind to |
|---|---|
| Pixel diff | *Whether the render is correct* — it proves only "identical to a blessed PNG", and the blessing is a human act. Environment-variance is admitted by the vendor (Playwright warning above). |
| a11y tree | Anything with no accessible name/structure: visual overlap, clipping, z-order, colour, and — the case that bites design systems — **fonts** (see §5a, the `[FONT_MISSING]` blind spot, stated by Anthropic's own prompt material). |
| Unit/component | The assembled page: integration layout, real routing, real network. |
| Console/network tools | Everything visual. |
| Screenshot-to-vision | Pixel-level quantification: a vision model reads, it cannot measure (repo rule: render verification is pixel truth, not OCR). |
| Video recording | Anything not watched; the artefact is for a human, and it is the *least* machine-checkable of the set. |

Facts vs inference: the "blind to" claims for pixels/a11y are stated by the
sources (Playwright's variance warning; Playwright MCP's "ambiguity common with
screenshot-based approaches"; Anthropic's `[FONT_MISSING]` note quoted in §5f).
The unit-test and console-network rows are **my inference** from what those
methods are, not vendor claims.

---

## 3. Q2 — The "UI as text/model, not pixels" school, and its stated reasons

The school is real and it is the *mainstream* choice at the browser layer. Its
stated reasons, verbatim:

1. **No vision model required / determinism.** Playwright MCP: "Uses Playwright's
   accessibility tree, not pixel-based input … No vision models needed, operates
   purely on structured data … Avoids ambiguity common with screenshot-based
   approaches." (README, fetched 2026-09-20.)
2. **DOM-aware is faster.** Anthropic's own computer-use skill: "**Chrome MCP**
   (`mcp__claude-in-chrome__*`) — if the target is a web app and there's no
   dedicated MCP for it, use the browser tools. **DOM-aware, much faster than
   clicking pixels.**"
   (`claude-code-system-prompts/system-prompts/skill-computer-use-mcp.md`,
   `ccVersion: 2.1.89`.)
3. **Cheaper than a screenshot, when a tool only needs text.** Claude Code's own
   bridge-timeout guidance: "Try a lighter operation (e.g., `get_page_text`
   instead of a screenshot)".
   (`.../tool-description-claude-in-chrome-bridge-timeout-error.md`.)
4. **Token discipline as a design principle.** Chrome DevTools MCP:
   "**Token-Optimized**: Return semantic summaries. 'LCP was 3.2s' is better
   than 50k lines of JSON. Files are the right location for large amounts of
   data." and "**Reference over Value**: for heavy assets (screenshots, traces,
   videos), return a file path or resource URI, never the raw data stream."
   (https://raw.githubusercontent.com/ChromeDevTools/chrome-devtools-mcp/main/docs/design-principles.md,
   fetched 2026-09-20.)
5. **Terminal UI is asserted as a cell grid, not pixels.** Textual's test API
   runs the app headless and asserts on app state
   (https://textual.textualize.io/guide/testing/, fetched 2026-09-20);
   `pytest-textual-snapshot` keeps SVG snapshots for regression
   ("A `pytest-textual-snapshot` test saves an SVG screenshot of a running
   Textual app to disk… If the new screenshot differs from the old one, the test
   fails", https://github.com/Textualize/pytest-textual-snapshot, fetched
   2026-09-20); ratatui's `TestBackend` renders to memory and asserts buffer
   lines — "A Backend implementation used for integration testing that renders
   to an memory buffer", with `assert_buffer_lines([...])` and a separate
   `scrollback()` buffer
   (https://docs.rs/ratatui/latest/ratatui/backend/struct.TestBackend.html,
   fetched 2026-09-20).
6. **Structured state beats a rendered page for *reasoning*.** Anthropic's tool
   design guidance: "if an LLM agent uses a tool that returns ALL contacts and
   then has to read through each one token-by-token, it's wasting its limited
   context space on irrelevant information … The better and more natural
   approach … is to skip to the relevant item directly."
   (https://www.anthropic.com/engineering/writing-tools-for-agents, published
   2025-09-11.)

**Inference (mine):** this school is not "anti-pixel sentiment" — it is a
*bandwidth and determinism* argument. Where pixels carry information text
cannot (layout collision, font substitution, colour), the same vendors DO ship
screenshots and zoom (`tool-description-computer-zoom.md`, `ccVersion: 2.1.173`:
"Take a higher-resolution screenshot of a specific region … Use this liberally
to inspect small text, button labels, or fine UI details"). Both representations
coexist; the field's rule of thumb is *cheapest representation that answers the
question*.

---

## 4. Q3 — The crux: prior art for model-initiated UI observation as a tool

**Answer: yes, abundantly — as long as "UI" means a browser page or a desktop. It
is a named, first-class, model-initiated tool in every major harness. As a tool
over the *terminal* surface the agent is developing, I found none.**

*Observation-as-its-own-tool, named, mid-task:*

- `mcp__claude-in-chrome__read_page` — "Get an accessibility tree representation
  of elements on the page. By default returns all elements including non-visible
  ones. Output is limited to 50000 characters by default. If the output exceeds
  this limit it is truncated at a line boundary, with a note giving the full
  size — pass a larger `max_chars`, or use `depth`/`ref_id` to focus on part of
  the page. Optionally filter for only interactive elements."
  (`claude-code-system-prompts/system-prompts/tool-description-claude-in-chrome-read-page.md`,
  `ccVersion: 2.1.211`; README index line 553.)
- `get_page_text` — "Extract raw text content from the page, prioritizing article
  content." (same corpus, `ccVersion: 2.1.173`.)
- `read_console_messages` — "Read browser console messages … **IMPORTANT: Always
  provide a pattern to filter messages** — without a pattern, you may get too
  many irrelevant messages." (same corpus.)
- `take_snapshot` (chrome-devtools-mcp, quoted in §2) and `take_screenshot` with
  `filePath`/`fullPage`/`format`/`quality`/`uid` parameters, plus
  `screencast_start` (a *recording* as a tool).
- Anthropic computer use: `screenshot` ("Capture the full display and return it
  as an image") and `zoom` ("Capture only that region … This lets Claude read
  small text or dense UI that isn't legible in a downscaled full screenshot").
- Codex CLI exposes `view_image(path)` as a tool over local files: "View a local
  image file from the filesystem when visual inspection is needed."
  (`~/work/codex/codex-rs/core/src/tools/handlers/view_image_spec.rs`, local
  checkout `ad9e809`; the handler rejects the call when the model lacks image
  input: "view_image is not allowed because you do not support image inputs",
  `.../handlers/view_image.rs`).
- Claude Code's file-read tool explicitly doubles as the image eye: "This tool
  allows Claude Code to read images (eg PNG, JPG, etc). When reading an image
  file the contents are presented visually as Claude Code is a multimodal LLM …
  **You will regularly be asked to read screenshots. If the user provides a path
  to a screenshot, ALWAYS use this tool to view the file at the path.**"
  (`.../tool-description-readfile.md`, `ccVersion: 2.1.235`.)

*The strongest single statement of the crux — an instruction to look before
asserting:*

> "**Look before you assert.** If the user asks about app state (what's open,
> what's connected, what an app can do), take a screenshot and check before
> answering. Don't answer from memory — the user's setup or app version may
> differ from what you expect. If you're about to say an app doesn't support an
> action, that claim should be grounded in what you just saw on screen, not
> general knowledge."
> — `claude-code-system-prompts/system-prompts/skill-computer-use-mcp.md`
> (`ccVersion: 2.1.89`)

*And an instruction that the observation is not optional when the change is
UI-shaped:*

> "For UI or frontend changes, start the dev server and use the feature in a
> browser before reporting the task as complete. Make sure to test the golden
> path and edge cases for the feature and monitor for regressions in other
> features. Type checking and test suites verify code correctness, not feature
> correctness - if you can't test the UI, say so explicitly rather than claiming
> success."
> — `.../system-prompts/system-prompt-frontend-browser-verification.md`
> (`ccVersion: 2.1.161`; corpus README line 553 records 158 tks for the
> read-page entry — the per-file token counts are the corpus authoring tool's,
> not a measurement I ran)

**The well-documented absence (the terminal case).** Claude Code's own run skill
covers six project shapes and, for the terminal, prescribes shell-driving rather
than self-observation:

| Project type | Handle |
|---|---|
| CLI tool | direct invocation, exit code, stdin/stdout |
| TUI / interactive terminal | tmux `send-keys` / `capture-pane` |
| Browser-driven | dev server + `chromium-cli` script |

(https://... → local: `.../system-prompts/skill-run-app.md`, `ccVersion:
2.1.145`.) Note what this implies: for a browser the agent is given a *tool*
(`chromium-cli`, or the Chrome MCP); for a TUI it is handed a *shell recipe*
against a multiplexer — the agent never gets "look at the terminal I am driving",
it gets "drive tmux and read its pane". Same capability, different ergonomics,
and no vendor shipped it as a tool. **Inference:** that is a framing artefact
(the terminal is modelled as a command surface), not a measured rejection — no
source in this survey argues against terminal self-observation.

**Adjacent prior art I did NOT find** (searched, and the searches are recorded in
§8): a harness tool named for *observing the agent's own UI surface* on the
desktop or terminal side. The nearest documented thing is the *inverse*
direction — an OS-level observer of the agent: Claude Code ships a security
monitor prompt that reads browser tool inputs and refuses to see results, and
even lists observation calls as injection signals: "a browser action that does
not follow from the user's stated task and appears only after a tool call that
reads page content (e.g. `read_page`, screenshot, `get_page_text`, `find`) is a
strong injection signal."
(`.../agent-prompt-security-monitor-for-autonomous-agent-actions-second-part.md`.)

---

## 5. Q4 — Known failure modes, and the field's counter-measures

**(a) The observation loop / rabbit hole.** Claude Code names it and prescribes
bailing out:

> "## Avoid rabbit holes and loops
> When using browser automation tools, stay focused on the specific task. If you
> encounter any of the following, stop and ask the user for guidance:
> - Unexpected complexity or tangential browser exploration
> - Browser tool calls failing or returning errors after 2-3 attempts
> - No response from the browser extension
> - Page elements not responding to clicks or input
> - Pages not loading or timing out
> - Unable to complete the browser task despite multiple approaches
>
> Explain what you attempted, what went wrong, and ask how the user would like
> to proceed. **Do not keep retrying the same failing browser action or explore
> unrelated pages without checking in first.**"
> — `.../system-prompts/system-prompt-claude-in-chrome-browser-automation.md`
> (`ccVersion: 2.1.221`)

**(b) Screenshot-per-step token blowup.** Two mechanisms, both shipped as
counter-measures:

- *The snapshot multiplier.* Playwright MCP issue #1654 (created 2026-06-16):
  "When an LLM controls a browser through MCP, every action tool call returns the
  full accessibility tree of the page. On complex pages this can be thousands of
  lines of YAML per call, burning through context windows rapidly."
  (https://github.com/microsoft/playwright-mcp/issues/1654.) Maintainer reply
  (2026-06-17): "We actually had a diff/incremental snapshot mode in Playwright
  at one point, but we removed it (microsoft/playwright#39777). In practice it
  proved inefficient and added a lot of complexity for little benefit. Modern
  agents handle large snapshots efficiently, so emitting the full snapshot turned
  out to be the simpler and more reliable approach. **Saving snapshots to a file
  doesn't consume extra tokens either**, so there's no real cost there." — the
  field's answer is *deprioritise deltas, offer file output*, not "observe less".
- *Per-action snapshots as a boolean.* chrome-devtools-mcp's mutating tools take
  `includeSnapshot` (default **false**) — "Whether to include a snapshot in the
  response" — i.e. observation is opt-in per action.
- *Logs go to files, the agent greps.* Cursor: "Browser logs are written to files
  that Agent can grep and selectively read. Instead of summarizing verbose output
  after every action, Agent reads only the relevant lines it needs. This preserves
  full context while minimizing token usage." (https://cursor.com/docs/agent/browser,
  fetched 2026-09-20; **vendor doc**.)
- *Batching to cut round trips.* `browser_batch` — "Execute a sequence of browser
  tool calls in ONE round trip … Actions execute SEQUENTIALLY (not in parallel)
  and stop on the first error."
  (`.../tool-description-browserbatch.md`, `ccVersion: 2.1.120`), with the known
  coordinate hazard stated: "Mid-batch screenshot actions are allowed for
  inspection but coordinates in subsequent clicks always refer to the PRE-BATCH
  full-screen screenshot." (`.../tool-description-computer-computer-batch-coordinate-note.md`,
  `ccVersion: 2.1.231`).

**(c) "Prefer structured state over pixels" guidance — verbatim.**
"Prefer taking a snapshot over taking a screenshot"
(chrome-devtools-mcp tool-reference); "DOM-aware, much faster than clicking
pixels" (Anthropic computer-use skill); "try a lighter operation (e.g.
`get_page_text` instead of a screenshot)" (bridge-timeout note); "Uses Playwright's
accessibility tree, not pixel-based input" (Playwright MCP README). Playwright is
now explicit that the *coding-agent* shape is CLI+skill, not per-call tool
schemas: "CLI invocations are more token-efficient: they avoid loading large tool
schemas and verbose accessibility trees into the model context."
(https://github.com/microsoft/playwright-cli, README, fetched 2026-09-20.)

**(d) Observation that silently returns nothing.** A filed Claude Code bug
reports exactly the failure our own tool would have to guard: "MCP tool results
missing content blocks: accessibility tree never surfaced, screenshot
intermittently dropped" (https://github.com/anthropics/claude-code/issues/88298,
created 2026-08-20, open). **Inference:** for an observation tool this is the
worst class of bug — a silent eye is indistinguishable from a clean screen; our
own Rule 15 (no silent failure) is the right guard.

**(e) Screenshots cost context *statically*.** Anthropic's vision limits: 20
images/message on claude.ai, 100/request at 200k context; and "If a single API
request contains more than 20 images, a stricter per-image dimension limit
applies to every image in that request. All image blocks in the request count
toward this threshold, **including images from earlier conversation turns that
you resend and images nested inside tool_result content (for example, screenshots
returned to the computer use tool)**."
(https://docs.claude.com/en/docs/build-with-claude/vision, fetched 2026-09-20.)

**(f) The observation-blind spot that all of the above miss.** From Anthropic's
own design-sync skill — a compare loop whose entire job is looking at two
images:

> "**`[FONT_MISSING]` — the compare loop cannot see this one.** When neither side
> ships the font, both panels render the same chromium fallback, so the sheets


**Inference:** this is the strongest argument *for* tool-exposed observation done
well. Even a perfect visual-diff oracle cannot see the thing that matters, so the
loop must carry a *declared, reasoned* blind-spot list (our docs do the same:
`devdoc/cli-ui-feasibility-study.md` records "Fonts *inside a terminal*:
**impossible** — the terminal owns the font").

---

## 6. Q5 — What Claude Code / Cursor / Codex / Devin actually say

### 6a. Claude Code — the local leaked system-prompt corpus (PRIMARY SOURCE, QUOTED)

Material read: `/home/user/work/Downloads/claude-code-system-prompts` —

up-to-date list of all Claude Code's various system prompts and their associated
token counts as of **Claude Code v2.1.238** (August 20th, 2026)", maintained by
Piebald (a vendor with its own product link at the top of the README — **treat
the surrounding page as vendor material; the prompt text itself is the
primary artefact**). Each extracted file carries a metadata header with
`name` / `description` / `ccVersion`, which is how I date the quotes below.

Read-only, no writes to this tree. Files quoted and their `ccVersion`:
`system-prompt-claude-in-chrome-browser-automation.md` (2.1.221),
`system-prompt-frontend-browser-verification.md` (2.1.161),
`skill-computer-use-mcp.md` (2.1.89), `tool-description-computer.md` (2.0.71),
`tool-description-computer-zoom.md` (2.1.173),
`tool-description-claude-in-chrome-read-page.md` (2.1.211),
`tool-description-claude-in-chrome-get-page-text.md` (2.1.173),
`tool-description-claude-in-chrome-read-console-messages.md` (2.1.173),
`tool-description-readfile.md` (2.1.235),
`system-reminder-browser-read-only-access-guidance.md` (2.1.235),
`system-reminder-app-read-only-access-guidance.md` (2.1.235),
`skill-run-app.md` (2.1.145), `tool-description-browserbatch.md` (2.1.120),
`tool-description-computer-computer-batch-coordinate-note.md` (2.1.231),
`skill-design-sync-storybook-source-shape.md`, `skill-verify-skill.md`,
`agent-prompt-security-monitor-for-autonomous-agent-actions-second-part.md`.

**Verbatim — screenshots / UI / browser tooling:**

> "Use a mouse and keyboard to interact with a web browser, and take screenshots.
> …
> * Whenever you intend to click on an element like an icon, you should consult a
> screenshot to determine the coordinates of the element before moving the cursor.
> * If you tried clicking on a program or link but it failed to load, even after
> waiting, try adjusting your click location so that the tip of the cursor
> visually falls on the element that you want to click."
> — `tool-description-computer.md` (`ccVersion: 2.0.71`)

> "Take a higher-resolution screenshot of a specific region of the last
> full-screen screenshot. Use this liberally to inspect small text, button
> labels, or fine UI details that are hard to read in the downsampled full-screen
> image. IMPORTANT: Coordinates in subsequent click calls always refer to the
> full-screen screenshot, never the zoomed image. This tool is read-only for
> inspecting detail."
> — `tool-description-computer-zoom.md` (`ccVersion: 2.1.173`)

> "Get an accessibility tree representation of elements on the page. By default
> returns all elements including non-visible ones. Output is limited to 50000
> characters by default. If the output exceeds this limit it is truncated at a
> line boundary, with a note giving the full size — pass a larger max_chars, or
> use depth/ref_id to focus on part of the page. Optionally filter for only
> interactive elements."
> — `tool-description-claude-in-chrome-read-page.md` (`ccVersion: 2.1.173`)

> "Read browser console messages (console.log, console.error, console.warn, etc.)
> from a specific tab. … IMPORTANT: Always provide a pattern to filter messages -
> without a pattern, you may get too many irrelevant messages."
> — `tool-description-claude-in-chrome-read-console-messages.md` (2.1.173)

> "Extract raw text content from the page, prioritizing article content. Ideal for
> reading articles, blog posts, or other text-heavy pages. Returns plain text
> without HTML formatting."
> — `tool-description-claude-in-chrome-get-page-text.md` (2.1.173)

> "The Chrome extension is connected but the page may be loading, unresponsive, or
> waiting on a permission prompt in the extension side panel. Try a lighter
> operation (e.g., "get_page_text" instead of a screenshot) or ask the user to
> check the page and any pending prompts."
> — `tool-description-claude-in-chrome-bridge-timeout-error.md`

> "This tool allows Claude Code to read images (eg PNG, JPG, etc). When reading an
> image file the contents are presented visually as Claude Code is a multimodal
> LLM. … You will regularly be asked to read screenshots. If the user provides a
> path to a screenshot, ALWAYS use this tool to view the file at the path. This
> tool will work with all temporary file paths."
> — `tool-description-readfile.md` (`ccVersion: 2.1.235`)

**Verbatim — tool selection (the routing prose, which is the thing that
persuades a model to use a tool at all):**

> "**Pick the right tool for the app.** Each tier trades speed/precision against
> coverage:
> 1. **Dedicated MCP for the app** — if the task is in an app that has its own MCP
> (Slack, Gmail, Calendar, Linear, etc.) and that MCP is connected, use it.
> API-backed tools are fast and precise.
> 2. **Chrome MCP** (`mcp__claude-in-chrome__*`) — if the target is a web app and
> there's no dedicated MCP for it, use the browser tools. DOM-aware, much faster
> than clicking pixels. …
> 3. **Computer use** — for native desktop apps … and cross-app workflows."
> — `skill-computer-use-mcp.md` (`ccVersion: 2.1.89`)

> "**Browsers** (Safari, Chrome, Firefox, Edge, Arc, etc.) → tier **"read"**:
> visible in screenshots, but clicks and typing are blocked. You can read what's
> already on screen. … **Terminals and IDEs** (Terminal, iTerm, VS Code,
> JetBrains, etc.) → tier **"click"**: visible and left-clickable, but typing, key
> presses, right-click, modifier-clicks, and drag-drop are blocked. … For shell
> commands, use the Bash tool."
> — `skill-computer-use-mcp.md` (2.1.89)



> "How you use screenshots: nothing here — but the *inverse* appears in the
> security monitor, which treats an observation call as an injection signal"
> — see §4's quote of
> `agent-prompt-security-monitor-for-autonomous-agent-actions-second-part.md`.

**Verbatim — the observation is the deliverable (Anthropic's own grader loop):**

> "**Grading is done by whoever is working the component** … After each compare
> run: Read the sheet (and raw PNGs when in doubt), judge each story **from the
> images alone** …"
> — `skill-design-sync-storybook-source-shape.md`

> ".design-sync/.cache/compare/<Name>.json — pairing facts + shot paths (**no
> similarity scores — your eyes are the judge**)."
> — same file

**Verbatim — the operational cost model, from the same corpus:**

> "If the mcp__claude-in-chrome__* tools are deferred (must be loaded via
> ToolSearch before use), load every tool you expect to need in ONE ToolSearch
> call — the select query accepts a comma-separated list — never one call per
> tool."
> — `system-prompt-chrome-browser-mcp-tools.md` (`ccVersion: 2.1.221`), i.e.
> **observation tools are gated behind a discovery step whose cost is explicitly
> managed**.

*What the local corpus does NOT contain* (honest gap): there is no single
authoritative Claude Code "enabled tool list"; `ccVersion` on each file marks the
version a string was extracted from, not what a given session loads. The
browser/computer tools appear only when the user's extension/account/plan makes
them available (the prompts themselves say so: "If the Chrome extension isn't
connected, ask the user to install it"; "Computer Use is controlled by the
**Enable desktop mode** toggle"). So **inference**: presence of a tool description
in the corpus ≠ default availability in a stock Claude Code session.

### 6b. Cursor

Cursor ships a first-party browser tool; the docs page states its observation
model and its token counter-measures: "Agent can control a web browser to test
applications, audit accessibility, convert designs into code, and more. … We've
optimized the browser tools to be more efficient and reduce token usage …
**Visual feedback with images**: Screenshots are integrated directly with the
file reading tool, so Agent actually sees the browser state as images rather
than relying on text descriptions. This enables better understanding of visual
layouts and UI elements. … **Smart prompting**: Agent receives additional context
about browser logs, including total line counts and preview snippets …" and
"Native integration: Agent displays browser actions like screenshots and actions
in the chat" (https://cursor.com/docs/agent/browser, fetched 2026-09-20;
**vendor documentation — product claims, no measurement published**). Note the
convergence: Cursor's answer to "token blowup" (§5b) is the same as Claude Code's
— *files + selective read* — not "observe less".

### 6c. Codex (local checkout read; primary)

`~/work/codex` at `ad9e809`. **Facts:** the tree exposes exactly one
vision-shaped tool, `view_image(path)` — "View a local image file from the
filesystem when visual inspection is needed. Use this for images already
available on disk." — with an optional `detail` parameter ("Defaults to `high`;
use `original` to preserve exact resolution"), registered only when the model
advertises image input (`spec_plan.rs`: `if turn_context.config.features.enabled(Feature::ViewImage)`;
the handler's error text: "view_image is not allowed because you do not support
image inputs"). Guardian reviewers are given a minimal toolset that *includes*
`view_image` alongside `exec_command`/`write_stdin` (`spec_plan.rs:890`).
**No browser tool, no screenshot tool, no DOM/a11y tool exists in the checkout**
(`grep -ril 'browser|playwright|screenshot' docs/*.md` → nothing relevant).
**Could not determine:** the deployed Codex product's full tool surface — only the
public repo is available to me, and the repo deliberately holds the CLI/exec
surface. **Inference:** Codex's stance is "the agent brings its own observation"
(a local image file, a shell), not "the harness ships an eye" — the opposite
choice from Claude Code's Chrome MCP, and worth noting as a legitimate design
point in the space.

### 6d. Devin

Devin's docs describe a full desktop observer with an explicit
"agent decides on its own" doctrine:

> "Devin sees the screen as a 1024×768 pixel display and can perform actions like
> clicking, typing, scrolling, dragging, and taking screenshots — just like a
> human sitting at the computer."
> "**Autonomously when appropriate.** Devin decides on its own when desktop
> interaction is the right tool for the job. If a task involves clicking UI
> elements, navigating an app, filling out forms, or visually verifying something,
> Devin will use Computer Use without being explicitly asked. You don't need to
> tell Devin *how* to interact with the screen — just tell it *what* to
> accomplish."
> — https://docs.devin.ai/work-with-devin/computer-use.md (fetched 2026-09-20)

And crucially, **Devin names the terminal as a UI surface it looks at** — the
closest thing in the survey to our idea, though mediated through desktop
computer-use rather than a terminal-specific tool:

> "This goes beyond browser automation — Devin can interact with **any
> application** that renders on screen, including: … **Terminal-based UIs** (TUI
> programs, interactive CLIs) … **Any visual interface** that can be displayed on
> the desktop."
> — same page (fetched 2026-09-20)

Android emulation uses the same two-lens split we would want: "`adb` (command
line) — install APKs, run tests, capture logs, take screenshots" vs "Computer Use
(desktop) — tap, swipe, type, and navigate the emulator's screen visually"
(https://docs.devin.ai/onboard-devin/environment/android-emulation.md, fetched
2026-09-20). **Vendor docs; no measurements published.**

---

## 7. Q6 — What a careful practitioner does under OUR constraints, and is our idea already-left?

**Our four constraints** (from the briefing, all verifiable here): zero runtime
dependencies; a terminal UI that can be asserted as text; a *measured* adoption
problem (tools that exist but are not called); research-only, no production
change.

**What the field's practice reduces to, mapped onto us:**

1. *Prefer the cheapest observation that answers the question* (§3). For us the
   cheapest is not a new tool at all — it is a **text grid**. We already have it:
   `internal/lineedit` renders the editor into a cell grid and asserts on it —
   `render_test.go:30 TestLineEditorRender`, `:238 TestLineEditorScreenGrid`,
   plus `cursor_screen_test.go`, `editor_rows_test.go`, `screen.go`. This is
   ratatui-`TestBackend`/Textual-`Screen` in Go, and it is the representation the
   whole industry picks for structured surfaces. **Measured, not inferred:** read
   from this worktree.
2. *Files for heavy artefacts, the model reads selectively* (§5b). Our equivalents
   exist: `devdoc/COMPUTER-USE-CLOSURE-VERIFIED.md` records `screen_capture`
   self-report cross-checked three independent ways (`file`, ImageMagick
   `identify`, `sha256sum` all agreeing, with a before/after hash change proving
   the actuation loop) and `internal/shot/` holds the zero-CGO X11 + CDP
   implementations. So the pixel path is built and *verified* — and it is exactly
   the path the field deprioritises for UI while keeping for the cases text
   cannot reach.
3. *Observation must be model-initiated, not test-mediated.* This is our crux and
   the field's normal practice for browsers (§4). Our own precedent for the
   terminal case exists in a different habitat: a pty harness that reads the
   screen as stripped ANSI text and a fixed-geometry `TIOCSWINSZ` (stored in
   memory as the closure recipe, used to reproduce the chat small-terminal bug
   deterministically). That is a *test* mechanism today, not a model-callable
   tool.
4. *Adoption is the real gate* (Repo Rule 11 + the measured audit: `git_blame`
   0 calls / 21 435; `code_search` 0.3%). Every vendor above treats *prompt
   routing* as the persuasion mechanism — the tool-selection tiers in
   `skill-computer-use-mcp.md`, the "ALWAYS use this tool" in
   `tool-description-readfile.md`, the "Prefer taking a snapshot over taking a
   screenshot" in chrome-devtools-mcp. Our adoption problem is not solved by
   building the tool; it is solved by the routing sentence, and the routing
   sentence has to name a *situation* the model recognises ("the change is
   UI-shaped and I cannot tell whether it renders").

**Is our idea already-visited-and-left?** Split verdict, and the split matters:

- *Visited and standard*: "expose observation as a tool" (browser/DOM/a11y,
  screenshot/zoom, console/network). Do not re-litigate this — copy it.
- *Visited and deliberately narrowed*: incremental/delta observation — built,
  pulled out, and the removal explained by a maintainer
  (microsoft/playwright-mcp#1654, reply 2026-06-17). Do not re-invent per-step
  deltas; if we observe repeatedly, that decision needs a reason the maintainer's
  argument does not cover (a text grid is not a 50 k-line JSON, so the argument
  may not transfer — **inference**).
- *Not visited*: a tool that lets the model look at **the harness's own terminal
  surface** mid-task. No vendor ships it; the nearest are Devin's computer-use
  over "terminal-based UIs" (pixels, desktop-mediated, not a terminal tool) and
  Claude Code's `skill-run-app.md`, which tells the agent to drive tmux
  `capture-pane` — i.e. observation is available, but only as an improvised shell
  recipe the model must *remember to write*, which is precisely the adoption
  failure mode we measure in ourselves.

**Recommendation (research-grade, no code changed here).**
Narrow the idea to what the survey leaves open and enforce the field's own
discipline:

1. **Do not build a pixel eye for the terminal.** The field's own rule (§3) makes
   the text grid the first-class surface; pixels are the escalation for what text
   cannot carry (font substitution — `[FONT_MISSING]`; colour; z-order), and our
   terminal cannot carry fonts at all (`devdoc/cli-ui-feasibility-study.md`).
2. **Make the observation a *text* observation with a declared blind-spot list.**
   The `[FONT_MISSING]` precedent (§5f) shows the artefact must state what it
   cannot see, or the loop reports a false pass.
3. **Route by situation, in the tool description's first sentence** (§4, §6a) —
   the only mechanism the field uses to make a model pick a tool; and prove
   adoption with the Rule 11 recipe (two real task scenarios, session logs
   showing the call) *before* claiming the tool works.
4. **Do not chase per-step deltas** (microsoft/playwright-mcp#1654). A text grid
   is cheap enough to return whole; the maintainer's argument against deltas is
   about a different cost structure, but the burden of proof sits with whoever
   wants the complexity.
5. **Silent-eye guard.** The one filed defect in this space is an observation tool
   returning nothing (claude-code#88298). A tool whose whole value is "look" must
   fail loudly when it cannot see.

---

## 8. Could not determine / failed routes (honest section)

**Routes that failed or were blocked, and how that constrains the conclusion:**

| Route | Result (measured) | Consequence for this report |
|---|---|---|
| `web_search` default (merged bing+google+arxiv) | `"google": "timeout"`, `"arxiv": "unavailable"`; bing returned CN SEO pages (Zhihu/CSDN/runoob) irrelevant to the query | **Conclusion is built on directly-fetched primary URLs, not on search discovery.** A reader should assume the long tail of blog posts on this topic was never seen; the survey covers official docs + repos + arXiv abstracts, which is where the load-bearing claims live. |
| `web_search` method=arxiv | `arxiv API HTTP 406: Not Acceptable` | No arXiv *search*; I fetched specific arXiv IDs I already knew (`2307.13854`, `2401.13649`, `2401.13919`, `2404.07972`) — the absence of *other* papers is undetermined. |
| `web_search` method=github | `github api returned no results` (empty for the OpenHands browser query) | GitHub *issue* discovery had to go through the REST search API directly, which worked. |
| site-scoped bing query (`docs.devin.ai …`) | returned Google Docs / 腾讯文档 pages | Search-engine scoping is unusable here; vendor docs were found via each vendor's `llms.txt` index instead. |
| `https://www.firecrawl.dev/blog/introducing-actions` | fetch timed out (45 s) | One candidate blog source lost; no claim depends on it. |
| `https://openai.com/index/computer-using-agent/` | **HTTP 403** | OpenAI's Computer-Using Agent page could not be read. **No OpenAI/o3 CUA claims appear in this report.** The platform docs page fetched but returned navigation chrome only, so it is not cited. |
| `https://docs.devin.ai/product-guides/browser`, `.../essential-guidelines/browser` | HTTP 404 (my guessed paths) | Resolved: the docs index (`llms.txt`) gives real paths (`/work-with-devin/computer-use.md`, `/work-with-devin/testing-and-recordings.md`), which are cited. The browser-specific Devin page (as distinct from computer-use) was **not** read. |
| Cursor / Devin / Chrome DevTools MCP doc *publication dates* | Most vendor doc pages carry no date; only the chrome-devtools blog (2025-09-23) does | Those citations carry "fetched 2026-09-20" instead; an undated vendor claim is weaker evidence and is marked as such. |

**Things I could not determine (stated as gaps, not filled from recall):**

1. **A measured token cost for screenshots-in-context vs an a11y snapshot.** No
   primary measurement found; the sources state *policy* ("token-optimized",
   "prefer snapshot", `includeSnapshot` default false) and one anecdote (issue
   #1654's "thousands of lines of YAML per call"), not instrumented numbers. Any
   claim that "screenshots cost N× a snapshot" would be invented.
2. **Whether any vendor ships terminal-surface observation as a *tool*.** I
   checked Claude Code's prompt corpus exhaustively by filename and by content
   for `browser|screenshot|playwright|computer use|terminal|tmux|capture-pane`,
   read Codex's tool registry locally, read Cursor's and Devin's docs. Absence of
   evidence across those four is strong but is not proof about other harnesses
   (Aider, OpenHands' TUI paths, Goose, Amp, Antigravity, Zed, Warp) — **not
   surveyed**.
3. **Claude Code's default enabled-tool set.** The leaked corpus is per-string
   with `ccVersion` stamps and no session-level manifest; browser/computer tools
   are extension/plan-gated by the prompts' own admission. So "Claude Code has a
   UI-observation tool by default" is *not* established — only that the tool
   descriptions and their routing prose are shipped.
4. **Codex's deployed tool surface.** Only the public checkout (`ad9e809`) was
   read; it has `view_image` and no browser tool. Whether the hosted product adds
   more is undetermined.
5. **Internal adoption data at any vendor.** All "agent sees screenshots", "agent
   decides on its own" claims are vendor documentation with no published
   telemetry. Contrast with our own case, where adoption is *measured* (git_blame
   0/21 435) — the field's claims and our evidence are not the same kind of thing.
6. **Unrelated but noticed:** `go test ./internal/standards/ -run TestDocManifest`
   in this worktree fails at master already with 4 unclassified devdoc files
   (`build-cache-tree-gc.md`, `help-shot-row-index.md`, `scratch-gc-declared-trees.md`,
   `voice-input-format-contract.md`). Reported as an observation, not fixed here
   (this task is research-only and touches only its own devdoc file + DOC-INDEX row).

---



## 9. Evidence (self-filed)

Every claim this doc makes about a gate or a run carries its own observation:

- **Scope of the change.** `git diff master...HEAD --stat` in this worktree printed:

  ```
  devdoc/DOC-INDEX.tsv                    |   1 +
  devdoc/agent-ui-observation-research.md | 767 ++++++++++++++++++++++++++++++++
  2 files changed, 768 insertions(+)
  ```

  No production `.go` file appears — this run touched `devdoc/` and one row of its
  manifest.
- **Rule 30.** `git status --porcelain` in this worktree printed **nothing**
  (observed immediately before and after the commit).
- **Doc-class gates, before and after.** `go test ./internal/standards/ -run TestDocManifest`:
  - on the stashed (unmodified) worktree: `2101 rows cover 2105 files`, failing on
    **4** files — `build-cache-tree-gc.md`, `help-shot-row-index.md`,
    `scratch-gc-declared-trees.md`, `voice-input-format-contract.md`;
  - with this change applied: the **same 4** files, now `2102 rows cover 2106 files`.

  So the gate was already red at master, `agent-ui-observation-research.md` is
  **not** among its failures, and my row is counted. That pre-existing failure is
  reported, not fixed here (§8.6) — this task is research-only.
- **Anchor gate.** `go test ./internal/agent/ -run TestDocAnchor` printed zero
  lines matching `agent-ui-observation-research` — the doc is classed
  `historical`, so no URL in it is gated as an anchor. (That gate has its own
  pre-existing failures: 3 new / 8 stale permits, none of them mine.)
- **No `make fmt` was run**, and the local leak tree was read read-only: no write
  ever targeted `/home/user/work/Downloads/claude-code-system-prompts`.
- **Evidence trail.** Every §2–§6 claim carries a URL or a local path; local quotes
  name their file and its `ccVersion` (read from each file's own metadata header,
  not from the README's index).
