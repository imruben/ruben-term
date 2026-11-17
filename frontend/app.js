// ruben-term · the window: tabs, panes, the side panels and the command palette.
// The Go side (window.go.main.App) owns the shells; this side draws them with xterm.js.
//
// A tab holds one or more panes. The layout of a tab is its DOM: a .leaf is a pane,
// a .split[data-dir=row|col] holds leaves and splits with a .gutter between each two,
// and flex-grow is the share of the space each one takes.
'use strict';

const api = window.go.main.App;
const rt = window.runtime;
const $ = (id) => document.getElementById(id);

const THEMES = {
  dark: {
    background: '#0f1012', foreground: '#d4d7dd', cursor: '#e6b450', cursorAccent: '#0f1012',
    selectionBackground: 'rgba(230, 180, 80, 0.28)', selectionInactiveBackground: 'rgba(230, 180, 80, 0.16)',
    black: '#23262b', red: '#ef6b73', green: '#9ccf7a', yellow: '#e6b450',
    blue: '#6cb0f5', magenta: '#c792ea', cyan: '#5ccfe6', white: '#c5c9d1',
    brightBlack: '#666d79', brightRed: '#ff8a90', brightGreen: '#b8e394', brightYellow: '#ffcc66',
    brightBlue: '#8cc4ff', brightMagenta: '#d9a9ff', brightCyan: '#7fe3f5', brightWhite: '#f0f2f5',
  },
  light: {
    background: '#fbfaf8', foreground: '#2b2d31', cursor: '#b8730a', cursorAccent: '#fbfaf8',
    selectionBackground: 'rgba(184, 115, 10, 0.24)', selectionInactiveBackground: 'rgba(184, 115, 10, 0.14)',
    black: '#2b2d31', red: '#c4342d', green: '#3d7a1f', yellow: '#9a6a00',
    blue: '#2161c7', magenta: '#8a3fc0', cyan: '#0f7c8c', white: '#8a8f98',
    brightBlack: '#6b7079', brightRed: '#d9453d', brightGreen: '#4c8f29', brightYellow: '#b07d00',
    brightBlue: '#2f74e0', brightMagenta: '#9d52d6', brightCyan: '#1590a3', brightWhite: '#c5c9d1',
  },
};

let boot = null;          // what Bootstrap() said: platform, config, profiles
let cfg = null;
let isMac = false;
let fontSize = 13;
const tabs = [];          // { key, el, view, panes: [], focused, zoomed }
let activeTab = null;
const bySession = new Map();   // session id → pane
const early = new Map();       // output that arrived before its pane knew its session id
const byLeaf = new WeakMap();  // .leaf element → pane
let seq = 0;

// the pane you are typing in
const cur = () => (activeTab ? activeTab.focused : null);
const allPanes = () => tabs.flatMap((t) => t.panes);

// what the side panels and the palette know, per profile
const cache = new Map();  // profileId → { ports, ssh, projects }
const known = (pid) => {
  if (!cache.has(pid)) cache.set(pid, { ports: null, ssh: null, projects: null });
  return cache.get(pid);
};

// ───────────────────────── helpers ─────────────────────────

function el(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (k.startsWith('on')) node.addEventListener(k.slice(2), v);
    else if (v !== false && v != null) node.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children) if (c) node.append(c);
  return node;
}

