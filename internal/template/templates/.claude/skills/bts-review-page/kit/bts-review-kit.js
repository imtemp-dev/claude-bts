/* bts review kit v3.
 *
 * Two transports behind one UI: a claude.ai artifact (window.claude.use:
 * db, comments, dictation, permissions), or a page `bts review serve`
 * serves from the project (config "transport": "local", which the server
 * adds): page data over the server's JSON API and event stream, a round
 * sent by writing it, dictation through the browser's speech recognition.
 *
 * Everything a bts review page needs that is the same on every page: note
 * mode, the note editor with dictation, pins, the Review tray, rounds sent
 * as one comment each, round progress, proposals with Accept and Reject,
 * Update plan, and the confirm rows.  The page draws the change; the kit
 * makes it reviewable.
 *
 * The page provides:
 *   <script type="application/json" id="bts-review-config">
 *     {"recipe": "r-001-x", "kit": "2", "docs": {"intent": {"ver": "v1", "hash": "…"}, …},
 *      "confirm": ["intent", "scope"], "page": "page v1", "lang": "ko"}
 *   </script>
 *   A feature's page names "feature": "f-005-x" in place of "recipe"; its
 *   one confirm row is the "feature" doc, whose "file" names what it covers.
 *   [data-note="<section> › <part>"] on every reviewable element,
 *   [data-bts-decision="<key>"] containing [data-bts-option="<value>"] cards,
 *   optionally [data-bts-proposals-slot] and [data-bts-confirm-slot].
 * The page declares the capabilities `bts review page` prints.
 * Contract: docs/research/review-pages-design.md in claude-bts.
 */
