// SPDX-License-Identifier: GPL-2.0-only
// webui_smoke.js — board-free frontend smoke test (jsdom + fixtures)
//
// Why this exists: `webui_check.js` is a "real board + real hardware" behavior regression, it must touch the board
// (it builds test chains, flips settings and flips them back) — it cannot run while the owner is listening to music. But this file sends **zero write
// requests**: every API is answered with tools/fixtures/webui/*.json fixtures, any PUT/POST is intercepted
// and logged. So UI changes can be verified any time, no need to wait for the owner to stop listening.
//
// It pins down 10 things for 0.3 (two-block IA + backend-reported budget, all fragile spots):
//   ① Page load: stylesheet parses cleanly, no script exceptions;
//   ② **Two blocks**: effect card strip + settings list, fixed order; page has no 0.2 "type" layer and
//      no "genre preset" set (a duplicate second skin, removed per owner request);
//   ③ All 4 built-in cards present (community picks; other 8 removed on 2026-09-29), pick one → each setting follows the card,
//      switches light up accordingly, top shows the card name; custom cards/copy/delete see ⑪;
//   ④ Settings list: 16 entries **always present** (name + switch + current value per row), click a row to expand controls;
//   ⑤ Change any value → top becomes "Custom" (card name no longer claims to hold);
//   ⑥ Slot badges and top limit come from **backend-reported** (/api/dsp/budget), not hardcoded in panel;
//      Over limit only marks red, **never auto-disables anything**;
//   ⑦ Bypass switch / source / volume each send their own request, body shape correct;
//   ⑧ Capacity-400 error is distilled into one human line (0.3 §4.2 machine-readable fields), not a full per-segment dump;
//   ⑨ buildChain request body: type carries card name, gain_db **always present**, disabled items send {off:true};
//  ⑩ Zero board writes throughout (all write requests intercepted by this test).
//
// ⚠️ **Run both versions** (the public English version is not a "dead page that only passes identity"):
//     node tools/webui_smoke.js webui/index.html
//     node tools/webui_smoke.js linux/debian/app/webui/index.html
//   So assertions always find rows by **cap key** (data-hd, structure) and compare badges by **number**, never touching copy.
//   Finding rows by Chinese display names once broke the whole English version — that meant the gate only tested CN, i.e. no EN coverage.
//
// Usage: NODE_PATH=<jsdom>/node_modules node tools/webui_smoke.js [html path]

const fs = require('fs');
const path = require('path');
const { JSDOM, VirtualConsole } = require('jsdom');

const HTML = process.argv[2] || path.join(__dirname, '..', 'webui', 'index.html');
// Expected built-in card count (release panel ships with zero cards: node tools/webui_smoke.js <file> 0).
// Default 4 = current CN/EN panels; with 0, skip all card-touching cases (select/delete/restore).
const EXPECT_CARDS = parseInt(process.argv[3] || '4', 10);
const FIX = path.join(__dirname, 'fixtures', 'webui');

let fail = 0;
const check = (c, m) => { console.log((c ? '  ✓ ' : '  ✗ ') + m); if (!c) fail++; };
const section = t => console.log('\n' + t);
const sleep = ms => new Promise(r => setTimeout(r, ms));
const readFix = n => JSON.parse(fs.readFileSync(path.join(FIX, n + '.json'), 'utf8'));

// 0.3 panel only reads these five APIs (0.2 /api/dsp/types and /api/dsp/eq/parse are gone).
// ⚠️ Missing one entry = that fetch falls through to the next match or returns {}, symptom is "one row value does not refresh",
//    hard to trace back to a missing fixture-table entry.
const FIXMAP = {
  '/api/dsp/capabilities': 'capabilities',
  '/api/dsp/chain': 'chain',
  '/api/dsp/ddc': 'ddc',
  '/api/dsp/budget': 'budget',
  '/api/status': 'status',
};