function icon(path, size) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${size || 16} ${size || 16}`);
  svg.setAttribute('aria-hidden', 'true');
  const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  p.setAttribute('d', path);
  svg.append(p);
  return svg;
}

let toastTimer = 0;
function toast(msg, info) {
  const t = $('toast');
  t.textContent = String(msg && msg.message ? msg.message : msg);
  t.classList.toggle('info', !!info);
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, info ? 2500 : 6000);
}

function decode(b64) {
  if (Uint8Array.fromBase64) return Uint8Array.fromBase64(b64);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

const profile = (id) => boot.profiles.find((p) => p.id === id);
const shortPath = (p) => (p || '').replace(/^\/(home|Users)\/[^/]+/, '~');
const shq = (s) => "'" + String(s).replace(/'/g, "'\\''") + "'";   // quoted for a posix shell

function stored(key, fallback) {
  try { return JSON.parse(localStorage.getItem(key)) ?? fallback; } catch (_) { return fallback; }
}
function store(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch (_) { /* not kept, that is all */ }
}

// ───────────────────────── theme and font ─────────────────────────

const systemDark = window.matchMedia('(prefers-color-scheme: dark)');
function themeName() {
  if (cfg.theme === 'light') return 'light';
  if (cfg.theme === 'system') return systemDark.matches ? 'dark' : 'light';
  return 'dark';
}
function termTheme() {
  return Object.assign({}, THEMES[themeName()], cfg.colors || {});
}
function applyLook() {
  document.documentElement.dataset.theme = themeName();
  const theme = termTheme();
  document.documentElement.style.setProperty('--bg', theme.background);
  for (const p of allPanes()) {
    p.term.options.theme = theme;
    p.term.options.fontFamily = cfg.fontFamily;
    p.term.options.fontSize = fontSize;
    p.term.options.lineHeight = cfg.lineHeight;
    p.term.options.scrollback = cfg.scrollback;
    p.term.options.cursorBlink = cfg.cursorBlink;
    fitPane(p);
  }
}
systemDark.addEventListener('change', () => { if (cfg && cfg.theme === 'system') applyLook(); });

function zoomText(delta) {
  fontSize = delta === 0 ? cfg.fontSize : Math.min(40, Math.max(6, fontSize + delta));
  applyLook();
}

// ───────────────────────── panes ─────────────────────────

function fitPane(p) {
  if (!p.el.isConnected || p.el.clientWidth < 20 || p.el.clientHeight < 20) return;
  try { p.fit.fit(); } catch (_) { /* not laid out yet */ }
}

// every pane refits when its box changes: the window, a split, a gutter, the side panel
const resizes = new ResizeObserver((entries) => {
  requestAnimationFrame(() => {
    for (const e of entries) {
      const p = byLeaf.get(e.target);
      if (p && !p.gone) fitPane(p);
    }
  });
});

// Creates a pane of a tab: its box and its terminal. The caller puts p.el in the
// layout and then calls startPane.
function makePane(tab, opts) {
  const prof = profile(opts.profileId);
  const term = new Terminal({
    allowProposedApi: true,
    fontFamily: cfg.fontFamily,
    fontSize,
    lineHeight: cfg.lineHeight,
    scrollback: cfg.scrollback,
    cursorBlink: cfg.cursorBlink,
    cursorStyle: 'bar',
    cursorInactiveStyle: 'outline',
    theme: termTheme(),
    macOptionIsMeta: true,
    rightClickSelectsWord: false,
    drawBoldTextInBrightColors: false,
    minimumContrastRatio: 1,
    windowsPty: boot.platform === 'windows' ? { backend: 'conpty', buildNumber: boot.windowsBuild } : undefined,
  });
  const p = {
    key: ++seq, id: null, tab, profileId: opts.profileId, el: el('div', { class: 'leaf' }),
    term, fit: new FitAddon.FitAddon(), search: new SearchAddon.SearchAddon(),
    label: opts.label || prof.name, title: '', cwd: opts.cwd || '',
    unacked: 0, born: Date.now(), dead: false, gone: false,
  };
  p.el.style.flexGrow = '1';
  byLeaf.set(p.el, p);
  tab.panes.push(p);
  term.loadAddon(p.fit);
  term.loadAddon(p.search);
  term.loadAddon(new WebLinksAddon.WebLinksAddon((ev, uri) => { api.OpenURL(uri).catch(toast); }));
  try {
    term.loadAddon(new Unicode11Addon.Unicode11Addon());
    term.unicode.activeVersion = '11';
  } catch (_) { /* widths fall back to Unicode 6 */ }
  return p;
}

// Opens the terminal of a pane that is already in the layout and starts its shell.
async function startPane(p, opts) {
  const term = p.term;
  term.open(p.el);
  try {
    const gl = new WebglAddon.WebglAddon();
    gl.onContextLoss(() => gl.dispose());   // falls back to the DOM renderer
    term.loadAddon(gl);
  } catch (_) { /* no WebGL: the DOM renderer draws */ }
  resizes.observe(p.el);

  term.textarea.addEventListener('focus', () => setFocused(p));
  term.attachCustomKeyEventHandler((ev) => onTermKey(p, ev));
  term.onData((data) => {
    if (p.dead) { closePane(p); return; }
    if (p.id) api.Write(p.id, data);
  });
  term.onBinary((data) => { if (p.id) api.WriteBinary(p.id, btoa(data)); });
  term.onResize(({ cols, rows }) => { if (p.id) api.Resize(p.id, cols, rows); });
  term.onTitleChange((title) => {
    // Windows shells call themselves by their full path: keep the program's name
    const exe = /^.*[\\/]([^\\/]+)\.exe$/i.exec(title);
    p.title = exe ? exe[1] : title;
    renderTab(p.tab);
  });
  term.onBell(() => { if (p.tab !== activeTab) p.tab.el.classList.add('bell'); });
  term.onSelectionChange(() => {
    if (cfg.copyOnSelect && term.hasSelection()) rt.ClipboardSetText(term.getSelection());
  });
  // OSC 7: the shell says which folder it is in → new tabs, splits and launchers open there
  term.parser.registerOscHandler(7, (data) => {
    const m = /^file:\/\/[^/]*(\/.*)$/.exec(data);
    if (m) {
      try { p.cwd = decodeURIComponent(m[1]); } catch (_) { p.cwd = m[1]; }
      renderTab(p.tab);
    }
    return true;
  });
  p.el.addEventListener('contextmenu', (ev) => {   // right click: copy the selection, or paste
    ev.preventDefault();
    if (term.hasSelection()) copySelection(p); else paste(p);
  });

  fitPane(p);
  term.write('\x1b[2mstarting ' + profile(p.profileId).name + '…\x1b[0m');
  try {
    const id = await api.NewSession(p.profileId, opts.cwd || '', opts.run || '', term.cols, term.rows);
    if (p.gone) { api.CloseSession(id); early.delete(id); return; }   // closed while it was starting
    p.id = id;
    bySession.set(id, p);
    term.write('\x1b[2K\r');
    for (const chunk of early.get(id) || []) feed(p, chunk);
    early.delete(id);
    api.Resize(id, term.cols, term.rows);
  } catch (err) {
    p.dead = true;
    term.write('\x1b[2K\r\x1b[31m' + String(err && err.message ? err.message : err).replace(/\n/g, '\r\n') +
      '\x1b[0m\r\n\x1b[2mpress a key to close\x1b[0m');
  }
}

function feed(p, bytes) {
  p.term.write(bytes, () => {
    p.unacked += bytes.length;
    if (p.unacked >= 65536 && p.id) {     // tell the Go side what has been drawn (flow control)
      api.Ack(p.id, p.unacked);
      p.unacked = 0;
    }
  });
}

// The pane that has the keyboard: bookkeeping only (the terminal already has the focus).
function setFocused(p) {
  const tab = p.tab;
  if (tab.focused === p && p.el.classList.contains('focused')) return;
  const before = cur() && cur().profileId;
  for (const q of tab.panes) q.el.classList.toggle('focused', q === p);
  tab.focused = p;
  renderTab(tab);
  if (tab === activeTab && before !== p.profileId) refreshPanel();
}
function focusPane(p) {
  setFocused(p);
  p.term.focus();
}

// Splits the pane you are in. dir "row": side by side · "col": one above the other.
// The new pane opens in the same folder, with the same shell.
function splitPane(dir) {
  const p = cur();
  if (!p) return;
  const tab = p.tab;
  unzoom(tab);
  const np = makePane(tab, { profileId: p.profileId, cwd: p.cwd });
  const parent = p.el.parentElement;
  if (parent.classList.contains('split') && parent.dataset.dir === dir) {
    const half = String((parseFloat(p.el.style.flexGrow) || 1) / 2);   // share the space of this one
    p.el.style.flexGrow = half;
    np.el.style.flexGrow = half;
    p.el.after(gutter(), np.el);
  } else {
    const split = el('div', { class: 'split', 'data-dir': dir });
    split.style.flexGrow = p.el.style.flexGrow || '1';
    p.el.replaceWith(split);
    p.el.style.flexGrow = '1';
    split.append(p.el, gutter(), np.el);
  }
  tab.view.classList.toggle('multi', tab.panes.length > 1);
  startPane(np, { cwd: p.cwd });
  focusPane(np);
}

// The line between two panes: drag it to resize them, double click to make them equal.
function gutter() {
  const g = el('div', { class: 'gutter' });
  g.addEventListener('pointerdown', (ev) => {
    const a = g.previousElementSibling;
    const b = g.nextElementSibling;
    const row = g.parentElement.dataset.dir === 'row';
    const sa = row ? a.offsetWidth : a.offsetHeight;
    const total = sa + (row ? b.offsetWidth : b.offsetHeight);
    const grow = (parseFloat(a.style.flexGrow) || 1) + (parseFloat(b.style.flexGrow) || 1);
    const from = row ? ev.clientX : ev.clientY;
    const min = Math.min(80, total / 2);
    g.setPointerCapture(ev.pointerId);
    g.classList.add('drag');
    const move = (e) => {
      const na = Math.max(min, Math.min(total - min, sa + (row ? e.clientX : e.clientY) - from));
      a.style.flexGrow = String(grow * na / total);
      b.style.flexGrow = String(grow * (total - na) / total);
    };
    const up = () => {
      g.classList.remove('drag');
      g.removeEventListener('pointermove', move);
      g.removeEventListener('pointerup', up);
      g.removeEventListener('pointercancel', up);
      if (cur()) cur().term.focus();
    };
    g.addEventListener('pointermove', move);
    g.addEventListener('pointerup', up);
    g.addEventListener('pointercancel', up);
    ev.preventDefault();
  });
  g.addEventListener('dblclick', () => {
    for (const c of g.parentElement.children) if (!c.classList.contains('gutter')) c.style.flexGrow = '1';
  });
  return g;
}

function closePane(p) {
  if (p.gone) return;
  const tab = p.tab;
  p.gone = true;
  if (p.id) { bySession.delete(p.id); api.CloseSession(p.id); p.id = null; }
  resizes.unobserve(p.el);
  tab.panes.splice(tab.panes.indexOf(p), 1);
  if (!tab.panes.length) {       // the last pane takes the tab with it
    p.term.dispose();
    closeTab(tab);
    return;
  }
  if (tab.zoomed) unzoom(tab);
  // the pane to go to: the one that was next to this one
  const near = nearest(p, tab.panes);
  const parent = p.el.parentElement;
  const prev = p.el.previousElementSibling;
  const g = prev && prev.classList.contains('gutter') ? prev : p.el.nextElementSibling;
  if (g && g.classList.contains('gutter')) g.remove();
  p.term.dispose();
  p.el.remove();
  if (parent.classList.contains('split')) {     // a split with one thing left is that thing
    const kids = [...parent.children].filter((c) => !c.classList.contains('gutter'));
    if (kids.length === 1) {
      kids[0].style.flexGrow = parent.style.flexGrow || '1';
      parent.replaceWith(kids[0]);
    }
  }
  tab.view.classList.toggle('multi', tab.panes.length > 1);
  if (tab.focused === p) {
    tab.focused = near;
    if (tab === activeTab) focusPane(near); else setFocused(near);
  }
  renderTab(tab);
}

const centre = (r) => [(r.left + r.right) / 2, (r.top + r.bottom) / 2];
function nearest(p, panes) {
  const [x, y] = centre(p.el.getBoundingClientRect());
  let best = panes[0];
  let bestD = Infinity;
  for (const q of panes) {
    const [qx, qy] = centre(q.el.getBoundingClientRect());
    const d = Math.hypot(qx - x, qy - y);
    if (d < bestD) { best = q; bestD = d; }
  }
  return best;
}

// Moves to the pane on that side: the closest one, preferring the one facing this one.
function focusSide(dx, dy) {
  const p = cur();
  if (!p || p.tab.panes.length < 2) return;
  unzoom(p.tab);
  const r = p.el.getBoundingClientRect();
  let best = null;
  let bestScore = Infinity;
  for (const q of p.tab.panes) {
    if (q === p) continue;
    const s = q.el.getBoundingClientRect();
    let gap;
    let overlap;
    if (dx) {
      gap = dx > 0 ? s.left - r.right : r.left - s.right;
      overlap = Math.min(r.bottom, s.bottom) - Math.max(r.top, s.top);
    } else {
      gap = dy > 0 ? s.top - r.bottom : r.top - s.bottom;
      overlap = Math.min(r.right, s.right) - Math.max(r.left, s.left);
    }
    if (gap < -2) continue;                       // not on that side
    const score = gap + (overlap > 0 ? 0 : 100000) - overlap / 1000;
    if (score < bestScore) { best = q; bestScore = score; }
  }
  if (best) focusPane(best);
}
function focusNext(delta) {
  const p = cur();
  if (!p || p.tab.panes.length < 2) return;
  unzoom(p.tab);
  // in the order they are on screen
  const order = [...p.tab.view.querySelectorAll('.leaf')].map((e) => byLeaf.get(e));
  focusPane(order[(order.indexOf(p) + delta + order.length) % order.length]);
}

// One pane over the whole tab, and back.
function toggleZoom() {
  const p = cur();
  if (!p) return;
  const tab = p.tab;
  if (tab.zoomed) { unzoom(tab); return; }
  if (tab.panes.length < 2) return;
  tab.zoomed = p;
  tab.view.classList.add('zoomed');
  p.el.classList.add('zoom');
  renderTab(tab);
}
function unzoom(tab) {
  if (!tab.zoomed) return;
  tab.zoomed.el.classList.remove('zoom');
  tab.view.classList.remove('zoomed');
  tab.zoomed = null;
  renderTab(tab);
}

// ───────────────────────── tabs ─────────────────────────

function renderTab(tab) {
  const p = tab.focused;
  if (!p) return;
  const title = p.title || p.label;
  tab.el.querySelector('.title').textContent = title;
  const n = tab.panes.length;
  tab.el.querySelector('.panes').textContent = n > 1 ? (tab.zoomed ? `1/${n}` : String(n)) : '';
  tab.el.title = (p.cwd ? `${title}\n${shortPath(p.cwd)}` : title) + (n > 1 ? `\n${n} panes` : '');
}

// Opens a tab. opts: profileId, cwd (a folder of that profile's machine), run (a
// command to start in it), label (the tab's name until the shell sets a title).
function openTab(opts) {
  opts = opts || {};
  const here = cur();
  const profileId = opts.profileId || (here && here.profileId) || boot.defaultProfile;
  if (!profile(profileId)) { toast('No shell to open'); return null; }

  const tab = {
    key: ++seq, panes: [], focused: null, zoomed: null,
    view: el('div', { class: 'tabview' }),
    el: el('div', { class: 'tab', role: 'tab', tabindex: '0' },
      el('span', { class: 'dot' }),
      el('span', { class: 'title' }),
      el('span', { class: 'panes' }),
      el('button', { class: 'close', title: 'Close tab', 'aria-label': 'Close tab' }, icon('M4 4l8 8M12 4l-8 8'))),
  };
  tab.el.addEventListener('click', (ev) => {
    if (ev.target.closest('.close')) closeTab(tab); else activate(tab);
  });
  tab.el.addEventListener('auxclick', (ev) => { if (ev.button === 1) closeTab(tab); });
  tab.el.addEventListener('keydown', (ev) => { if (ev.key === 'Enter' || ev.key === ' ') activate(tab); });
  $('tabs').append(tab.el);
  $('terms').append(tab.view);
  tabs.push(tab);

  const p = makePane(tab, { profileId, cwd: opts.cwd, label: opts.label });
  tab.view.append(p.el);
  tab.focused = p;
  activate(tab);
  startPane(p, opts);
  return tab;
}

function activate(tab) {
  if (activeTab !== tab) {
    const before = cur() && cur().profileId;
    if (activeTab) {
      activeTab.el.classList.remove('active');
      activeTab.el.setAttribute('aria-selected', 'false');
      activeTab.view.classList.remove('active');
    }
    activeTab = tab;
    tab.el.classList.add('active');
    tab.el.classList.remove('bell');
    tab.el.setAttribute('aria-selected', 'true');
    tab.view.classList.add('active');
    tab.el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    closeFind();
    if (before !== tab.focused.profileId) refreshPanel();
  }
  focusPane(tab.focused);
}

function closeTab(tab) {
  const i = tabs.indexOf(tab);
  if (i < 0) return;
  tabs.splice(i, 1);
  for (const p of tab.panes.splice(0)) {
    p.gone = true;
    if (p.id) { bySession.delete(p.id); api.CloseSession(p.id); }
    resizes.unobserve(p.el);
    p.term.dispose();
  }
  tab.el.remove();
  tab.view.remove();
  if (activeTab === tab) {
    activeTab = null;
    const next = tabs[Math.min(i, tabs.length - 1)];
    if (next) activate(next);
  }
  if (!tabs.length) rt.Quit();
}

function stepTab(delta) {
  if (tabs.length < 2 || !activeTab) return;
  activate(tabs[(tabs.indexOf(activeTab) + delta + tabs.length) % tabs.length]);
}

rt.EventsOn('pty:data', (id, b64) => {
  const bytes = decode(b64);
  const p = bySession.get(id);
  if (p) { feed(p, bytes); return; }
  if (!early.has(id)) early.set(id, []);
  early.get(id).push(bytes);
});

rt.EventsOn('pty:exit', (id, code) => {
  const p = bySession.get(id);
  early.delete(id);
  if (!p) return;
  bySession.delete(id);
  p.id = null;
  // A shell that fails as it starts has something to say: keep it readable.
  if (code !== 0 && Date.now() - p.born < 2500) {
    p.dead = true;
    p.term.write(`\r\n\x1b[2m[ended with code ${code} · press a key to close]\x1b[0m`);
    return;
  }
  closePane(p);
});

// ───────────────────────── clipboard ─────────────────────────

function copySelection(p) {
  const text = p.term.getSelection();
  if (!text) return false;
  rt.ClipboardSetText(text);
  p.term.clearSelection();
  return true;
}
function paste(p) {
  rt.ClipboardGetText().then((text) => { if (text) p.term.paste(text); p.term.focus(); });
}

// ───────────────────────── keys ─────────────────────────

const ARROWS = { ArrowLeft: 'focusLeft', ArrowRight: 'focusRight', ArrowUp: 'focusUp', ArrowDown: 'focusDown' };
const VIEWS = ['ports', 'ssh', 'projects'];

// The app's own shortcuts. macOS uses ⌘; Windows and Linux use Ctrl+Shift, so that
// Ctrl+C, Ctrl+R, Ctrl+W… keep going to the shell. Ctrl+Alt is only ours with the
// arrows: with letters and digits it is AltGr, which types @, #, { … on many keyboards.
function shortcut(ev) {
  const k = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key;
  const code = ev.code;
  if (ev.ctrlKey && !ev.altKey && !ev.metaKey && k === 'Tab') return ev.shiftKey ? 'prev' : 'next';
  if (isMac) {
    if (ev.ctrlKey && !ev.metaKey && !ev.altKey && !ev.shiftKey && /^Digit[1-3]$/.test(code)) return VIEWS[code.slice(5) - 1];
    if (!ev.metaKey || ev.ctrlKey) return null;
    if (ev.altKey) return !ev.shiftKey && ARROWS[k] ? ARROWS[k] : null;
    if (ev.shiftKey) {
      return k === 'p' ? 'palette' : k === 'd' ? 'splitDown' : k === 'Enter' ? 'zoom'
        : (k === '[' || k === '{') ? 'prev' : (k === ']' || k === '}') ? 'next' : null;
    }
    if (/^Digit[1-9]$/.test(code)) return 'tab' + code.slice(5);
    return { t: 'new', w: 'close', d: 'splitRight', k: 'palette', p: 'palette', b: 'panel', f: 'find', ',': 'config',
      '[': 'paneLeft', ']': 'paneRight', '=': 'zoomIn', '+': 'zoomIn', '-': 'zoomOut', '0': 'zoomReset' }[k] || null;
  }
  if (ev.altKey && !ev.ctrlKey && !ev.shiftKey && !ev.metaKey && /^Digit[1-9]$/.test(code)) return 'tab' + code.slice(5);
  if (ev.ctrlKey && ev.altKey && !ev.shiftKey && !ev.metaKey) return ARROWS[k] || null;
  if (!ev.ctrlKey || ev.altKey || ev.metaKey) return null;
  if (ev.shiftKey) {
    if (/^Digit[1-3]$/.test(code)) return VIEWS[code.slice(5) - 1];
    if (k === 'Enter') return 'zoom';
    return { t: 'new', w: 'close', d: 'splitRight', e: 'splitDown', p: 'palette', b: 'panel', f: 'find', c: 'copy', v: 'paste' }[k] || null;
  }
  if (k === '=' || k === '+') return 'zoomIn';
  if (k === '-') return 'zoomOut';
  if (k === '0') return 'zoomReset';
  if (k === ',') return 'config';
  return null;
}

function run(action) {
  const p = cur();
  switch (action) {
    case 'new': openTab({ cwd: p && p.cwd }); break;
    case 'close': if (p) closePane(p); break;          // the pane; its tab when it is the last one
    case 'closeTab': if (activeTab) closeTab(activeTab); break;
    case 'next': stepTab(1); break;
    case 'prev': stepTab(-1); break;
    case 'splitRight': splitPane('row'); break;
    case 'splitDown': splitPane('col'); break;
    case 'focusLeft': focusSide(-1, 0); break;
    case 'focusRight': focusSide(1, 0); break;
    case 'focusUp': focusSide(0, -1); break;
    case 'focusDown': focusSide(0, 1); break;
    case 'paneLeft': focusNext(-1); break;
    case 'paneRight': focusNext(1); break;
    case 'zoom': toggleZoom(); break;
    case 'palette': togglePalette(); break;
    case 'panel': showView(view ? '' : lastView); break;
    case 'ports': case 'ssh': case 'projects': showView(view === action ? '' : action); break;
    case 'find': openFind(); break;
    case 'copy': if (p) copySelection(p); break;
    case 'paste': if (p) paste(p); break;
    case 'zoomIn': zoomText(1); break;
    case 'zoomOut': zoomText(-1); break;
    case 'zoomReset': zoomText(0); break;
    case 'config': toggleSettings(); break;
    case 'configFile': api.OpenConfig().catch(toast); break;
    case 'reload': reloadConfig(); break;
    default:
      if (action.startsWith('tab')) { const n = tabs[Number(action.slice(3)) - 1]; if (n) activate(n); }
  }
}

// Keys pressed in a terminal. Returning false keeps xterm from sending them to the shell.
function onTermKey(p, ev) {
  if (ev.type !== 'keydown') return true;
  const action = shortcut(ev);
  if (action) {
    ev.preventDefault();
    ev.stopPropagation();
    run(action);
    return false;
  }
  if (!isMac && ev.ctrlKey && !ev.shiftKey && !ev.altKey && !ev.metaKey) {
    // Windows habits: Ctrl+C copies when something is selected (else it interrupts),
    // Ctrl+V pastes.
    if (ev.key === 'c' && p.term.hasSelection()) { ev.preventDefault(); copySelection(p); return false; }
    if (ev.key === 'v' && boot.platform === 'windows') { ev.preventDefault(); paste(p); return false; }
  }
  return true;
}

// The same shortcuts when the focus is somewhere else (a panel, the bar).
window.addEventListener('keydown', (ev) => {
  if (ev.target.closest && ev.target.closest('.xterm')) return;   // the terminal handles its own
  if (!$('settings').hidden) {                            // the dialog keeps its keys
    if (ev.key === 'Escape') { ev.preventDefault(); closeSettings(); return; }
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 's') { ev.preventDefault(); saveSettings(); return; }
    if (shortcut(ev) === 'config') { ev.preventDefault(); closeSettings(); }
    return;
  }
  if (ev.key === 'Escape') {
    if (!$('palette').hidden) { closePalette(); return; }
    if (!$('menu').hidden) { closeMenu(); return; }
    if (!$('find').hidden) closeFind();
    if (ev.target.id === 'projects-filter' && ev.target.value) { ev.target.value = ''; renderProjects(); return; }
    if (cur()) cur().term.focus();
    return;
  }
  const action = shortcut(ev);
  if (!action || action === 'copy' || action === 'paste') return;
  if (!$('palette').hidden && action !== 'palette') return;
  ev.preventDefault();
  run(action);
}, true);

// ───────────────────────── find ─────────────────────────

function openFind() {
  const p = cur();
  if (!p) return;
  $('find').hidden = false;
  const input = $('find-input');
  const sel = p.term.getSelection();
  if (sel && !sel.includes('\n')) input.value = sel;
  input.focus();
  input.select();
}
function closeFind() {
  if ($('find').hidden) return;
  $('find').hidden = true;
  $('find').classList.remove('miss');
  for (const p of allPanes()) p.search.clearDecorations();
}
function find(back) {
  const q = $('find-input').value;
  const p = cur();
  if (!p || !q) { $('find').classList.remove('miss'); return; }
  const found = back ? p.search.findPrevious(q) : p.search.findNext(q);
  $('find').classList.toggle('miss', !found);
}
$('find').addEventListener('submit', (ev) => { ev.preventDefault(); find(false); });
$('find-input').addEventListener('keydown', (ev) => {
  if (ev.key === 'Enter' && ev.shiftKey) { ev.preventDefault(); find(true); }
});
$('find-input').addEventListener('input', () => find(false));
$('find-prev').addEventListener('click', () => find(true));
$('find-close').addEventListener('click', () => { closeFind(); if (cur()) cur().term.focus(); });

// ───────────────────────── launchers, profiles, the bar ─────────────────────────

// Starts a launcher (Claude, opencode…) in a new tab, in the folder of the pane you are in.
function launch(l, where) {
  const p = cur();
  const profileId = (where && where.profileId) || (p && p.profileId) || boot.defaultProfile;
  openTab({ profileId, cwd: (where && where.cwd) || (p && p.cwd) || '', run: l.command, label: l.name });
}

function renderBar() {
  const box = $('launchers');
  box.replaceChildren();
  for (const l of (cfg.launchers || []).slice(0, 3)) {
    box.append(el('button', {
      class: 'launcher', text: l.name, title: `${l.command} · in a new tab, in this folder`,
      onclick: () => launch(l),
    }));
  }
  $('profile-btn').hidden = boot.profiles.length < 2;
  const mod = isMac ? '⌃' : 'Ctrl+Shift+';
  document.querySelectorAll('.view-btn').forEach((b, i) => { b.title = `${b.textContent} (${mod}${i + 1})`; });
}

function closeMenu() { $('menu').hidden = true; $('menu').replaceChildren(); }
function openMenu(anchor, items) {
  const menu = $('menu');
  menu.replaceChildren(...items.map((it) => el('button', {
    role: 'menuitem', text: it.label, onclick: () => { closeMenu(); it.run(); },
  })));
  menu.hidden = false;
  const r = anchor.getBoundingClientRect();
  menu.style.top = `${r.bottom + 4}px`;
  menu.style.left = `${Math.max(6, Math.min(r.left, window.innerWidth - menu.offsetWidth - 6))}px`;
  menu.querySelector('button').focus();
}
window.addEventListener('mousedown', (ev) => {
  if (!$('menu').hidden && !ev.target.closest('#menu') && !ev.target.closest('#profile-btn')) closeMenu();
}, true);

$('new-tab').addEventListener('click', () => run('new'));
$('profile-btn').addEventListener('click', (ev) => {
  if (!$('menu').hidden) { closeMenu(); return; }
  openMenu(ev.currentTarget, boot.profiles.map((p) => ({ label: p.name, run: () => openTab({ profileId: p.id }) })));
});
$('palette-btn').addEventListener('click', () => togglePalette());
document.querySelectorAll('.view-btn').forEach((b) => b.addEventListener('click', () => run(b.dataset.view)));
$('win-min').addEventListener('click', () => rt.WindowMinimise());
$('win-max').addEventListener('click', () => rt.WindowToggleMaximise());
$('win-close').addEventListener('click', () => rt.Quit());
$('bar').addEventListener('dblclick', (ev) => {       // like a title bar
  if (ev.target === $('bar') || ev.target === $('drag')) rt.WindowToggleMaximise();
});
$('tabs').addEventListener('wheel', (ev) => {
  if (ev.deltaY) { stepTab(ev.deltaY > 0 ? 1 : -1); ev.preventDefault(); }
}, { passive: false });
window.addEventListener('focus', () => { if (view === 'ports') refreshPorts(); });

// ───────────────────────── side panels: ports · ssh · projects ─────────────────────────
// One at a time, each with its own button and key. They look at the machine of the
// pane you are in: a WSL distro, or Windows itself in a PowerShell tab.

const panelProfile = () => (cur() && cur().profileId) || boot.defaultProfile;
let view = '';              // the open panel: '', 'ports', 'ssh' or 'projects'
let lastView = 'ports';     // what Ctrl+Shift+B opens again
let portsTimer = 0;

function showView(name) {
  view = VIEWS.includes(name) ? name : '';
  if (view) lastView = view;
  $('panel').hidden = !view;
  for (const v of VIEWS) $('v-' + v).hidden = v !== view;
  document.querySelectorAll('.view-btn').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.view === view)));
  clearInterval(portsTimer);
  if (view === 'ports') portsTimer = setInterval(() => { if (document.hasFocus()) refreshPorts(); }, 4000);
  refreshPanel();
  if (view === 'projects') { $('projects-filter').focus(); $('projects-filter').select(); }
  else if (cur()) cur().term.focus();
}

// Draws the open panel with what is known and asks for fresh data.
function refreshPanel() {
  const pid = panelProfile();
  if (view === 'ports') { renderPorts(); refreshPorts(); }
  if (view === 'ssh') { renderSSH(); loadSSH(pid); }
  if (view === 'projects') { renderProjects(); loadProjects(pid); }
}

let portsBusy = false;
async function refreshPorts() {
  if (portsBusy) return;
  portsBusy = true;
  const pid = panelProfile();
  try {
    known(pid).ports = await api.Ports(pid);
  } catch (err) {
    known(pid).ports = known(pid).ports || [];
  } finally {
    portsBusy = false;
  }
  if (pid === panelProfile()) renderPorts();
}
async function loadSSH(pid) {
  try { known(pid).ssh = await api.SSHHosts(pid); } catch (_) { known(pid).ssh = []; }
  if (pid === panelProfile()) renderSSH();
  renderPalette();
}
async function loadProjects(pid) {
  try { known(pid).projects = await api.Projects(pid); } catch (_) { known(pid).projects = []; }
  if (pid === panelProfile()) renderProjects();
  renderPalette();
}

function fillRows(listId, countId, items, emptyText, build) {
  const list = $(listId);
  $(countId).textContent = items && items.length ? String(items.length) : '';
  if (!items) { list.replaceChildren(el('li', { class: 'empty', text: 'looking…' })); return; }
  if (!items.length) { list.replaceChildren(el('li', { class: 'empty', text: emptyText })); return; }
  list.replaceChildren(...items.map(build));
}

const armed = new Set();   // "pid:port" of the stop buttons waiting for the second click

function renderPorts() {
  if (view !== 'ports') return;
  const pid = panelProfile();
  if (document.querySelector('#ports .act:hover, #ports .act:focus')) {
    // do not move a button from under the pointer: draw a moment later
    if (!renderPorts.retry) renderPorts.retry = setTimeout(() => { renderPorts.retry = 0; renderPorts(); }, 1200);
    return;
  }
  fillRows('ports', 'ports-count', known(pid).ports, 'nothing is listening', (p) => {
    const who = p.docker ? p.docker : (p.process || 'other user');
    const sub = p.docker ? 'docker' : shortPath(p.cwd) === '~' ? '~' : p.project;
    const key = `${p.pid}:${p.port}`;
    const kill = el('button', {
      class: 'act danger' + (armed.has(key) ? ' armed' : ''), text: armed.has(key) ? 'sure?' : 'stop',
      title: p.pid ? `Stop ${who} (pid ${p.pid}) · Shift+click: kill -9` : 'Belongs to another user: stop it with sudo',
      disabled: !p.pid,
      onclick: async (ev) => {
        if (!armed.has(key)) {
          armed.add(key);
          ev.currentTarget.textContent = 'sure?';
          ev.currentTarget.classList.add('armed');
          setTimeout(() => { armed.delete(key); }, 3000);
          return;
        }
        armed.delete(key);
        try { await api.KillProcess(pid, p.pid, ev.shiftKey); } catch (err) { toast(err); }
        setTimeout(refreshPorts, 350);
      },
    });
    return el('li', { class: 'row' },
      el('button', {
        class: 'main', title: `Open http://localhost:${p.port}` + (p.cwd ? `\n${shortPath(p.cwd)}` : '') + (p.addr !== '*' ? `\nlistens on ${p.addr}` : ''),
        onclick: () => api.OpenURL(`http://localhost:${p.port}`).catch(toast),
      },
      el('span', { class: 'port', text: String(p.port) }),
      el('span', { class: 'name' + (p.docker || p.process ? '' : ' unknown'), text: who }),
      sub ? el('span', { class: 'sub', text: sub }) : null),
      el('div', { class: 'acts' }, kill),
    );
  });
}