(() => {
  'use strict';
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };

  let CFG = {};
  try { CFG = JSON.parse(($('#bts-review-config') || {}).textContent || '{}'); } catch (e) { CFG = {}; }
  const DOCS = CFG.docs || {};
  const PAGE = [CFG.page].concat(Object.keys(DOCS).map((k) => k + ' ' + DOCS[k].ver)).filter(Boolean).join(' · ');
  const RECIPE = CFG.recipe || '';
  const FEATURE = CFG.feature || '';
  const SUBJECT = FEATURE ? 'feature ' + FEATURE : 'recipe ' + RECIPE;
  const CONFIRM_CMD = FEATURE ? 'bts feature confirm --via review' : 'bts recipe confirm --via review';
  const LOCAL = CFG.transport === 'local';
  const API = typeof CFG.api === 'string' ? CFG.api : '';
  const STATUS = { draft: 'Draft', submitted: 'Sent', applied: 'Applied', declined: 'Declined', answered: 'Answered', proposed: 'Change proposed', page: 'Page fixed' };
  const reduced = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const S = { noting: false, comments: null, dict: null, dictOff: false, perms: null, db: null, local: false, dbReady: false, can: 'unknown', waiting: false, reloadPending: false,
    notes: new Map(), rounds: new Map(), proposals: new Map(), roundSeen: new Map(), ed: null, anchor: null, busy: false, hot: null, lang: CFG.lang || '' };

  /* ---------- UI scaffold ---------- */
  const bar = el('div', 'btsr-bar');
  bar.innerHTML =
    '<div class="btsr-bar-in">' +
    '<span class="btsr-title"></span><span class="btsr-ver"></span>' +
    '<span class="btsr-conn" data-can="unknown">Checking whether Claude is listening…</span>' +
    '<span class="btsr-chip" role="status" hidden></span>' +
    '<button type="button" class="btsr-btn" data-k="note" aria-pressed="false" title="Note mode (N)">Note <kbd>N</kbd></button>' +
    '<button type="button" class="btsr-btn" data-k="tray" aria-pressed="false">Review <span class="btsr-count"></span></button>' +
    '<button type="button" class="btsr-btn btsr-primary btsr-send" hidden>Send notes</button>' +
    '<button type="button" class="btsr-btn btsr-primary" data-k="decide" hidden>Update plan</button>' +
    '</div>';
  document.body.prepend(bar);
  $('.btsr-title', bar).textContent = CFG.title || document.title || 'Review';
  $('.btsr-ver', bar).textContent = PAGE;

  const pinLayer = el('div', 'btsr-pins');
  document.body.append(pinLayer);

  const note = el('div', 'btsr-note');
  note.hidden = true;
  note.setAttribute('role', 'dialog');
  note.innerHTML =
    '<div class="btsr-where">At: <b></b></div>' +
    '<textarea maxlength="3800" placeholder="What should change here? Speak or type."></textarea>' +
    '<div class="btsr-partial" aria-live="polite"></div>' +
    '<div class="btsr-reply" hidden></div>' +
    '<div class="btsr-row">' +
    '<button type="button" class="btsr-btn btsr-mic" hidden aria-pressed="false" title="Speak (Alt+M)">Speak</button>' +
    '<select class="btsr-lang" aria-label="Dictation language" hidden><option value="">Auto</option><option value="ko">Korean</option><option value="en">English</option></select>' +
    '<span class="btsr-grow"></span>' +
    '<button type="button" class="btsr-btn" data-k="del" hidden>Delete</button>' +
    '<button type="button" class="btsr-btn" data-k="close">Close</button>' +
    '<button type="button" class="btsr-btn btsr-primary" data-k="save">Save note</button>' +
    '</div>' +
    '<div class="btsr-status" aria-live="polite"></div>' +
    '<div class="btsr-row"><button type="button" class="btsr-btn" data-k="allow" hidden>Allow this page to comment for me</button></div>';
  document.body.append(note);
  const ta = $('textarea', note), partial = $('.btsr-partial', note), status = $('.btsr-status', note), reply = $('.btsr-reply', note);
  const saveBtn = $('[data-k=save]', note), delBtn = $('[data-k=del]', note), allowBtn = $('[data-k=allow]', note);
  const mic = $('.btsr-mic', note), langSel = $('.btsr-lang', note);
  langSel.value = S.lang;

  const tray = el('aside', 'btsr-tray');
  tray.hidden = true;
  tray.setAttribute('aria-label', 'Review notes');
  tray.innerHTML =
    '<div class="btsr-tray-head"><b>Your review</b><button type="button" class="btsr-btn" data-k="tclose">Close</button></div>' +
    '<div class="btsr-tray-list"></div>' +
    '<div class="btsr-tray-foot">' +
    '<button type="button" class="btsr-btn btsr-primary btsr-send" hidden>Send notes</button>' +
    '<div class="btsr-status" data-k="tstatus" aria-live="polite"></div>' +
    '<button type="button" class="btsr-btn" data-k="tallow" hidden>Allow this page to comment for me</button>' +
    '<div class="btsr-legend"><span><i class="s-draft"></i>Draft</span><span><i class="s-submitted"></i>Sent</span><span><i class="s-submitted btsr-pulse"></i>Working</span><span><i class="s-proposed"></i>Change proposed</span><span><i class="s-applied"></i>Applied</span><span><i class="s-answered"></i>Answered</span><span><i class="s-declined"></i>Declined</span></div>' +
    '<p class="btsr-fine">Answers come from the Claude session doing the work: on each pin and in its terminal. The comment thread Send creates is only a receipt.</p>' +
    '</div>';
  document.body.append(tray);
  const trayList = $('.btsr-tray-list', tray), trayStatus = $('[data-k=tstatus]', tray), trayAllow = $('[data-k=tallow]', tray);

  const toastEl = el('div', 'btsr-toast');
  toastEl.hidden = true;
  toastEl.setAttribute('role', 'status');
  document.body.append(toastEl);

  const props = el('section', 'btsr-proposals');
  props.hidden = true;
  props.setAttribute('data-note', 'Proposed changes');
  props.innerHTML =
    '<div class="btsr-eyebrow">Proposed changes · from your notes</div><h2>What Claude would change in the plan</h2>' +
    '<p class="btsr-muted">Nothing in the plan changes until you press Update plan. Accept or reject each proposal first.</p>' +
    '<div class="btsr-plist"></div>' +
    '<div class="btsr-row"><button type="button" class="btsr-btn btsr-primary" data-k="apply" disabled>Update plan</button><span class="btsr-status" data-k="pstatus"></span></div>';
  const confirmSec = el('section', 'btsr-confirm');
  confirmSec.innerHTML = '<div class="btsr-eyebrow">Confirm</div><h2>Sign off this version</h2>' +
    '<p class="btsr-muted">Send your notes first. Confirming sends the version\'s fingerprint to Claude, which checks it against the file before recording your answer.</p>';
  (CFG.confirm || Object.keys(DOCS)).forEach((k) => {
    const d = DOCS[k]; if (!d) return;
    const row = el('div', 'btsr-crow');
    row.dataset.note = 'Confirm › ' + k + ' ' + d.ver;
    row.dataset.doc = k; row.dataset.ver = d.ver; row.dataset.hash = d.hash;
    const what = el('div', 'btsr-what');
    what.append(el('b', '', k.charAt(0).toUpperCase() + k.slice(1) + ' ' + d.ver), el('span', 'btsr-muted', (d.file || k + '.md') + ' · fingerprint ' + d.hash), el('span', 'btsr-cstate'));
    const b = el('button', 'btsr-btn btsr-primary', 'Confirm ' + k); b.type = 'button'; b.dataset.k = 'confirm';
    row.append(what, b);
    confirmSec.append(row);
  });
  const main = $('main') || document.body;
  const pslot = $('[data-bts-proposals-slot]'), cslot = $('[data-bts-confirm-slot]');
  if (pslot) pslot.append(props); else main.append(props);
  if (cslot) cslot.append(confirmSec); else main.append(confirmSec);

  $$('[data-bts-decision] [data-bts-option]').forEach((opt) => {
    const key = opt.closest('[data-bts-decision]').dataset.btsDecision;
    if (!opt.dataset.note) opt.dataset.note = 'Decision ' + key + ' › ' + opt.dataset.btsOption;
    const b = el('button', 'btsr-btn', 'Pick this'); b.type = 'button'; b.dataset.k = 'pick';
    opt.append(b);
  });

  /* ---------- helpers ---------- */
  let toastTimer = 0;
  function toast(msg) { toastEl.textContent = msg; toastEl.hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => { toastEl.hidden = true; }, 4500); }
  const iso = () => new Date().toISOString();
  const newId = () => 'n-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 6);
  const enc = new TextEncoder();
  function fit(s, max) {
    max = max || 3800;
    if (enc.encode(s).length <= max) return s;
    let lo = 0, hi = s.length;
    while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (enc.encode(s.slice(0, mid)).length <= max - 16) lo = mid; else hi = mid - 1; }
    return s.slice(0, lo) + '\n[cut]';
  }
  function elFor(label) { return $$('[data-note]').find((e) => e.dataset.note === label) || null; }
  function excerptOf(e) { return e ? (e.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 160) : ''; }
  function sorted() { return [...S.notes].sort((a, b) => String(a[1].created || '').localeCompare(String(b[1].created || ''))); }
  function drafts() { return sorted().filter(([, n]) => n.status === 'draft' && n.kind !== 'confirm'); }
  function pendingRound() { for (const [, r] of S.rounds) if (r.status !== 'processed') return r.round; return null; }
  function nextRound() {
    let m = 0;
    for (const [, n] of S.notes) if (typeof n.round === 'number') m = Math.max(m, n.round);
    for (const [, r] of S.rounds) if (typeof r.round === 'number') m = Math.max(m, r.round);
    return m + 1;
  }
  function chosen(key) { const d = sorted().filter(([, n]) => n.kind === 'decision' && n.decision === key); return d.length ? d[d.length - 1][1].value : null; }
  function isWorking(n) { const r = n.round != null ? S.rounds.get('r-' + n.round) : null; return n.status === 'submitted' && !!r && r.status === 'picked'; }
  function ago(t) { const s = Math.max(0, Math.round((Date.now() - Date.parse(t)) / 1000)); return s < 90 ? s + ' s' : Math.round(s / 60) + ' min'; }
  function scrollTo(e) { if (e) e.scrollIntoView({ block: 'start', behavior: reduced ? 'auto' : 'smooth' }); }

  /* ---------- store: page data when available, this browser otherwise ---------- */
  const LKEY = 'bts-review:' + (FEATURE || RECIPE);
  function localLoad() { try { S.notes = new Map(Object.entries(JSON.parse(localStorage.getItem(LKEY) || '{}'))); } catch (e) { S.notes = new Map(); } }
  function localFlush() { try { localStorage.setItem(LKEY, JSON.stringify(Object.fromEntries(S.notes))); } catch (e) {} }
  const store = {
    async set(id, data) { if (S.db) return S.db.collection('notes').doc(id).set(data); S.notes.set(id, data); localFlush(); render(); },
    async patch(id, data) { if (S.db) return S.db.collection('notes').doc(id).update(data); S.notes.set(id, Object.assign({}, S.notes.get(id), data)); localFlush(); render(); },
    async del(id) { if (S.db) return S.db.collection('notes').doc(id).delete(); S.notes.delete(id); localFlush(); render(); },
    async round(id, data) { if (S.db) return S.db.collection('rounds').doc(id).set(data); S.rounds.set(id, data); render(); }
  };
  function sayTray(m, w) { trayStatus.textContent = m || ''; trayStatus.classList.toggle('btsr-warn', !!w); }
  function attachDb(db) {
    S.db = db;
    const slow = setTimeout(() => { if (!S.dbReady) sayTray('Notes are slow to load. They appear here once the page data answers.'); }, 10000);
    const toMap = (snap) => { const m = new Map(); snap.docs.forEach((d) => { if (d.exists) m.set(d.id, d.data()); }); return m; };
    db.collection('notes').onSnapshot((snap) => { clearTimeout(slow); S.dbReady = true; S.notes = toMap(snap); render(); },
      (e) => { clearTimeout(slow); sayTray('Notes could not load (' + ((e && e.code) || 'error') + '). Reload the page.', true); });
    db.collection('rounds').onSnapshot((snap) => {
      const m = toMap(snap);
      m.forEach((r, id) => {
        const prev = S.roundSeen.get(id);
        if (prev && prev !== r.status) {
          if (r.status === 'picked') toast('Claude picked up round ' + r.round + ' and is working on it.');
          else if (r.status === 'processed') toast(r.kind === 'apply' ? 'Claude updated the plan (round ' + r.round + '). The page shows the new version.' : 'Claude answered round ' + r.round + '. The answers are on the pins.');
        }
        S.roundSeen.set(id, r.status);
      });
      S.rounds = m; render();
    }, () => {});
    db.collection('proposals').onSnapshot((snap) => { S.proposals = toMap(snap); render(); }, () => {});
  }
  function goLocal() { S.local = true; localLoad(); render(); sayTray('Page data is unavailable here, so notes stay in this browser and Send puts them in the message itself.'); }

  /* ---------- connection and consent ---------- */
  const conn = $('.btsr-conn', bar);
  const CONN = LOCAL
    ? { available: 'A Claude session is waiting', no_session: 'No session is waiting: rounds are kept until one checks', off: 'The review server is not answering', none: 'The review server is not answering', unknown: 'Connecting to the review server…' }
    : { available: 'Claude is listening', no_session: 'No Claude session is watching', writers_only: 'Only editors can send to Claude',
      off: 'Sending to Claude is off in this view', none: 'Comments are unavailable in this view', unknown: 'Checking whether Claude is listening…' };
  function setConn(v) { S.can = v; conn.dataset.can = v; conn.textContent = CONN[v] || CONN.off; render(); }
  async function refreshCan() { const c = S.comments; if (!c) { setConn('none'); return; } try { setConn(await c.canSendToClaude()); } catch (e) { setConn('off'); } }
  function explain(code) {
    if (LOCAL) return code === 'forbidden'
      ? ['The review server refused this page. Open the link `bts review open` printed.', false]
      : ['The review server did not answer. Check it is running (`bts review open` restarts it), then send again. Your notes are kept.', false];
    if (code === 'consent_required') return ['This page may not comment for you yet. Press Allow below, then send again. Your notes are kept.', true];
    if (code === 'claude_unavailable') { setConn('off'); return ['Claude cannot take notes from this view right now. Send again to post them as a plain comment.', false]; }
    if (code === 'forbidden') return ['Commenting from this page is off for you here. Use claude.ai\'s own comment mode instead.', false];
    if (code === 'rate_limited') return ['That was quick. Wait a few seconds and send again.', false];
    if (code === 'invalid') return ['The message was refused as written. Try fewer notes at once.', false];
    return ['It did not go through. Send once more; if it fails again, reload the page.', false];
  }
  async function allowComments(say, btn) {
    const p = S.perms, RELOAD = 'Reload the page, send again, and answer the prompt claude.ai shows. Your notes are kept.';
    if (!p) { say(RELOAD, true); return; }
    let st = 'unavailable';
    try { st = await p.state('comments'); } catch (e) {}
    if (st === 'denied') {
      try { await p.manage(); } catch (e) { say('Open the artifact\'s Permissions menu, allow comments, then send again.', true); return; }
      try { st = await p.state('comments'); } catch (e) { st = 'unavailable'; }
    } else if (st === 'prompt') {
      try { const r = await p.request(['comments']); st = (r && r.comments) || 'unavailable'; } catch (e) { st = 'unavailable'; }
    }
    if (st === 'granted') { btn.hidden = true; say('Allowed. Send again.'); } else say(RELOAD, true);
  }
  async function post(anchorEl, text) {
    const c = S.comments;
    const anchor = await c.anchorFor(anchorEl);
    if (S.can === 'available') return c.sendToClaude({ anchor, text: fit(text) });
    return c.create({ anchor, text: fit(text) });
  }
  const FOOT = '\nNo reply is needed in this thread: the working session answers in its terminal and on the page.';

  /* ---------- editor ---------- */
  function setStatus(m, w) { status.textContent = m || ''; status.classList.toggle('btsr-warn', !!w); }
  function place(target, at) {
    const docW = document.documentElement.clientWidth, w = Math.min(460, docW - 32);
    let x, y;
    if (at && typeof at.pageX === 'number' && (at.pageX || at.pageY)) { x = at.pageX - 24; y = at.pageY + 16; }
    else { const r = target.getBoundingClientRect(); x = r.left + window.scrollX; y = r.bottom + window.scrollY + 10; }
    const left = Math.max(16, Math.min(x, docW - w - 16));
    note.style.left = left + 'px'; note.style.top = y + 'px';
    const px = at && at.pageX ? at.pageX : left + 30;
    note.style.setProperty('--btsr-tip', Math.max(14, Math.min(px - left - 6, w - 26)) + 'px');
  }
  async function openEditor(o) {
    if (S.dict && S.dict.active()) { try { await S.dict.stop(); } catch (e) {} }
    const n = o.id ? S.notes.get(o.id) : null;
    const label = n ? n.label : o.label;
    const target = o.el || elFor(label) || bar;
    S.ed = { id: o.id || null, label, kind: n ? n.kind : (o.kind || 'note'), value: n ? n.value : o.value, decision: n ? n.decision : o.decision, doc: o.doc, el: target };
    const readOnly = !!(n && n.status !== 'draft');
    $('.btsr-where b', note).textContent = label;
    ta.value = n ? n.text : (o.preset || '');
    ta.readOnly = readOnly;
    partial.textContent = ''; setStatus(''); allowBtn.hidden = true;
    reply.hidden = !readOnly; reply.replaceChildren();
    if (readOnly) {
      reply.append(el('span', 'btsr-st s-' + n.status, (STATUS[n.status] || n.status) + (n.round ? ' · round ' + n.round : '')),
        el('span', '', n.reply || (isWorking(n) ? 'Claude is working on this round.' : 'Sent. Waiting for the session to pick this round up.')));
      if (n.status === 'proposed' && !props.hidden) {
        const go = el('button', 'btsr-btn', 'See the proposal'); go.type = 'button';
        go.addEventListener('click', () => { closeEditor(); scrollTo(props); });
        reply.append(go);
      }
    }
    saveBtn.hidden = readOnly; delBtn.hidden = !(n && n.status === 'draft');
    mic.hidden = readOnly || !S.dict || S.dictOff; langSel.hidden = mic.hidden;
    saveBtn.textContent = S.ed.kind === 'confirm' ? 'Send confirmation' : 'Save note';
    place(target, o.at);
    note.hidden = false;
    if (!readOnly) { ta.focus(); ta.setSelectionRange(ta.value.length, ta.value.length); }
  }
  function closeEditor() {
    if (S.dict && S.dict.active()) S.dict.stop().catch(() => {});
    note.hidden = true; S.ed = null;
    if (S.reloadPending) location.reload();
  }
  // A local page reloads when the session rewrote page.html; never under an
  // open editor, where the text being typed would be lost.
  function reloadPage() { if (!note.hidden) { S.reloadPending = true; toast('The page has a new version. It reloads when you close this note.'); return; } location.reload(); }

  async function saveNote() {
    if (S.busy || !S.ed) return;
    const ed = S.ed;
    if (ed.kind === 'confirm') return sendConfirm();
    const text = ta.value.trim();
    if (!text) { setStatus('Say or type something first.', true); return; }
    const prev = ed.id ? S.notes.get(ed.id) : null, id = ed.id || newId();
    const data = { label: ed.label, anchor: ed.label, excerpt: excerptOf(ed.el), text: text.slice(0, 3800), kind: ed.kind, status: 'draft', round: null, page: PAGE, created: prev ? prev.created : iso(), updated: iso() };
    if (ed.kind === 'decision') { data.value = ed.value || null; data.decision = ed.decision || null; }
    S.busy = true; saveBtn.disabled = true; if (S.db) setStatus('Saving… The first time, claude.ai may ask to let this page keep data.');
    try {
      if (ed.kind === 'decision') { for (const [oid, o] of [...S.notes]) if (oid !== id && o.kind === 'decision' && o.decision === ed.decision && o.status === 'draft') await store.del(oid); }
      await store.set(id, data);
      closeEditor();
      toast('Saved. It waits in Review until you send your notes.');
    } catch (e) { setStatus('The note was not saved (' + ((e && e.code) || 'error') + '). Try again.', true); }
    finally { S.busy = false; saveBtn.disabled = false; }
  }
  async function sendConfirm() {
    const ed = S.ed;
    if (!S.comments) { setStatus('Comments are unavailable in this view, so the confirmation cannot be sent from here.', true); return; }
    const body = ta.value.trim();
    S.busy = true; saveBtn.disabled = true; setStatus('Sending…');
    const round = nextRound();
    try {
      await post(ed.el, 'Confirmation · round ' + round + ' · ' + SUBJECT + ' · ' + PAGE + '\n' + body + '\nFor the working session: check the fingerprint against the file and record the confirmation (' + CONFIRM_CMD + ').' + FOOT);
    } catch (e) { const r = explain(e && e.code); setStatus(r[0], true); allowBtn.hidden = !r[1]; S.busy = false; saveBtn.disabled = false; return; }
    const id = newId(), at = iso();
    try {
      await store.set(id, { label: ed.label, anchor: ed.label, excerpt: excerptOf(ed.el), text: body, kind: 'confirm', value: ed.value, doc: ed.doc, status: 'submitted', round, page: PAGE, created: at, updated: at, submitted: at });
      await store.round('r-' + round, { round, count: 1, ids: [id], submitted: at, status: 'submitted', page: PAGE, kind: 'confirm' });
    } catch (e) {}
    S.busy = false; saveBtn.disabled = false; closeEditor();
    toast('Confirmation sent. Claude checks the fingerprint, then records it.');
  }
  async function submitAll() {
    const list = drafts();
    if (!list.length) { sayTray('No notes to send yet.'); return; }
    if (!S.comments) { openTray(true); sayTray('Comments are unavailable in this view, so the review cannot be sent from here.', true); return; }
    if (S.busy) return;
    S.busy = true; render();
    const round = nextRound(), ids = list.map(([id]) => id);
    const head = 'Review round ' + round + ' submitted · ' + list.length + ' note' + (list.length > 1 ? 's' : '') + ' · ' + SUBJECT + ' · ' + PAGE;
    const body = S.local
      ? '\n' + list.map(([, n], i) => (i + 1) + '. [' + n.label + '] ' + n.text.replace(/\s+/g, ' ').slice(0, 1200)).join('\n')
      : '\nFor the working session: read the notes from this page\'s data (collection "notes", ids ' + ids.join(', ') + ').';
    sayTray(LOCAL || S.can === 'available' ? 'Sending…' : 'Posting…');
    try { await post(bar, head + body + FOOT); }
    catch (e) { const r = explain(e && e.code); openTray(true); sayTray(r[0], true); trayAllow.hidden = !r[1]; S.busy = false; render(); return; }
    const at = iso();
    try {
      for (const id of ids) await store.patch(id, { status: 'submitted', round, submitted: at });
      await store.round('r-' + round, { round, count: ids.length, ids, submitted: at, status: 'submitted', page: PAGE, kind: 'review' });
      sayTray('');
    } catch (e) { sayTray('Sent, but the notes could not be marked as sent here. Claude still has them.', true); }
    S.busy = false; render();
    const sent = 'Sent ' + ids.length + ' note' + (ids.length > 1 ? 's' : '') + ' as round ' + round + '.';
    if (LOCAL) toast(S.can === 'available' ? sent + ' The waiting session takes it now.' : sent + ' Claude takes it when a session checks the page.');
    else toast(S.can === 'available' ? 'Sent ' + ids.length + ' note' + (ids.length > 1 ? 's' : '') + ' to Claude as round ' + round + '.' : 'Posted round ' + round + ' as a comment. Send it to Claude from its thread once a session is watching.');
  }
  async function applyPlan() {
    const acc = [...S.proposals].filter(([, p]) => p.status === 'accepted').map(([id]) => id);
    const rej = [...S.proposals].filter(([, p]) => p.status === 'rejected').map(([id]) => id);
    if (!S.comments) { toast('Comments are unavailable in this view, so the update cannot be requested from here.'); return; }
    if (!acc.length || S.busy) return;
    S.busy = true; render();
    const round = nextRound();
    try {
      await post(props, 'Plan update requested · round ' + round + ' · ' + SUBJECT + ' · ' + acc.length + ' accepted, ' + rej.length + ' rejected · ' + PAGE +
        '\nFor the working session: apply the accepted proposals (' + acc.join(', ') + '), record the rejected ones (' + (rej.join(', ') || 'none') + '), publish the new version.' + FOOT);
      await store.round('r-' + round, { round, kind: 'apply', ids: acc, rejected: rej, count: acc.length, submitted: iso(), status: 'submitted', page: PAGE });
      toast('Plan update sent as round ' + round + '. Claude applies the accepted changes and publishes the new version.');
    } catch (e) { toast(explain(e && e.code)[0]); }
    finally { S.busy = false; render(); }
  }
  async function decide(id, st) {
    if (!S.db) { toast('Page data is unavailable here, so the decision cannot be saved.'); return; }
    try { await S.db.collection('proposals').doc(id).update({ status: st, decided: iso() }); }
    catch (e) { toast('The decision was not saved (' + ((e && e.code) || 'error') + '). Try again.'); }
  }

  /* ---------- render ---------- */
  function roundLabel(r) {
    if (!r) return 'sent';
    if (r.status === 'processed') return r.kind === 'apply' ? 'plan updated' : 'Claude answered';
    if (r.status === 'picked') return 'Claude is working on it' + (r.picked ? ', picked up ' + ago(r.picked) + ' ago' : '');
    return 'sent ' + (r.submitted ? ago(r.submitted) + ' ago' : '') + ', waiting for the session';
  }
  function renderProgress() {
    let last = null;
    for (const [, r] of S.rounds) if (!last || (r.round || 0) > (last.round || 0)) last = r;
    const chip = $('.btsr-chip', bar);
    const fresh = !!last && (last.status !== 'processed' || (!!last.processed && Date.now() - Date.parse(last.processed) < 90000));
    chip.hidden = !fresh;
    if (fresh) { chip.dataset.state = last.status; chip.textContent = 'Round ' + last.round + ': ' + roundLabel(last); }
    $$('.btsr-tray-h[data-round]', tray).forEach((h) => { h.textContent = 'Round ' + h.dataset.round + ' · ' + roundLabel(S.rounds.get('r-' + h.dataset.round)); });
  }
  function render() {
    const list = sorted(), d = drafts().length;
    $('.btsr-count', bar).textContent = d ? String(d) : '';
    $$('.btsr-send').forEach((b) => { b.hidden = !d; b.disabled = S.busy; b.textContent = (LOCAL || S.can === 'available' || !S.comments ? 'Send ' : 'Post ') + d + ' note' + (d > 1 ? 's' : '') + (S.can === 'available' ? ' to Claude' : ''); });
    $$('[data-bts-decision]').forEach((dec) => {
      const v = chosen(dec.dataset.btsDecision);
      $$('[data-bts-option]', dec).forEach((o) => o.classList.toggle('btsr-chosen', !!v && o.dataset.btsOption === v));
    });
    $$('.btsr-crow', confirmSec).forEach((row) => {
      const cs = list.filter(([, n]) => n.kind === 'confirm' && n.value === row.dataset.hash);
      const last = cs.length ? cs[cs.length - 1][1] : null;
      row.classList.toggle('btsr-sent', !!last);
      $('.btsr-cstate', row).textContent = !last ? '' : last.status === 'applied' ? 'Confirmed. ' + (last.reply || '') : last.status === 'declined' ? 'Not recorded. ' + (last.reply || '') : 'Sent. Waiting for Claude.';
    });
    renderTray(list); renderProgress(); renderProposals(); schedulePins();
  }
  function item(id, n, k) {
    const b = el('button', 'btsr-tn'); b.type = 'button';
    const top = el('div', 'btsr-tn-top');
    top.append(el('span', 'btsr-dot s-' + n.status, n.kind === 'confirm' ? '✓' : String(k)), el('span', 'btsr-tn-where', n.label),
      el('span', 'btsr-st s-' + n.status, isWorking(n) ? 'Working' : (STATUS[n.status] || n.status)));
    b.append(top, el('div', 'btsr-tn-txt', n.text));
    if (n.reply) b.append(el('div', 'btsr-tn-reply', n.reply));
    b.addEventListener('click', () => { const t = elFor(n.label); if (t) t.scrollIntoView({ block: 'center', behavior: reduced ? 'auto' : 'smooth' }); setTimeout(() => openEditor({ id }), reduced ? 0 : 380); });
    return b;
  }
  function renderTray(list) {
    trayList.replaceChildren();
    const num = new Map(list.map(([id], i) => [id, i + 1]));
    const dr = list.filter(([, n]) => n.status === 'draft');
    trayList.append(el('div', 'btsr-tray-h', 'Ready to send' + (dr.length ? ' · ' + dr.length : '')));
    if (!dr.length) trayList.append(el('p', 'btsr-muted', 'No draft notes. Press N, then click any spot on the page to add one.'));
    dr.forEach(([id, n]) => trayList.append(item(id, n, num.get(id))));
    const byRound = new Map();
    list.filter(([, n]) => n.status !== 'draft').forEach(([id, n]) => { const k = n.round || 0; if (!byRound.has(k)) byRound.set(k, []); byRound.get(k).push([id, n]); });
    [...byRound.keys()].sort((a, b) => b - a).forEach((k) => {
      const r = S.rounds.get('r-' + k), h = el('div', 'btsr-tray-h', 'Round ' + k); h.dataset.round = String(k);
      trayList.append(h);
      if (r && r.summary) trayList.append(el('p', 'btsr-sum', r.summary));
      byRound.get(k).forEach(([id, n]) => trayList.append(item(id, n, num.get(id))));
    });
  }
  function propCard(id, p, k) {
    const c = el('div', 'btsr-pcard s-' + p.status + '-b');
    const h = el('div', 'btsr-pcard-h');
    h.append(el('b', '', 'Proposal ' + k + ' · ' + (p.target || 'plan')), el('span', 'btsr-st', p.status === 'proposed' ? 'Waiting for you' : p.status === 'accepted' ? 'Accepted' : 'Rejected'));
    c.append(h, el('p', '', p.summary || ''));
    if (p.before || p.after) {
      const d = el('div', 'btsr-pdiff');
      if (p.before) d.append(el('div', 'btsr-del', p.before));
      if (p.after) d.append(el('div', 'btsr-ins', p.after));
      c.append(d);
    }
    if (p.why) c.append(el('p', 'btsr-muted', p.why));
    const row = el('div', 'btsr-row');
    const a = el('button', 'btsr-btn', 'Accept'); a.type = 'button'; a.setAttribute('aria-pressed', String(p.status === 'accepted'));
    const r = el('button', 'btsr-btn', 'Reject'); r.type = 'button'; r.setAttribute('aria-pressed', String(p.status === 'rejected'));
    a.addEventListener('click', () => decide(id, p.status === 'accepted' ? 'proposed' : 'accepted'));
    r.addEventListener('click', () => decide(id, p.status === 'rejected' ? 'proposed' : 'rejected'));
    row.append(a, r); c.append(row);
    return c;
  }
  function renderProposals() {
    $$('.btsr-ghost').forEach((e) => e.remove());
    $$('.btsr-pbadge').forEach((e) => e.remove());
    $$('.btsr-moving').forEach((e) => e.classList.remove('btsr-moving'));
    const pend = [...S.proposals].filter(([, p]) => p.status === 'proposed' || p.status === 'accepted' || p.status === 'rejected')
      .sort((a, b) => String(a[1].created || '').localeCompare(String(b[1].created || '')));
    props.hidden = !pend.length;
    const box = $('.btsr-plist', props); box.replaceChildren();
    pend.forEach(([id, p], i) => {
      const k = i + 1, src = p.anchor ? elFor(p.anchor) : null;
      if (src) {
        if (p.kind === 'move' && p.status !== 'rejected') src.classList.add('btsr-moving');
        src.append(el('span', 'btsr-pbadge s-' + p.status, 'Proposal ' + k + (p.kind === 'move' && p.to_label ? ' → ' + p.to_label : '')));
      }
      if (p.kind === 'move' && p.to_lane && p.status !== 'rejected') {
        const lane = elFor(p.to_lane);
        if (lane) { const g = el('div', 'btsr-ghost', p.after || p.summary || ''); g.append(el('small', '', 'Proposal ' + k + ' · ' + (p.status === 'accepted' ? 'accepted, not applied yet' : 'proposed'))); lane.append(g); }
      }
      box.append(propCard(id, p, k));
    });
    const acc = pend.filter(([, p]) => p.status === 'accepted').length, open = pend.filter(([, p]) => p.status === 'proposed').length;
    const applying = [...S.rounds.values()].some((r) => r.kind === 'apply' && r.status !== 'processed');
    const ab = $('[data-k=apply]', props), pst = $('[data-k=pstatus]', props), top = $('[data-k=decide]', bar);
    ab.disabled = !acc || open > 0 || applying || S.busy;
    ab.textContent = 'Update plan with ' + acc + ' accepted change' + (acc === 1 ? '' : 's');
    pst.textContent = applying ? 'Claude is updating the plan.' : open ? 'Accept or reject every proposal first (' + open + ' left).' : acc ? 'The plan files change only when you press this.' : 'Nothing accepted. The rejections are recorded and the plan stays as it is.';
    top.hidden = !pend.length || applying || (!acc && !open);
    top.textContent = open ? 'Decide ' + open + ' proposal' + (open > 1 ? 's' : '') : 'Update plan (' + acc + ')';
  }
  let pinFrame = 0;
  function schedulePins() { cancelAnimationFrame(pinFrame); pinFrame = requestAnimationFrame(renderPins); }
  function renderPins() {
    pinLayer.replaceChildren();
    const list = sorted(), num = new Map(list.map(([id], i) => [id, i + 1])), docW = document.documentElement.clientWidth, byLabel = new Map();
    list.forEach(([id, n]) => { if (n.kind === 'confirm') return; const a = n.anchor || n.label; if (!byLabel.has(a)) byLabel.set(a, []); byLabel.get(a).push([id, n]); });
    byLabel.forEach((arr, label) => {
      const t = elFor(label); if (!t) return;
      const r = t.getBoundingClientRect(); if (!r.width && !r.height) return;
      arr.forEach(([id, n], k) => {
        const b = el('button', 'btsr-pin s-' + n.status + (isWorking(n) ? ' btsr-pulse' : ''), String(num.get(id))); b.type = 'button';
        b.setAttribute('aria-label', 'Note ' + num.get(id) + ', ' + (STATUS[n.status] || n.status) + ', on ' + n.label);
        b.title = (STATUS[n.status] || n.status) + ': ' + String(n.text || '').slice(0, 80);
        b.style.left = Math.max(4, Math.min(docW - 34, r.right + window.scrollX - 16 - k * 28)) + 'px';
        b.style.top = Math.max(0, r.top + window.scrollY - 13) + 'px';
        b.addEventListener('click', (ev) => { ev.stopPropagation(); openEditor({ id, at: ev }); });
        pinLayer.append(b);
      });
    });
  }
  window.addEventListener('resize', schedulePins);
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(schedulePins);
  try { new ResizeObserver(schedulePins).observe(document.body); } catch (e) {}
  setInterval(() => { if (!document.hidden) renderProgress(); }, 5000);

  /* ---------- dictation ---------- */
  function dictReason(r) {
    if (!r || r === 'stopped' || r === 'viewer_stopped') return;
    const M = {
      not_granted: 'Dictation is off for this page. Type instead, or allow it from the artifact\'s Permissions menu.',
      unavailable: S.lang ? 'Dictation in this language is not served here. Pick Auto and press Speak again.'
        : LOCAL ? 'Speech recognition is not available in this browser. Type, or use macOS dictation (press Fn twice).' : 'Dictation is not available in this view. Type instead.',
      no_gesture: 'Press Speak again to start.', hidden: 'Dictation stopped when the page was hidden. Press Speak to continue.',
      rate_limited: 'Too many dictations at once. Try again in a moment.', upstream_error: 'The microphone or speech service failed. Try again, or type.',
      invalid_argument: 'That language setting was refused. Pick Auto.'
    };
    setStatus(M[r] || M.upstream_error, true);
    if (r === 'not_granted' || (r === 'unavailable' && !S.lang)) { S.dictOff = true; mic.hidden = true; langSel.hidden = true; }
  }
  function setupDict(d) {
    S.dict = d;
    d.onPartial(({ text }) => { partial.textContent = text; });
    d.onFinal(({ text }) => { let t = text; if (!ta.value) t = t.replace(/^\s+/, ''); ta.value += t; partial.textContent = ''; });
    d.onState(({ state, reason }) => {
      mic.dataset.state = state;
      mic.setAttribute('aria-pressed', String(state === 'listening' || state === 'starting'));
      mic.textContent = state === 'listening' ? 'Listening · press to stop' : state === 'starting' ? 'Starting…' : state === 'finishing' ? 'Finishing…' : 'Speak';
      if (state === 'idle') { partial.textContent = ''; dictReason(reason); }
    });
  }

  /* ---------- events ---------- */
  function openTray(on) { tray.hidden = !on; $('[data-k=tray]', bar).setAttribute('aria-pressed', String(on)); }
  function setNoting(on) {
    S.noting = on; document.documentElement.classList.toggle('btsr-noting', on); $('[data-k=note]', bar).setAttribute('aria-pressed', String(on));
    if (!on && S.hot) { S.hot.classList.remove('btsr-hot'); S.hot = null; }
    if (on) toast('Note mode: click the spot you want to pin a note on. Press N or Esc to leave.');
  }
  bar.addEventListener('click', (e) => {
    const k = e.target.closest('[data-k]') && e.target.closest('[data-k]').dataset.k;
    if (k === 'note') setNoting(!S.noting);
    else if (k === 'tray') openTray(tray.hidden);
    else if (k === 'decide') scrollTo(props);
    else if (e.target.closest('.btsr-send')) submitAll();
  });
  $('[data-k=tclose]', tray).addEventListener('click', () => openTray(false));
  $('.btsr-send', tray).addEventListener('click', submitAll);
  trayAllow.addEventListener('click', () => allowComments(sayTray, trayAllow));
  saveBtn.addEventListener('click', saveNote);
  $('[data-k=close]', note).addEventListener('click', closeEditor);
  delBtn.addEventListener('click', async () => { if (!S.ed || !S.ed.id) return; try { await store.del(S.ed.id); closeEditor(); toast('Note deleted.'); } catch (e) { setStatus('The note was not deleted. Try again.', true); } });
  allowBtn.addEventListener('click', () => allowComments(setStatus, allowBtn));
  langSel.addEventListener('change', () => { S.lang = langSel.value; });
  mic.addEventListener('click', () => {
    const d = S.dict; if (!d) return;
    if (d.active()) { d.stop().catch(() => {}); return; }
    setStatus('');
    d.start(S.lang ? { language: S.lang } : undefined).catch((e) => dictReason(e && e.code));
  });
  $('[data-k=apply]', props).addEventListener('click', applyPlan);
  document.addEventListener('click', (e) => {
    const pick = e.target.closest && e.target.closest('[data-k=pick]');
    if (pick) {
      const opt = pick.closest('[data-bts-option]'), key = opt.closest('[data-bts-decision]').dataset.btsDecision, v = opt.dataset.btsOption;
      const existing = sorted().filter(([, n]) => n.kind === 'decision' && n.decision === key && n.status === 'draft' && n.value === v).pop();
      if (existing) openEditor({ id: existing[0], at: e });
      else openEditor({ el: opt, label: opt.dataset.note, kind: 'decision', decision: key, value: v, preset: 'Decision ' + key + ': ' + v + '\n', at: e });
      return;
    }
    const conf = e.target.closest && e.target.closest('[data-k=confirm]');
    if (conf) {
      const row = conf.closest('.btsr-crow'), d = drafts().length, p = pendingRound();
      const open = [...S.proposals.values()].filter((x) => x.status === 'proposed' || x.status === 'accepted').length;
      if (d) { toast('Send or delete your ' + d + ' draft note' + (d > 1 ? 's' : '') + ' first.'); openTray(true); return; }
      if (p) { toast('Claude is still working on round ' + p + '. Confirm once it answers.'); openTray(true); return; }
      if (open) { toast('Decide and apply the ' + open + ' open proposal' + (open > 1 ? 's' : '') + ' first.'); scrollTo(props); return; }
      let preset = 'CONFIRM ' + row.dataset.doc + ' ' + row.dataset.ver + ' (' + row.dataset.hash + ')';
      $$('[data-bts-decision]').forEach((dec) => { preset += '\nDecision ' + dec.dataset.btsDecision + ': ' + (chosen(dec.dataset.btsDecision) || 'not picked'); });
      openEditor({ el: row, label: row.dataset.note, kind: 'confirm', value: row.dataset.hash, doc: row.dataset.doc, preset: preset + '\n', at: e });
    }
  });
  document.addEventListener('mouseover', (e) => {
    if (!S.noting) return;
    const t = e.target.closest ? e.target.closest('[data-note]') : null;
    if (t === S.hot) return;
    if (S.hot) S.hot.classList.remove('btsr-hot');
    S.hot = t; if (S.hot) S.hot.classList.add('btsr-hot');
  });
  document.addEventListener('click', (e) => {
    if (!S.noting) return;
    const t = e.target;
    if (!t.closest || t.closest('.btsr-note, .btsr-tray, .btsr-bar, .btsr-pins, button, a, select, input, textarea')) return;
    const target = t.closest('[data-note]'); if (!target) return;
    e.preventDefault(); e.stopPropagation();
    openEditor({ el: target, label: target.dataset.note, kind: 'note', at: e });
  }, true);
  document.addEventListener('keydown', (e) => {
    const tag = e.target && e.target.tagName, typing = tag === 'TEXTAREA' || tag === 'INPUT' || tag === 'SELECT';
    if (e.altKey && e.code === 'KeyM' && !note.hidden) { e.preventDefault(); mic.click(); return; }
    if (e.key === 'Escape') { if (!note.hidden) { closeEditor(); return; } if (S.noting) { setNoting(false); return; } if (!tray.hidden) { openTray(false); return; } }
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && !note.hidden && !ta.readOnly) { e.preventDefault(); saveNote(); return; }
    if (!typing && !e.metaKey && !e.ctrlKey && !e.altKey && (e.key === 'n' || e.key === 'N')) { e.preventDefault(); setNoting(!S.noting); }
  });

  /* ---------- local transport (bts review serve) ---------- */
  // The same shapes the claude.ai capabilities give, so everything above
  // runs unchanged: a db whose collections take set/update/delete and
  // onSnapshot, and a comments object whose "send" is a no-op because
  // writing the round is what the session waits for.
  function localTransport() {
    const subs = { notes: [], rounds: [], proposals: [] };
    let last = null;
    async function call(method, path, body) {
      const r = await fetch(API + path, { method, credentials: 'same-origin', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined });
      if (!r.ok) { const e = new Error('HTTP ' + r.status); e.code = r.status === 403 ? 'forbidden' : 'upstream_error'; throw e; }
      return r.json();
    }
    const snap = (docs) => ({ docs: Object.keys(docs || {}).map((id) => ({ id, exists: true, data: () => docs[id] })) });
    function deliver(st) {
      last = st; S.waiting = !!st.waiting;
      Object.keys(subs).forEach((c) => subs[c].forEach(([ok]) => { try { ok(snap(st[c])); } catch (e) {} }));
      refreshCan();
    }
    async function refresh() { try { deliver(await call('GET', 'state')); } catch (e) { setConn('off'); } }
    const write = (p) => p.then((v) => { refresh(); return v; });
    const db = {
      collection(name) {
        return {
          doc(id) {
            const p = 'doc/' + name + '/' + encodeURIComponent(id);
            return { set: (d) => write(call('PUT', p, d)), update: (d) => write(call('PATCH', p, d)), delete: () => write(call('DELETE', p)) };
          },
          onSnapshot(ok, bad) { (subs[name] || []).push([ok, bad]); if (last) ok(snap(last[name])); return () => {}; }
        };
      }
    };
    const comments = {
      canSendToClaude: async () => (last ? (S.waiting ? 'available' : 'no_session') : 'unknown'),
      anchorFor: async (target) => (target && target.dataset && target.dataset.note) || '',
      sendToClaude: async () => ({ ok: true }),
      create: async () => ({ ok: true })
    };
    let es = null;
    try {
      es = new EventSource(API + 'events');
      es.addEventListener('state', (ev) => { try { deliver(JSON.parse(ev.data)); } catch (e) {} });
      es.addEventListener('reload', () => reloadPage());
      es.onerror = () => { if (es.readyState === EventSource.CLOSED) { setConn('off'); setTimeout(refresh, 2000); } };
    } catch (e) { setInterval(refresh, 2000); }
    refresh();
    return { db, comments };
  }
  function localDictation() {
    const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!SR) return null;
    let rec = null, on = false;
    const h = { partial: [], final: [], state: [] };
    const emit = (k, v) => h[k].forEach((f) => { try { f(v); } catch (e) {} });
    const reasonOf = (err) => (err === 'not-allowed' || err === 'service-not-allowed') ? 'not_granted' : err === 'no-speech' || err === 'aborted' ? 'stopped' : err === 'network' || err === 'language-not-supported' ? 'unavailable' : 'upstream_error';
    return {
      onPartial: (f) => h.partial.push(f), onFinal: (f) => h.final.push(f), onState: (f) => h.state.push(f),
      active: () => on,
      start: (o) => new Promise((resolve, reject) => {
        try {
          rec = new SR(); rec.continuous = true; rec.interimResults = true;
          rec.lang = (o && o.language) || document.documentElement.lang || navigator.language || '';
          rec.onstart = () => { on = true; emit('state', { state: 'listening' }); resolve(); };
          rec.onresult = (ev) => {
            let interim = '';
            for (let i = ev.resultIndex; i < ev.results.length; i++) { const r = ev.results[i]; if (r.isFinal) emit('final', { text: r[0].transcript }); else interim += r[0].transcript; }
            emit('partial', { text: interim });
          };
          rec.onerror = (ev) => { const reason = reasonOf(ev.error); const was = on; on = false; emit('state', { state: 'idle', reason }); if (!was) reject({ code: reason }); };
          rec.onend = () => { if (on) { on = false; emit('state', { state: 'idle', reason: 'stopped' }); } };
          emit('state', { state: 'starting' }); rec.start();
        } catch (e) { emit('state', { state: 'idle', reason: 'unavailable' }); reject({ code: 'unavailable' }); }
      }),
      stop: () => new Promise((resolve) => {
        if (!rec || !on) { resolve(); return; }
        emit('state', { state: 'finishing' });
        rec.onend = () => { on = false; emit('state', { state: 'idle', reason: 'stopped' }); resolve(); };
        try { rec.stop(); } catch (e) { on = false; resolve(); }
      })
    };
  }

  /* ---------- capabilities ---------- */
  render();
  const C = window.claude;
  if (LOCAL) {
    const t = localTransport();
    S.comments = t.comments;
    attachDb(t.db);
    const d = localDictation();
    if (d) setupDict(d);
    else ta.placeholder = 'What should change here? Type, or dictate with macOS (press Fn twice).';
    setInterval(() => { if (!note.hidden || document.hidden) return; refreshCan(); }, 10000);
  } else if (C && typeof C.use === 'function') {
    C.use('comments').then((c) => { S.comments = c; refreshCan(); }).catch(() => setConn('none'));
    C.use('dictation').then((d) => { if (d) setupDict(d); }).catch(() => {});
    C.use('permissions').then((p) => { S.perms = p; }).catch(() => {});
    C.use('db').then((db) => { if (db) attachDb(db); else goLocal(); }).catch(goLocal);
    setInterval(() => { if (!note.hidden || document.hidden) return; refreshCan(); }, 30000);
  } else { setConn('none'); goLocal(); }
})();