function fixtureFor(url) {
  const clean = url.replace(/^https?:\/\/[^/]+/, '').split('?')[0];
  const name = FIXMAP[clean];
  return name ? readFix(name) : null;
}

(async () => {
  if (!fs.existsSync(HTML)) { console.error('Page not found: ' + HTML); process.exit(2); }
  for (const n of Object.values(FIXMAP)) {
    if (!fs.existsSync(path.join(FIX, n + '.json'))) {
      console.error('Missing fixture: ' + n + '.json'); process.exit(2);
    }
  }
  console.log(`\nFrontend smoke (board-free): ${HTML}\nFixtures: ${FIX}`);

  const errors = [];
  const writes = [];
  const vc = new VirtualConsole();
  vc.on('jsdomError', e => errors.push('jsdomError: ' + e.message));
  vc.on('error', (...a) => errors.push('console.error: ' + a.join(' ')));

  // Capacity 400: the GET /api/dsp/budget call still uses fixtures, but the following PUT chain must return a **real** capacity error.
  // Only a real response body (not made up) can test the backend contract of capMsg reading fields.
  const CAPACITY_400 = JSON.stringify({
    error: 'Not enough slots: this chain needs 25 slots, engine only has 24 (takes the smaller of CAP2-reported NSLOT and the software constant; overflow would wrap and overwrite earlier slots, so rejected here)',
    code: 'capacity', what: 'slots', needed: 25, limit: 24,
  });
  let failNextPut = false;      // Enabled by the test itself: next PUT /api/dsp/chain returns 400

  // ⚠️ Fixtures must be **stateful**, otherwise the panel's most important promise cannot be tested.
  //
  // Reason: after every change the panel runs schedule → push → refresh, and refresh re-reads S from
  // GET /api/dsp/chain and redraws. With static fixtures, local edits would be faithfully
  // overwritten by the "board" — symptom is "switch will not flip", looking like a broken panel while testing a nonexistent board.
  // Only a stateful mock (PUT records into board, GET serves from board) can verify "what you see is what is on the board".
  const FX_CHAIN = readFix('chain');
  const EFFECT_KEYS = ['dyn_bass', 'dynamic_bass', 'crossfeed', 'surround', 'colorfulmusic',
    'exciter', 'vse', 'viperbass', 'clarity', 'speaker_correction', 'analogx', 'tube'];
  const board = Object.assign({}, FX_CHAIN, { chain: (FX_CHAIN.chain || []).slice() });
  let boardLimiter = Object.assign({ on: true, mode: 'truepeak', thr_db: -1, rel_ms: 60 },
                                   FX_CHAIN.limiter || {});
  let boardLoudness = Object.assign({ on: false, strength: 0.5, offset_db: 10 },
                                    FX_CHAIN.loudness || {});
  let boardGainDB = (typeof FX_CHAIN.gain_db === 'number') ? FX_CHAIN.gain_db : 0;
  let boardType = FX_CHAIN.type || '';
  const applyBody = b => {
    for (const k of EFFECT_KEYS) {
      const v = b[k];
      if (v === undefined) continue;                 // Absent = untouched (same semantics as backend)
      board[k] = (v && v.off === true) ? null : v;   // off:true = off
    }
    if (Array.isArray(b.chain)) board.chain = b.chain.map(x => Object.assign({}, x));
    if (typeof b.gain_db === 'number') boardGainDB = b.gain_db;
    if (typeof b.type === 'string' && b.type) boardType = b.type;
  };
  const chainView = () => Object.assign({}, board, {
    gain_db: boardGainDB, type: boardType,
    limiter: boardLimiter, loudness: boardLoudness,
  });

  const dom = new JSDOM(fs.readFileSync(HTML, 'utf8'), {
    url: 'http://smoke.local/', runScripts: 'dangerously', pretendToBeVisual: true, virtualConsole: vc,
    beforeParse(window) {
      window.fetch = async (input, init) => {
        const url = typeof input === 'string' && input.startsWith('http') ? input : 'http://smoke.local' + input;
        const method = ((init && init.method) || 'GET').toUpperCase();
        const p = url.replace(/^https?:\/\/[^/]+/, '');
        const ok = body => new Response(JSON.stringify(body),
          { status: 200, headers: { 'Content-Type': 'application/json' } });
        if (method !== 'GET') {                       // ← Log only, never sent anywhere
          writes.push({ method, path: p, body: (init && init.body) || '' });
          let b = null;
          try { b = JSON.parse((init && init.body) || 'null'); } catch (e) {}
          if (p === '/api/dsp/chain' && b) {
            if (failNextPut) {                        // Capacity error: **do not** touch board (backend rolls back)
              failNextPut = false;
              return new Response(CAPACITY_400,
                { status: 400, headers: { 'Content-Type': 'application/json' } });
            }
            applyBody(b);
            return ok({ ok: true, applied: (b.chain || []).length, preamp_db: 0 });
          }
          // ⚠️ POST **echo shape** differs from the GET /api/dsp/chain view shape (former is
          // {enabled:…}, latter is {on:…}). The mock must fold on back itself; stuffing the request body straight into
          // board makes the panel read "this item is on" — symptom is "it flips to Custom right after picking a card".
          // (The first mock failed here: it faithfully stored the request body, but faithfully stored the wrong shape.)
          if (p === '/api/dsp/limiter' && b) {
            boardLimiter = Object.assign({}, boardLimiter, { on: b.enabled !== false });
            if (b.enabled !== false)
              Object.assign(boardLimiter, { mode: b.mode, thr_db: b.thr_db, rel_ms: b.rel_ms });
            return ok({ ok: true });
          }
          if (p === '/api/dsp/loudness' && b) {
            boardLoudness = Object.assign({}, boardLoudness, { on: !!b.enabled });
            if (b.enabled) Object.assign(boardLoudness, { strength: b.strength, offset_db: b.offset_db });
            return ok({ ok: true });
          }
          if (p === '/api/dsp/bypass' && b) { return ok({ ok: true }); }
          if (p === '/api/source/select' && b) { return ok({ ok: true }); }
          if (p === '/api/volume' && b) { return ok({ ok: true }); }
          return ok({ ok: true, applied: 1, preamp_db: 0 });
        }
        if (p === '/api/dsp/chain') return ok(chainView());
        const j = fixtureFor(url);
        return ok(j === null ? {} : j);
      };
      window.WebSocket = class { constructor() { this.readyState = 0; } close() {} };
    },
  });

  await sleep(1800);
  const win = dom.window, doc = win.document;
  const $ = id => doc.getElementById(id);
  const qsa = (s, root) => [...(root || doc).querySelectorAll(s)];
  const click = el => { if (el) el.dispatchEvent(new win.MouseEvent('click', { bubbles: true })); };
  const wait = ms => new Promise(r => setTimeout(r, ms));

  // ⚠️ **Re-query the DOM every time**. renderCaps()/renderCards() rebuild the whole list via box.innerHTML='' on each state change
  // ⇒ any cached row/switch is already detached before the second click; clicking it does nothing.
  // (This smoke test failed exactly here: turning on 7 items only turned on 5, looking like the panel "auto-trimmed", but I was clicking thin air.
  //   That symptom is exactly what the "never auto-trim" rule guards against — the test violating it is sneakier than the panel violating it.)
  // ⚠️ Find rows by **cap key**, not by display name — display names are copy (CN name / EN "Tube warmth"),
  // and this gate **must run both versions**. `data-hd` is structure, identical in both versions, hence a stable anchor.
  const rowByKey = k => doc.querySelector('#caps .cap .hd[data-hd="' + k + '"]') &&
    doc.querySelector('#caps .cap .hd[data-hd="' + k + '"]').parentElement;
  const swByKey = k => { const r = rowByKey(k); return r ? r.querySelector('.hd .sw') : null; };
  const bodyByKey = k => { const r = rowByKey(k); return r ? r.querySelector('.body') : null; };
  const sumByKey = k => { const r = rowByKey(k); return r ? (r.querySelector('.hd .sum') || {}).textContent || '' : ''; };
  const badgeByKey = k => { const r = rowByKey(k); return r ? (r.querySelector('.hd .cost') || {}).textContent || '' : ''; };
  const isOpen = k => { const r = rowByKey(k); return !!r && r.className.indexOf('open') >= 0; };
  const isOn = k => { const s = swByKey(k); return !!s && s.className.indexOf('on') >= 0; };
  const setOpen = async (k, want) => { if (isOpen(k) !== want) { click(rowByKey(k).querySelector('.hd')); await wait(160); } };
  const setOn = async (k, want) => { if (isOn(k) !== want) { click(swByKey(k)); await wait(380); } };
  const onCount = () => qsa('#caps .cap .hd .sw.on').length;
  const reqs = p => writes.filter(w => w.path === p);
  const topSlots = () => parseInt((($('slots').textContent || '').match(/(\d+)/) || [0, 0])[1], 10);

  // ───────────────────────────────────────────── ① Load
  section('① Page load / stylesheet');
  const cssBad = errors.filter(e => /Could not parse CSS|stylesheet/i.test(e));
  check(cssBad.length === 0, 'Stylesheet parses without jsdom errors' + (cssBad.length ? ':' + cssBad[0].slice(0, 160) : ''));
  check(errors.length === 0, 'Loads with no exceptions' + (errors.length ? ':' + errors.join(' | ').slice(0, 240) : ''));
  // 4 built-in cards (only community picks since 2026-09-29; see effect-cards.json for the full 12-card set).
  // Custom cards come from backend types, absent from fixtures ⇒ only the 4 built-ins can appear here.
  check(qsa('#cards .card').length === EXPECT_CARDS, `Effect card strip renders ${EXPECT_CARDS} built-in cards (actual ${qsa('#cards .card').length})`);
  check(qsa('#caps .cap').length === 17, `Settings list renders 17 capabilities (actual ${qsa('#caps .cap').length})`);

  // ───────────────────────────────────────────── ② Two-block IA
  section('② IA: only two blocks (effects / settings)');
  const before = (a, b) => !!(a && b && (a.compareDocumentPosition(b) & win.Node.DOCUMENT_POSITION_FOLLOWING));
  check(before($('cards'), $('caps')), 'Card strip comes before settings list');
  check(!!$('cur') && !!$('slots'), 'Top has the "current effect + slots" row');
  check(!!doc.querySelector('.sechd'), 'Has the "Effects" section header');
  // Things removed in 0.2 must not sneak back: type layer (custom/create/copy/rename/delete), genre presets, theming
  check(!$('type-list') && !doc.querySelector('.type-item'), 'No 0.2 "type" layer');
  check(!$('preset-list') && !$('btn-save-preset') && !doc.querySelector('.pst-item'), 'No "genre preset" block');
  check(!$('theme-pick') && !doc.documentElement.getAttribute('data-theme'), 'No theming controls (0.3 panel has no themes)');

  // ── P0 feel metrics: the plan promises "min font size >=12px, hit area >=44px", long unwatched.
  //    Measured here from **stylesheet text** (jsdom does no layout, getComputedStyle cannot measure real boxes).
  //    Why it deserves its own section: when these numbers drop, eyes cannot tell but fingers can — waiting for user feedback is too late.
  section('②a Feel metrics (from stylesheet, not rendered output)');
  const srcCss = fs.readFileSync(HTML, 'utf8');
  const cssTxt = (srcCss.slice(srcCss.indexOf('<style>'), srcCss.indexOf('</style>'))
    || '').replace(/\/\*[\s\S]*?\*\//g, '');
  const decl = sel => {
    const m = cssTxt.match(new RegExp('(^|})\\s*' + sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'));
    return m ? m[2] : '';
  };
  const px = (body, prop) => {
    const m = body.match(new RegExp(prop + '\\s*:\\s*([0-9.]+)px'));
    return m ? parseFloat(m[1]) : -1;
  };
  const fontSizes = [...cssTxt.matchAll(/font-size:\s*([0-9.]+)(px|em|rem)/g)]
    .map(m => parseFloat(m[1]) * (m[2] === 'px' ? 1 : 16));
  const minFont = Math.min.apply(null, fontSizes);
  check(minFont >= 12, `Min font size = ${minFont}px(requires >=12px; the 11.5px spots were slot badges/headers, raised to 12 in 0.3)`);

  // Hit-area checklist: key = selector, value = [property, minimum]. ⚠️ Only measure the visible/clickable element itself,
  // not the ::before/::after painted parts (they are visuals; hit area belongs to the element).
  const HIT = [
    ['.sw', 'height', 44, 'Capability switch (one per capability, tapped dozens of times a day)'],
    ['input[type=range]', 'height', 44, 'Slider (44px element, 6px track)'],
    ['.seg button', 'min-height', 44, 'Segmented buttons (steps/sources)'],
    ['.seg.sm button', 'min-height', 44, 'Source-row segmented buttons'],
    ['.band input[type=number]', 'min-height', 44, 'Per-band EQ number box'],
    ['.band .x', 'min-height', 44, 'Per-band EQ delete key'],
    ['.card', 'min-height', 44, 'Effect card'],
    ['.hd', 'min-height', 44, 'Settings list row (click to expand)'],
  ];
  for (const [sel, prop, min, why] of HIT) {
    const v = px(decl(sel), prop);
    check(v >= min, `${sel} ${prop} = ${v >= 0 ? v + 'px' : '(not found)'} (requires >=${min}px) — ${why}`);
  }
  // The visible switch "pill" must not vanish/deform just because the hit area was enlarged
  check(/\.sw::before[^{]*\{[^}]*height:\s*26px/.test(cssTxt),
    'Visible switch pill still present (::before height 26px) — only the hit area was enlarged, not the pill');
  check(/\.sw\s*\{[^}]*margin:\s*-9px/.test(cssTxt),
    'Switch keeps margin:-9px (44px hit area only occupies 26px in layout, so the top row is not pushed taller)');

  section('②b Entrances removed in 0.2 (check source for what DOM cannot tell)');
  const src0 = srcCss;
  check(src0.indexOf('zybo_theme') < 0, 'No zybo_theme key in page (theming removed)');
  check(src0.indexOf('zybo_gain_db') >= 0, 'But zybo_gain_db remains (overall gain reuses the old key, old values come back automatically)');
  check(!$('btn-import'), 'Import-EQ entrance removed (device correction goes via autoeq.html)');
  check(src0.indexOf('autoeq.html') >= 0, 'Device-correction entry points to autoeq.html');

  // ───────────────────────────────────────────── ③ Pick a card
  section('③ Pick a card: settings follow the card');
  if (EXPECT_CARDS === 0) { check(true, 'Factory has no cards: skip select/delete/restore cases'); } else {
  // Card name comes from the first text node: badge span (⧉/✕) copy would pollute textContent
  const heavy = qsa('#cards .card').find(c => (c.firstChild && c.firstChild.textContent) === 'ClearPenguin');
  check(!!heavy, 'Found the ClearPenguin card');
  const limBefore = isOn('limiter');      // Criterion is "picking a card does not touch it", not "it was off to begin with"
  if (heavy) {
    click(heavy);
    await wait(600);
    check($('cur').textContent === 'ClearPenguin', `Top shows card name (${$('cur').textContent})`);
    check(heavy.className.indexOf('on') >= 0, 'Selected card is highlighted');
    // ClearPenguin = dyn_bass(gain 5) + 10-band EQ; limiter/convolver/device correction are not in the card
    check(isOn('dyn_bass'), 'Capability written in card (DYN) is switched on');
    check(sumByKey('dyn_bass').indexOf('5') >= 0,
      `DYN row shows the **card value** (${sumByKey('dyn_bass')})`);
    check(!isOn('viperbass'), 'Capability absent from card (bass boost) is **not** turned on');
    check(isOn('limiter') === limBefore, 'Limiter is protection-grade, picking a card does not touch it');
    check($('cur').textContent === 'ClearPenguin',
      'Card still recognized after refresh (matchCard must match)');
  }

  // ───────────────────────────────────────────── ④ Settings list
  }
  section('④ Settings list: 17 entries always present, click a row to expand');
  check(!!rowByKey('tube') && !!rowByKey('speaker_correction'),
    'Both tube-warmth and speaker-correction rows present (even when off)');
  // Capabilities without adjustable params (tube / speaker correction) **deliberately** generate no body and are not expandable
  check(!bodyByKey('tube'), 'Tube has no body container (nothing adjustable, so no space taken)');
  // Expandable rows show a chevron marker; rows without a body do not; the chevron flips once expanded
  check(!!rowByKey('clarity').querySelector('.hd .chev'), 'Expandable rows have a chevron');
  check(!rowByKey('tube').querySelector('.hd .chev'), 'Rows without a body have no chevron');
  check(!!bodyByKey('clarity'), 'Capability with adjustable params (clarity) has a body container');
  check(!isOpen('clarity'), 'Collapsed by default');
  await setOpen('clarity', true);
  check(isOpen('clarity'), 'Click a row → expands');
  const clrBody = bodyByKey('clarity');
  check(!!clrBody && qsa('.seg button', clrBody).length === 3,
    `Clarity expands to three steps (actual ${clrBody ? qsa('.seg button', clrBody).length : 0})`);
  await setOpen('clarity', false);
  check(!isOpen('clarity'), 'Click again → collapses');
  const eqBody = bodyByKey('eq');
  check(!!eqBody, 'EQ row has a body (band editor)');
  check(!!eqBody && qsa('.ehd span', eqBody).length >= 3,
    'EQ has column headers (freq/gain/Q) — three bare number boxes mean nothing without them');

  // ───────────────────────────────────────────── ⑤ Change value → Custom
  section('⑤ Change one value → top becomes "Custom" (card name no longer claims to hold)');
  await setOn('tube', true);
  check(isOn('tube'), 'Switch flipped');
  check($('cur').textContent !== 'ClearPenguin',
    `Top no longer claims that card (shows ${$('cur').textContent}, i.e. the "Custom" state)`);
  await setOn('tube', false);

  // ───────────────────────────────────────────── ⑥ Budget from backend
  section('⑥ Slot budget comes from backend (not hardcoded in panel)');
  const fxBudget = readFix('budget');
  const per = {};
  fxBudget.per_effect.forEach(e => per[e.id] = e.slots);
  check(new RegExp('/\\s*' + fxBudget.limit + '\\b').test($('slots').textContent || ''),
    `Top limit is backend-reported ${fxBudget.limit}(${$('slots').textContent}) — NSLOT on old bitstreams may not be 24`);

  await setOn('viperbass', true);
  const badgeNum = k => parseInt((badgeByKey(k).match(/\d+/) || [0, -1])[0], 10);
  check(badgeNum('viperbass') === per.viperbass,
    `NATURAL step badge = backend-reported ${per.viperbass} slots (actual ${badgeByKey('viperbass') || 'not drawn'})`);
  await setOpen('viperbass', true);
  const vbBody = bodyByKey('viperbass');
  const pbpBtn = vbBody ? [...vbBody.querySelectorAll('.seg button')].find(b => /Pure Bass/.test(b.textContent)) : null;
  check(!!pbpBtn, 'Bass boost expands to show the "Pure Bass+" step');
  if (pbpBtn) {
    click(pbpBtn);
    await wait(500);
    check(badgeNum('viperbass') === per.viperbass_pbp,
      `Badge after switching to PBP = backend-reported ${per.viperbass_pbp} slots (actual ${badgeByKey('viperbass')}) —`
      + 'Same effect can cost different slots per step; hardcoding one value would mislead');
  }
  await setOn('viperbass', false);
  check(topSlots() > 0, `Top shows an occupancy estimate (${topSlots()} slots)`);

  // ⚠️ Never auto-trim: turning on many must not silently switch anything off
  const many = ['viperbass', 'dynamic_bass', 'clarity', 'vse', 'analogx', 'crossfeed', 'colorfulmusic'];
  for (const k of many) await setOn(k, true);
  const afterOn = onCount();
  check(many.every(k => isOn(k)),
    `After turning on ${many.length} items, each is still on (${afterOn} on)`);
  check(topSlots() > fxBudget.limit, `This combo really exceeds the limit (${topSlots()} > ${fxBudget.limit})`);
  check(($('slots').className || '').indexOf('over') >= 0, 'Top marks red when over the limit');
  check(many.every(k => isOn(k)) && onCount() === afterOn,
    'Still nothing auto-disabled after marking red (**mark red only, never trim**)');
  for (const k of many) await setOn(k, false);   // Switch each back off so later assertions are not skewed

  // ───────────────────────────────────────────── ⑦ Three controls
  section('⑦ Bypass / source / volume each send their own request');
  const nByp0 = reqs('/api/dsp/bypass').length;
  click($('master'));
  await wait(400);
  const byp = reqs('/api/dsp/bypass');
  check(byp.length === nByp0 + 1, `One click → one /api/dsp/bypass (${byp.length} total)`);
  let bBody = null; try { bBody = JSON.parse(byp[byp.length - 1].body); } catch (e) {}
  check(!!bBody && bBody.bypass === true, `Body is {bypass:true} (${byp.length ? byp[byp.length - 1].body : '-'})`);
  check((($('masterTxt') || {}).textContent || '').length > 0,
    `Has a status line next to it (${($('masterTxt') || {}).textContent})`);

  const btns = qsa('#srcs button');
  check(btns.length === 4, `4 source steps (${btns.map(b => b.textContent).join(' / ')})`);
  if (btns.length >= 2) {
    const nSrc0 = reqs('/api/source/select').length;
    click(btns[1]);
    await wait(400);
    const s = reqs('/api/source/select');
    check(s.length === nSrc0 + 1, `One click → one /api/source/select (${s.length} total)`);
    let sBody = null; try { sBody = JSON.parse(s[s.length - 1].body); } catch (e) {}
    check(!!sBody && !!sBody.source, `Body carries source (${s.length ? s[s.length - 1].body : '-'})`);
  }

  const vol = $('vol');
  vol.value = 55;
  vol.dispatchEvent(new win.Event('change', { bubbles: true }));
  await wait(400);
  const v = reqs('/api/volume');
  check(v.length >= 1, `Drag volume → one /api/volume (${v.length} total)`);
  let vBody = null; try { vBody = JSON.parse(v[v.length - 1].body); } catch (e) {}
  check(!!vBody && vBody.volume === 55, `Body is {volume:55} (${v.length ? v[v.length - 1].body : '-'})`);

  // ───────────────────────────────────────────── ⑧ Capacity error
  section('⑧ Capacity 400: distilled into one human line (0.3 §4.2 machine-readable fields)');
  failNextPut = true;
  await setOpen('clarity', true);
  const r0 = qsa('#caps .cap .body input[type=range]')[0];
  check(!!r0, 'Found a slider to trigger a push');
  if (r0) {
    r0.value = String(parseFloat(r0.value || r0.min || 0) + 1);
    r0.dispatchEvent(new win.Event('input', { bubbles: true }));
  }
  await wait(900);
  const logTxt = $('log').textContent || '';
  check(logTxt.indexOf('25') >= 0 && logTxt.indexOf('24') >= 0,
    `Status line uses backend needed/limit (${logTxt.slice(0, 90)})`);
  check(logTxt.indexOf('PK@') < 0 && logTxt.length < 160,
    'Backend per-segment dump is not pasted verbatim (old 0.2 symptom)');
  check(($('log').className || '').indexOf('e') >= 0, 'Marked with error styling');

  // ───────────────────────────────────────────── ⑨ buildChain body
  section('⑨ Push body shape (type / gain_db / off:true)');
  const puts = reqs('/api/dsp/chain');
  check(puts.length >= 1, `PUT /api/dsp/chain ${puts.length} times`);
  check(puts.every(w => w.method === 'PUT'), 'Chain only uses PUT (backend only accepts GET / PUT, POST is rejected)');
  let body = null;
  for (let i = puts.length - 1; i >= 0; i--) {
    try { body = JSON.parse(puts[i].body); break; } catch (e) {}
  }
  check(!!body, 'Last body is valid JSON');
  if (body) {
    check(typeof body.type === 'string' && body.type.length > 0,
      `type carries current card name/Custom (${JSON.stringify(body.type)}) — board archive then matches panel`);
    check(body.hasOwnProperty('gain_db'),
      'gain_db **always present** (backend is *float64, missing = keep old value; once silently cleared to 0)');
    check(Array.isArray(body.chain), 'chain is an array');
    check(Object.keys(body).some(k => body[k] && body[k].off === true),
      'Disabled capabilities send {off:true} (not absent — absent means "untouched", so it would never switch off)');
  }

  // ───────────────────────────────────────────── ⑪ Custom cards (save/copy/delete entrances present, not clicked)
  // Static DOM only: clicking "Save as effect" would pop a prompt, delete would pop a confirm; fixtures never click either
  // (clicking would not really save anyway: all writes are intercepted). The real save/delete loop runs in the real-board gate.
  section('⑪ Custom-card entrances (present statically, not clicked)');
  check(!!doc.querySelector('#caps + .seg #cardSave'), 'Settings area has a "Save as effect" button below');
  check(!!$('bkSave') && !!$('bkRestore') && !!$('bkfile'), 'Settings area has backup/restore buttons (+ hidden file picker)');
  check(!!$('cardEdit'), 'Effect header row has an "edit" button (card strip normally has zero buttons)');
  check(!!$('sheetWrap') && !!$('sheetOk') && !!$('sheetDel') && !!$('sheetCancel'),
    'In-page sheet present (naming/confirm-delete go through it, no native prompt/confirm)');
  // Edit mode is off in fixtures: badge nodes exist (one copy button per card), but strip has no manage class ⇒ CSS hides them all
  check(qsa('#cards .bdg.dup').length === qsa('#cards .card').length, 'One copy badge per card (hidden by CSS)');
  check(!$('cards').classList.contains('manage'), 'Strip normally has no manage (zero visible badges)');
  check(!$('cardRestore'), 'No hidden built-in cards ⇒ restore button stays hidden');

  // ───────────────────────────────────────────── ⑩ Zero board writes
  section('⑩ Zero board writes throughout');
  console.log('     Intercepted write requests:');
  writes.forEach(w => console.log(`       ${w.method} ${w.path} ${w.body.slice(0, 70)}`));
  check(writes.length > 0, '(All these write requests were intercepted by this test, sent nowhere)');
  check(!writes.some(w => /\/api\/dsp\/presets|\/api\/dsp\/eq\/parse/.test(w.path)),
    'Did not touch the API sets removed in 0.2 (genre presets / import parsing).' +
    '/api/dsp/types exception: custom cards live in it, but this test clicked no button, so no writes are expected');

  console.log(fail ? `\n== smoke ${fail} FAILED ==` : '\n== frontend smoke all passed ==');
  process.exit(fail ? 1 : 0);
})().catch(e => { console.error('\nSmoke script crashed:', e); process.exit(3); });