function renderSSH() {
  if (view !== 'ssh') return;
  const pid = panelProfile();
  fillRows('ssh', 'ssh-count', known(pid).ssh, 'no servers yet', (h) => {
    const sub = [h.user, h.hostName && h.hostName !== h.name ? h.hostName : ''].filter(Boolean).join('@');
    return el('li', { class: 'row' },
      el('button', { class: 'main', title: `${sshCommand(pid, h)} · in a new tab`, onclick: () => ssh(pid, h) },
        el('span', { class: 'name', text: h.name }),
        sub ? el('span', { class: 'sub', text: sub }) : null),
      h.saved ? el('div', { class: 'acts' }, el('button', { class: 'act', text: 'edit', title: 'Edit in Settings', onclick: () => openSettings('servers') })) : null,
    );
  });
  addSettingsLink('ssh', 'Add a server', 'servers');
}
// A host of ~/.ssh/config is reached by its name; a server of Settings by its address.
function sshCommand(profileId, h) {
  const posix = profile(profileId).kind === 'posix';
  const q = (s) => (posix ? (/^[\w@.:\/~+-]+$/.test(s) && !s.startsWith('~') ? s : s.startsWith('~/') ? '~/' + shq(s.slice(2)) : shq(s)) : (/\s/.test(s) ? `"${s}"` : s));
  if (!h.saved) return 'ssh ' + q(h.name);
  const args = ['ssh'];
  if (h.port && h.port !== '22') args.push('-p', q(h.port));
  if (h.identity) args.push('-i', q(h.identity));
  args.push(q(h.user ? `${h.user}@${h.hostName}` : h.hostName));
  return args.join(' ');
}
function ssh(profileId, h) {
  openTab({ profileId, run: sshCommand(profileId, h), label: h.name });
}
// A link to Settings under an empty panel.
function addSettingsLink(listId, text, section) {
  const empty = $(listId).querySelector('.empty');
  if (empty && !empty.querySelector('.link') && empty.textContent !== 'looking…') {
    empty.append(' · ', el('button', { class: 'link', text, onclick: () => openSettings(section) }));
  }
}

