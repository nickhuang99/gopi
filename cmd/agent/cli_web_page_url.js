// cli_web_page_url.js — prints the URL the SHIPPED page's own fetch helper
// builds for one same-origin endpoint, given a location.search.
//
// The page's URL building (withToken) is browser-side code: it cannot be
// imported by a Go test and it must not be re-implemented in one (a copy proves
// nothing about the page). This harness extracts the real <script> out of
// cli_web.go, evaluates it with the page harness' shim, and calls the page's own
// helper — so the Go test asserts what the BROWSER will send.
//
// usage: node cli_web_page_url.js <endpoint-url> <location-search>
//   e.g. node cli_web_page_url.js /api/chat '?t=abc123'   → /api/chat?t=abc123
'use strict';

const path = require('path');
const { loadPage } = require('./cli_web_page_harness.js');

const endpoint = process.argv[2];
const search = process.argv[3] || '';
if (!endpoint) {
  console.error('usage: node cli_web_page_url.js <endpoint-url> [location-search]');
  process.exit(2);
}

const ctx = loadPage(path.join(__dirname, 'cli_web.go'));
ctx.state.locationSearch = search;
const built = ctx.api.withToken(endpoint);
if (typeof built !== 'string' || built === '') {
  console.error('the page\'s withToken returned ' + JSON.stringify(built));
  process.exit(3);
}
process.stdout.write(built);