// Projects: one group per project folder of Settings (projectRoots). A group
// folds when you click its name; the filter looks in all of them.
function renderProjects() {
  if (view !== 'projects') return;
  const pid = panelProfile();
  const all = known(pid).projects;
  const box = $('projects');
  const words = $('projects-filter').value.toLowerCase().split(/\s+/).filter(Boolean);
  const launchers = (cfg.launchers || []).slice(0, 2);
  const folded = stored('rt.folded', {});
  if (!all) { $('projects-count').textContent = ''; box.replaceChildren(el('div', { class: 'empty', text: 'looking…' })); return; }
  const shown = all.filter((p) => words.every((w) => p.name.toLowerCase().includes(w) || p.root.toLowerCase() === w));
  $('projects-count').textContent = words.length ? `${shown.length} of ${all.length}` : (all.length ? String(all.length) : '');
  if (!shown.length) {
    box.replaceChildren(el('div', { class: 'empty', text: all.length ? 'nothing matches' : cfg.projectRoots.length ? 'no folders found' : 'no project folders yet' }));
    if (!all.length) addSettingsLink('projects', cfg.projectRoots.length ? 'Settings' : 'Add one', 'projects');
    return;
  }
  const groups = [];
  for (const p of shown) {
    let g = groups.find((x) => x.root === p.root);
    if (!g) groups.push(g = { root: p.root, items: [] });
    g.items.push(p);
  }
  box.replaceChildren(...groups.flatMap((g) => {
    const open = words.length > 0 || !folded[g.root];
    const head = el('button', {
      class: 'group-head', 'aria-expanded': String(open), title: open ? 'Fold' : 'Unfold',
      onclick: () => {
        if (words.length) return;
        const f = stored('rt.folded', {});
        if (f[g.root]) delete f[g.root]; else f[g.root] = true;
        store('rt.folded', f);
        renderProjects();
      },
    }, icon('M4 6.5l4 4 4-4'), el('span', { text: g.root }), el('span', { class: 'count', text: String(g.items.length) }));
    if (!open) return [head];
    return [head, el('ul', { class: 'rows' }, ...g.items.map((p) => el('li', { class: 'row' },
      el('button', { class: 'main', title: `${shortPath(p.path)} · open in a new tab`, onclick: () => openTab({ profileId: pid, cwd: p.path }) },
        el('span', { class: 'git' + (p.git ? ' on' : ''), title: p.git ? 'git repo' : '' }),
        el('span', { class: 'name', text: p.name })),
      el('div', { class: 'acts' }, ...launchers.map((l) => el('button', {
        class: 'act', text: l.name, title: `${l.command} in ${p.name}`,
        onclick: () => launch(l, { profileId: pid, cwd: p.path }),
      }))))))];
  }));
}
$('projects-filter').addEventListener('input', renderProjects);
$('projects-filter').addEventListener('keydown', (ev) => {
  if (ev.key !== 'Enter') return;                       // Enter: the first one that matches
  const first = $('projects').querySelector('.row .main');
  if (first) { ev.preventDefault(); first.click(); }
});

// ───────────────────────── command palette ─────────────────────────

let paletteItems = [];
let paletteSel = 0;

function paletteSource() {
  const pid = panelProfile();
  const k = known(pid);
  const p0 = cur();
  const here = p0 && p0.cwd ? shortPath(p0.cwd) : '';
  const mod = isMac ? '⌘' : 'Ctrl+Shift+';
  const ctl = isMac ? '⌘' : 'Ctrl+';
  const items = [];
  for (const l of cfg.launchers || []) {
    items.push({ kind: 'run', label: l.name, hint: here ? `in ${here}` : l.command, search: l.command, run: () => launch(l) });
  }
  for (const h of k.ssh || []) {
    items.push({ kind: 'ssh', label: h.name, hint: [h.user, h.hostName].filter(Boolean).join('@'), run: () => ssh(pid, h) });
  }
  items.push(
    { kind: 'pane', label: 'Split right', hint: 'a pane beside this one', keys: mod + 'D', run: () => run('splitRight') },
    { kind: 'pane', label: 'Split down', hint: 'a pane below this one', keys: isMac ? '⌘⇧D' : mod + 'E', run: () => run('splitDown') },
    { kind: 'pane', label: 'Maximise pane', hint: 'and back', keys: isMac ? '⌘⇧Enter' : mod + 'Enter', run: () => run('zoom') },
    { kind: 'pane', label: 'Close pane', keys: mod + 'W', run: () => run('close') },
  );
  for (const p of boot.profiles) {
    items.push({ kind: 'tab', label: `New tab: ${p.name}`, keys: p.id === pid ? mod + 'T' : '', run: () => openTab({ profileId: p.id, cwd: p.id === pid && p0 ? p0.cwd : '' }) });
  }
  items.push(
    { kind: 'tab', label: 'Close tab', hint: 'with all its panes', run: () => run('closeTab') },
    { kind: 'panel', label: 'Ports', hint: 'what is listening', keys: (isMac ? '⌃' : mod) + '1', run: () => showView('ports') },
    { kind: 'panel', label: 'SSH', hint: 'your servers and ~/.ssh/config', keys: (isMac ? '⌃' : mod) + '2', run: () => showView('ssh') },
    { kind: 'panel', label: 'Projects', hint: 'your project folders', keys: (isMac ? '⌃' : mod) + '3', run: () => showView('projects') },
    { kind: 'view', label: 'Find in the terminal', keys: mod + 'F', run: () => openFind() },
    { kind: 'view', label: 'Bigger text', keys: ctl + '+', run: () => zoomText(1) },
    { kind: 'view', label: 'Smaller text', keys: ctl + '-', run: () => zoomText(-1) },
    { kind: 'app', label: 'Settings', hint: 'project folders, servers, launchers, look', keys: ctl + ',', run: () => openSettings() },
    { kind: 'app', label: 'Add a server', hint: 'Settings · Servers', search: 'vps ssh host', run: () => openSettings('servers', true) },
    { kind: 'app', label: 'Add a project folder', hint: 'Settings · Projects', search: 'root', run: () => openSettings('projects') },
    { kind: 'app', label: 'Open the config file', hint: boot.configPath, run: () => run('configFile') },
    { kind: 'app', label: 'Reload the config', run: () => reloadConfig() },
  );
  const first = (cfg.launchers || [])[0];
  for (const p of k.projects || []) {
    items.push({
      kind: 'project', label: p.name, hint: shortPath(p.path), search: p.root,
      run: (alt) => (alt && first ? launch(first, { profileId: pid, cwd: p.path }) : openTab({ profileId: pid, cwd: p.path })),
      alt: first ? first.name : '',
    });
  }
  for (const p of k.ports || []) {
    items.push({
      kind: 'port', label: `localhost:${p.port}`, hint: [p.docker || p.process, p.docker ? 'docker' : p.project].filter(Boolean).join(' · '),
      run: () => api.OpenURL(`http://localhost:${p.port}`).catch(toast),
    });
  }
  return items;
}

function renderPalette() {
  if ($('palette').hidden) return;
  const words = $('palette-input').value.toLowerCase().split(/\s+/).filter(Boolean);
  const all = paletteSource();
  paletteItems = !words.length ? all : all
    .map((it) => {
      const label = it.label.toLowerCase();
      const hay = `${it.kind} ${label} ${(it.hint || '').toLowerCase()} ${(it.search || '').toLowerCase()}`;
      let score = 0;
      for (const w of words) {
        if (!hay.includes(w)) return null;
        score += label.startsWith(w) ? 0 : label.includes(w) ? 10 : 30;
      }
      return { it, score };
    })
    .filter(Boolean)
    .sort((a, b) => a.score - b.score)
    .map((x) => x.it);
  paletteSel = Math.min(paletteSel, Math.max(0, paletteItems.length - 1));
  const list = $('palette-list');
  if (!paletteItems.length) {
    list.replaceChildren(el('li', { class: 'none', text: 'nothing matches' }));
  } else {
    list.replaceChildren(...paletteItems.map((it, i) => el('li', {
      class: i === paletteSel ? 'sel' : '', role: 'option', 'aria-selected': String(i === paletteSel),
      onmousemove: () => { if (paletteSel !== i) { paletteSel = i; markPalette(); } },
      onclick: (ev) => pick(i, ev.shiftKey),
    },
    el('span', { class: 'kind', text: it.kind }),
    el('span', { class: 'label', text: it.label }),
    el('span', { class: 'hint', text: it.hint || '' }),
    el('span', { class: 'keys', text: it.keys || '' }))));
  }
  markPalette();
}
function markPalette() {
  const rows = $('palette-list').children;
  for (let i = 0; i < rows.length; i++) {
    const on = i === paletteSel && paletteItems.length > 0;
    rows[i].classList.toggle('sel', on);
    rows[i].setAttribute('aria-selected', String(on));
    if (on) rows[i].scrollIntoView({ block: 'nearest' });
  }
  const it = paletteItems[paletteSel];
  $('palette-foot').textContent = it && it.alt ? `Enter: open in a new tab · Shift+Enter: open with ${it.alt}` : 'Enter: run · Esc: close';
}
function pick(i, alt) {
  const it = paletteItems[i];
  if (!it) return;
  closePalette();
  it.run(alt);
}
function togglePalette() {
  if (!$('palette').hidden) { closePalette(); return; }
  $('palette').hidden = false;
  $('palette-input').value = '';
  paletteSel = 0;
  renderPalette();
  $('palette-input').focus();
  const pid = panelProfile();           // fill in what is not known yet
  if (!known(pid).ssh) loadSSH(pid);
  if (!known(pid).projects) loadProjects(pid);
  api.Ports(pid).then((ports) => { known(pid).ports = ports; renderPalette(); }).catch(() => {});
}
function closePalette() {
  $('palette').hidden = true;
  if (cur()) cur().term.focus();
}
$('palette-input').addEventListener('input', () => { paletteSel = 0; renderPalette(); });
$('palette-input').addEventListener('keydown', (ev) => {
  const n = paletteItems.length;
  if (ev.key === 'ArrowDown' || (ev.ctrlKey && ev.key === 'n')) { ev.preventDefault(); if (n) { paletteSel = (paletteSel + 1) % n; markPalette(); } }
  else if (ev.key === 'ArrowUp' || (ev.ctrlKey && ev.key === 'p' && !ev.shiftKey)) { ev.preventDefault(); if (n) { paletteSel = (paletteSel - 1 + n) % n; markPalette(); } }
  else if (ev.key === 'Enter') { ev.preventDefault(); pick(paletteSel, ev.shiftKey); }
});
$('palette').addEventListener('mousedown', (ev) => { if (ev.target === $('palette')) closePalette(); });

// ───────────────────────── config ─────────────────────────

// Uses a new Boot (after a reload or a save): the bar, the look, the panels.
function useBoot(b) {
  boot = b;
  cfg = boot.config;
  fontSize = cfg.fontSize;
  cache.clear();
  renderBar();
  applyLook();
  refreshPanel();
}
async function reloadConfig() {
  useBoot(await api.ReloadConfig());
  if (boot.configError) toast(`config.json: ${boot.configError}\nUsing the defaults.`);
  else toast('Config reloaded', true);
}

// ───────────────────────── settings ─────────────────────────
// A form over config.json: what you change is a draft until Save. Paths are as the
// shell of the pane you are in sees them (a WSL tab: /home/…, ~ for home).

const SECTIONS = [
  ['general', 'General'], ['projects', 'Projects'], ['servers', 'Servers'], ['launchers', 'Launchers'],
];
let draft = null;
let section = 'general';

const clone = (o) => JSON.parse(JSON.stringify(o));
function field(label, control, help) {
  const id = 's-' + (++seq);
  control.id = control.id || id;
  return [el('label', { for: control.id, text: label }),
    el('div', { class: 's-field' }, control, help ? el('span', { class: 'help', text: help }) : null)];
}
function input(value, oninput, props) {
  return el('input', Object.assign({ type: 'text', value: value ?? '', spellcheck: 'false', autocomplete: 'off',
    oninput: (ev) => oninput(ev.target.value) }, props || {}));
}
function check(label, value, onchange) {
  return el('label', { class: 's-check' },
    el('input', { type: 'checkbox', checked: !!value, onchange: (ev) => onchange(ev.target.checked) }), label);
}
function select(value, options, onchange) {
  const s = el('select', { onchange: (ev) => onchange(ev.target.value) },
    ...options.map(([v, t]) => el('option', { value: v, text: t })));
  s.value = value;
  return s;
}
const xIcon = () => icon('M4 4l8 8M12 4l-8 8');
function removeBtn(label, onclick) {
  return el('button', { type: 'button', class: 'btn icon', title: label, 'aria-label': label, onclick }, xIcon());
}
function moveBtn(up, disabled, onclick) {
  return el('button', { type: 'button', class: 'btn icon move', title: up ? 'Move up' : 'Move down', 'aria-label': up ? 'Move up' : 'Move down', disabled, onclick },
    icon(up ? 'M4 9.5l4-4 4 4' : 'M4 6.5l4 4 4-4'));
}
const pickerProfile = () => profile(panelProfile()) || boot.profiles[0];

function renderSettings(focusLast) {
  $('settings-nav').replaceChildren(...SECTIONS.map(([id, name]) => el('button', {
    type: 'button', role: 'tab', text: name, 'aria-selected': String(id === section),
    onclick: () => { section = id; renderSettings(); },
  })));
  const body = $('settings-body');
  body.replaceChildren(...{ general: sGeneral, projects: sProjects, servers: sServers, launchers: sLaunchers }[section]());
  if (focusLast) {
    const inputs = body.querySelectorAll('.s-list input[type="text"]');
    const target = section === 'servers' ? body.querySelector('.s-list .s-card:last-child input') : inputs[inputs.length - 1];
    if (target) target.focus();
  }
}

function sGeneral() {
  const d = draft;
  return [el('div', { class: 's-grid' },
    ...field('Theme', select(d.theme, [['dark', 'Dark'], ['light', 'Light'], ['system', 'Same as the system']], (v) => { d.theme = v; })),
    ...fontField(d),
    ...field('Font size', el('input', { type: 'number', min: 6, max: 40, value: d.fontSize, oninput: (ev) => { d.fontSize = Number(ev.target.value) || 13; fontPreview(); } })),
    ...field('Line height', el('input', { type: 'number', min: 1, max: 2, step: 0.05, value: d.lineHeight, oninput: (ev) => { d.lineHeight = Number(ev.target.value) || 1.2; fontPreview(); } })),
    ...field('Default shell', select(d.defaultProfile && boot.profiles.some((p) => p.id === d.defaultProfile || p.name === d.defaultProfile) ? (boot.profiles.find((p) => p.id === d.defaultProfile || p.name === d.defaultProfile).id) : '',
      [['', `The first one (${boot.profiles[0] ? boot.profiles[0].name : 'none'})`], ...boot.profiles.map((p) => [p.id, p.name])], (v) => { d.defaultProfile = v; })),
    ...field('Panel at start', select(d.panel || '', [['', 'None'], ['ports', 'Ports'], ['ssh', 'SSH'], ['projects', 'Projects']], (v) => { d.panel = v; })),
    ...field('Scrollback', el('input', { type: 'number', min: 0, step: 1000, value: d.scrollback, oninput: (ev) => { d.scrollback = Math.max(0, Number(ev.target.value) || 0); } }), 'Lines kept above the screen.'),
    el('span'), el('div', { class: 's-field' }, check('Load my ~/.zshrc', d.loadUserRc, (v) => { d.loadUserRc = v; }),
      el('span', { class: 'help', text: 'Your own lines (PATH, aliases); new tabs only.' })),
    el('span'), check('Copy on select', d.copyOnSelect, (v) => { d.copyOnSelect = v; }),
    el('span'), check('Blinking cursor', d.cursorBlink, (v) => { d.cursorBlink = v; }),
  )];
}

// Font: the fonts of this computer, monospaced ones first. Picking one writes
// "<font>", then fallbacks that have the Nerd Font icons, then monospace.
let fontList = null;                 // installed families, null until asked
const FONT_FALLBACK = ['JetBrainsMono Nerd Font', 'JetBrainsMono NF', 'Symbols Nerd Font Mono'];
const cssFamilies = (list) => list.split(',').map((f) => f.trim().replace(/^["']|["']$/g, '')).filter(Boolean);
const fontCss = (name) => [name, ...FONT_FALLBACK.filter((f) => f !== name)].map((f) => `"${f.replace(/"/g, '')}"`).join(', ') + ', monospace';

function isMono(name) {
  const c = isMono.ctx || (isMono.ctx = document.createElement('canvas').getContext('2d'));
  c.font = `20px "${name.replace(/"/g, '')}"`;
  return Math.abs(c.measureText('iiiiiiiiii').width - c.measureText('WWWWWWWWWW').width) < 1;
}
// The font the terminal really uses: the first of the list that is installed.
function currentFont(list) {
  const fams = cssFamilies(list);
  if (!fontList) return fams[0] || '';
  const lower = new Map(fontList.map((f) => [f.toLowerCase(), f]));
  for (const f of fams) if (lower.has(f.toLowerCase())) return lower.get(f.toLowerCase());
  return '';
}

function fontPreview() {
  const box = document.getElementById('font-preview');
  if (!box || !draft) return;
  box.style.fontFamily = draft.fontFamily;
  box.style.fontSize = `${draft.fontSize}px`;
  box.style.lineHeight = String(draft.lineHeight);
}

function fontField(d) {
  const custom = input(d.fontFamily, (v) => { d.fontFamily = v; fontPreview(); }, { class: 'mono', 'aria-label': 'Font list (CSS)' });
  const sel = el('select', { 'aria-label': 'Font' });
  const help = el('span', { class: 'help' });
  const preview = el('div', { id: 'font-preview', class: 'font-preview' },
    el('span', { style: 'color: var(--accent)', text: '~/dev/projects' }), ' on ', el('span', { style: 'color: #c792ea', text: 'main' }), '\n',
    el('span', { style: 'color: var(--ok)', text: '❯' }), ' ls -la {}[] => != 0O 1lI |');
  const fill = () => {
    const now = currentFont(d.fontFamily);
    const opts = [];
    if (!fontList) {
      opts.push(el('option', { value: '', text: now ? `${now} (looking for fonts…)` : 'Looking for fonts…' }));
    } else {
      const mono = fontList.filter(isMono);
      const other = fontList.filter((f) => !mono.includes(f));
      if (!now) opts.push(el('option', { value: '', text: 'Not installed: ' + (cssFamilies(d.fontFamily)[0] || '?') }));
      opts.push(el('optgroup', { label: 'Monospaced' }, ...mono.map((f) => el('option', { value: f, text: f }))));
      if (other.length) opts.push(el('optgroup', { label: 'Other fonts' }, ...other.map((f) => el('option', { value: f, text: f }))));
    }
    opts.push(el('option', { value: '\u0000custom', text: 'Custom CSS list…' }));
    sel.replaceChildren(...opts);
    sel.value = fontField.custom ? '\u0000custom' : (fontList ? now : '');
    custom.hidden = !fontField.custom;
    help.textContent = fontField.custom
      ? 'Font names separated by commas: the first one installed is used.'
      : fontList ? `${fontList.filter(isMono).length} monospaced fonts on this computer.` : '';
  };
  sel.addEventListener('change', () => {
    fontField.custom = sel.value === '\u0000custom';
    if (!fontField.custom && sel.value) { d.fontFamily = fontCss(sel.value); custom.value = d.fontFamily; }
    fill();
    fontPreview();
    if (fontField.custom) custom.focus();
  });
  fontField.custom = false;
  fill();
  if (!fontList) {
    api.Fonts().then((list) => { fontList = list && list.length ? list : null; if (!fontList) fontField.custom = true; })
      .catch(() => { fontField.custom = true; })
      .finally(() => { if (sel.isConnected) fill(); });
  }
  setTimeout(fontPreview);
  return field('Font', sel, null).concat([el('span'), el('div', { class: 's-field' }, custom, help, preview)]);
}

function sProjects() {
  const d = draft;
  const pr = pickerProfile();
  const list = el('div', { class: 's-list' }, ...(d.projectRoots.length ? d.projectRoots.map((r, i) => el('div', { class: 's-item' },
    input(r, (v) => { d.projectRoots[i] = v; }, { class: 'mono', placeholder: '~/dev/projects', 'aria-label': 'Folder' }),
    removeBtn('Remove', () => { d.projectRoots.splice(i, 1); renderSettings(); }),
  )) : [el('div', { class: 's-empty', text: 'No folders yet.' })]));
  return [
    el('p', { class: 's-intro' }, 'Each folder is a group in the Projects panel; the folders inside it are your projects. Paths as ',
      el('strong', { text: pr ? pr.name : 'the shell' }), ' sees them; ', el('code', { text: '~' }), ' is your home.'),
    list,
    el('div', { class: 's-actions' },
      el('button', { type: 'button', class: 'btn', text: 'Browse…', onclick: async () => {
        try {
          const path = await api.PickFolder(pr.id);
          if (path && !d.projectRoots.includes(path)) { d.projectRoots.push(path); renderSettings(); }
        } catch (err) { toast(err); }
      } }),
      el('button', { type: 'button', class: 'btn small', text: 'Type a path', onclick: () => { d.projectRoots.push(''); renderSettings(true); } })),
  ];
}

function sServers() {
  const d = draft;
  const pr = pickerProfile();
  const cards = d.servers.map((s, i) => {
    const preview = el('div', { class: 'preview' });
    const upd = (k) => (v) => { s[k] = v; preview.textContent = s.hostName ? sshCommand(pr.id, Object.assign({ saved: true }, s)) : 'ssh …'; };
    upd('name')(s.name);
    return el('div', { class: 's-card' },
      input(s.name, upd('name'), { placeholder: 'Name (vps)', 'aria-label': 'Name' }),
      input(s.user, upd('user'), { placeholder: 'User (root)', 'aria-label': 'User' }),
      input(s.port, upd('port'), { placeholder: '22', 'aria-label': 'Port', inputmode: 'numeric' }),
      el('div', { class: 'row-end' },
        input(s.hostName, upd('hostName'), { class: 'mono', placeholder: 'Address: 203.0.113.7 or vps.example.com', 'aria-label': 'Address' }),
        removeBtn('Remove server', () => { d.servers.splice(i, 1); renderSettings(); })),
      el('div', { class: 'row-end' },
        input(s.identity, upd('identity'), { class: 'mono', placeholder: 'Key file (optional): ~/.ssh/id_ed25519', 'aria-label': 'Key file' }),
        el('button', { type: 'button', class: 'btn small', text: 'Browse…', onclick: async () => {
          try { const f = await api.PickFile(pr.id); if (f) { s.identity = f; renderSettings(); } } catch (err) { toast(err); }
        } })),
      preview);
  });
  return [
    el('p', { class: 's-intro' }, 'Servers you reach with SSH: they show first in the SSH panel and the palette, next to the hosts of ',
      el('code', { text: '~/.ssh/config' }), '. A click opens ', el('code', { text: 'ssh' }), ' in a new tab.'),
    el('div', { class: 's-list' }, ...(cards.length ? cards : [el('div', { class: 's-empty', text: 'No servers yet.' })])),
    el('div', { class: 's-actions' }, el('button', { type: 'button', class: 'btn', text: 'Add a server', onclick: () => {
      d.servers.push({ name: '', hostName: '', user: '', port: '', identity: '' }); renderSettings(true);
    } })),
  ];
}

function sLaunchers() {
  const d = draft;
  const n = d.launchers.length;
  const rows = d.launchers.map((l, i) => el('div', { class: 's-item' },
    input(l.name, (v) => { l.name = v; }, { placeholder: 'Name', 'aria-label': 'Name', style: 'flex: 0 0 140px' }),
    input(l.command, (v) => { l.command = v; }, { class: 'mono', placeholder: 'Command: claude --continue', 'aria-label': 'Command' }),
    moveBtn(true, i === 0, () => { d.launchers.splice(i - 1, 0, d.launchers.splice(i, 1)[0]); renderSettings(); }),
    moveBtn(false, i === n - 1, () => { d.launchers.splice(i + 1, 0, d.launchers.splice(i, 1)[0]); renderSettings(); }),
    removeBtn('Remove', () => { d.launchers.splice(i, 1); renderSettings(); })));
  return [
    el('p', { class: 's-intro', text: 'Programs that open in a new tab, in the folder you are in. The first three are buttons in the bar; all are in the palette.' }),
    el('div', { class: 's-list' }, ...(rows.length ? rows : [el('div', { class: 's-empty', text: 'No launchers.' })])),
    el('div', { class: 's-actions' }, el('button', { type: 'button', class: 'btn', text: 'Add a launcher', onclick: () => {
      d.launchers.push({ name: '', command: '' }); renderSettings(true);
    } })),
  ];
}

function openSettings(to, addServer) {
  if (!$('palette').hidden) closePalette();
  closeMenu();
  draft = clone(cfg);
  draft.projectRoots = draft.projectRoots || [];
  draft.servers = draft.servers || [];
  draft.launchers = draft.launchers || [];
  section = to || section;
  if (addServer) draft.servers.push({ name: '', hostName: '', user: '', port: '', identity: '' });
  $('settings').hidden = false;
  renderSettings(!!addServer);
  if (!addServer) $('settings-nav').querySelector('[aria-selected="true"]').focus();
}
function closeSettings() {
  $('settings').hidden = true;
  draft = null;
  if (cur()) cur().term.focus();
}
function toggleSettings() { if ($('settings').hidden) openSettings(); else closeSettings(); }
async function saveSettings() {
  if (!draft) return;
  const bad = draft.servers.find((s) => (s.name || s.user || s.port || s.identity) && !s.hostName.trim());
  if (bad) { section = 'servers'; renderSettings(); toast(`The server ${bad.name || ''} needs an address.`); return; }
  try {
    useBoot(await api.SaveConfig(draft));
    closeSettings();
    toast('Settings saved', true);
  } catch (err) {
    toast(err);
  }
}
$('settings-btn').addEventListener('click', () => toggleSettings());
$('settings-close').addEventListener('click', closeSettings);
$('settings-cancel').addEventListener('click', closeSettings);
$('settings-save').addEventListener('click', saveSettings);
$('settings-file').addEventListener('click', () => { closeSettings(); run('configFile'); });
$('settings').addEventListener('mousedown', (ev) => { if (ev.target === $('settings')) closeSettings(); });

// ───────────────────────── start ─────────────────────────

(async function start() {
  boot = await api.Bootstrap();
  cfg = boot.config;
  fontSize = cfg.fontSize;
  isMac = boot.platform === 'darwin';
  document.body.classList.add(isMac ? 'mac' : boot.platform);
  $('win').hidden = boot.platform !== 'windows';
  renderBar();
  applyLook();
  if (boot.configError) toast(`config.json: ${boot.configError}\nUsing the defaults.`);
  await document.fonts.ready.catch(() => {});
  openTab({});
  if (VIEWS.includes(cfg.panel)) { showView(cfg.panel); if (cur()) cur().term.focus(); }
  autorun(boot.autorun);
})();

// RUBEN_TERM_AUTORUN="ports ;; sleep:800 ;; type:ls\r ;; splitRight": steps the window
// runs by itself after it starts, for checking it without hands. A step is an action
// of run(), or sleep:<ms>, type:<text>, launch:<n>, project:<name>, profile:<id>,
// palette:<text>, filter:<text>. Steps are separated by " ;; ".
async function autorun(script) {
  if (!script) return;
  const pause = (ms) => new Promise((r) => setTimeout(r, ms));
  await pause(1500);
  for (const step of script.split(' ;; ')) {
    const [name, ...rest] = step.split(':');
    const arg = rest.join(':');
    const p = cur();
    if (name === 'sleep') await pause(Number(arg));
    else if (name === 'type') { if (p && p.id) api.Write(p.id, arg.replace(/\\r/g, '\r').replace(/\\x03/g, '\x03')); }
    else if (name === 'launch') launch(cfg.launchers[Number(arg)]);
    else if (name === 'project') openTab({ cwd: ((known(panelProfile()).projects || []).find((x) => x.name === arg) || {}).path });
    else if (name === 'profile') openTab({ profileId: arg });
    else if (name === 'palette') { togglePalette(); $('palette-input').value = arg; renderPalette(); }
    else if (name === 'filter') { $('projects-filter').value = arg; renderProjects(); }
    else run(name);
    await pause(250);
  }
}
