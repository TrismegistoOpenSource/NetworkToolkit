// SPDX-License-Identifier: MIT

// ============================================================
//  Toolkit Tailscale e OpenVPN (solo Linux)
// ============================================================
// La GUI non reimplementa nulla: ogni tasto chiede al backend di lanciare i
// normali comandi da terminale, e la console mostra la riga di comando esatta
// e il suo output in diretta (evento vpn:console). I LED leggono lo stato con
// comandi leggeri SOLO mentre il toolkit è visibile: il controllo periodico si
// ferma uscendo dal toolkit e, ovviamente, chiudendo la finestra.

import {
  VPNToolkits,
  TailscaleState,
  TailscaleService,
  TailscaleConnection,
  OpenVPNState,
  OpenVPNTunnel,
  OpenVPNSearchPrivileged,
  PublicIP,
  OpenURL,
} from '../wailsjs/go/main/App';
import { EventsOn, ClipboardSetText } from '../wailsjs/runtime';

// --- Tipi (specchio del JSON di internal/vpn) ---
interface UnitState { unit: string; active: string; enabled: string; }
interface Peer { name: string; dnsName: string; os: string; ips: string[]; online: boolean; self: boolean; }
interface TSState {
  supported: boolean; installed: boolean; hasPkexec: boolean;
  installHint: string; message: string;
  service: UnitState; backend: string; authUrl: string;
  health: string[] | null; peers: Peer[] | null; upPending: boolean;
}
interface Tunnel { name: string; unit: string; config: string; legacy: boolean; active: string; enabled: string; }
interface OVState {
  supported: boolean; installed: boolean; hasPkexec: boolean;
  installHint: string; message: string;
  tunnels: Tunnel[] | null; notes: string[] | null;
  unreadable: boolean; searchedRoot: boolean;
}
interface ActionResult { ok: boolean; message: string; pending: boolean; authUrl: string; ipBefore: string; ipAfter: string; }
interface ConsoleLine { toolkit: string; time: string; stream: string; text: string; }

const POLL_MS = 4000;
const byId = (id: string) => document.getElementById(id) as HTMLElement;
const btn = (id: string) => document.getElementById(id) as HTMLButtonElement;

// ------------------------------------------------------------
//  Console
// ------------------------------------------------------------
const URL_RE = /https:\/\/[^\s"'<>]+/g;

function appendConsole(consoleId: string, l: ConsoleLine): void {
  const con = byId(consoleId);
  const row = document.createElement('span');
  const t = document.createElement('span');
  t.className = 't';
  t.textContent = l.time + '  ';
  row.appendChild(t);

  const body = document.createElement('span');
  body.className = l.stream === 'err' ? 'serr' : l.stream;
  const text = l.stream === 'cmd' ? '$ ' + l.text : l.text;
  // Gli indirizzi https diventano link: è così che si apre la pagina di
  // login che stampa `tailscale up`. Il testo si inserisce sempre come
  // testo, mai come HTML.
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    const url = m[0].replace(/[.,;)]+$/, '');
    const at = m.index ?? 0;
    body.appendChild(document.createTextNode(text.slice(last, at)));
    const a = document.createElement('a');
    a.textContent = url;
    a.title = 'Apri nel browser';
    a.addEventListener('click', () => { void openURL(url); });
    body.appendChild(a);
    last = at + url.length;
  }
  body.appendChild(document.createTextNode(text.slice(last) + '\n'));
  row.appendChild(body);

  const atBottom = con.scrollHeight - con.scrollTop - con.clientHeight < 30;
  con.appendChild(row);
  if (atBottom) con.scrollTop = con.scrollHeight;
}

async function openURL(url: string): Promise<void> {
  try { await OpenURL(url); } catch (e) { toastFn('Indirizzo non apribile: ' + String(e), 'err'); }
}

function wireConsoleButtons(): void {
  for (const b of Array.from(document.querySelectorAll('[data-copy-console]')) as HTMLElement[]) {
    b.addEventListener('click', async () => {
      const text = byId(b.dataset.copyConsole as string).textContent || '';
      try {
        await ClipboardSetText(text);
        toastFn('Console copiata', 'ok');
      } catch {
        toastFn('Copia non riuscita', 'err');
      }
    });
  }
  for (const b of Array.from(document.querySelectorAll('[data-clear-console]')) as HTMLElement[]) {
    b.addEventListener('click', () => { byId(b.dataset.clearConsole as string).innerHTML = ''; });
  }
}

// ------------------------------------------------------------
//  Elementi comuni
// ------------------------------------------------------------
type Toast = (msg: string, kind: 'ok' | 'err') => void;
let toastFn: Toast = () => {};

/** LED come quelli delle schede di rete: verde attivo, ambra in transizione,
 *  rosso guasto, grigio spento o assente. */
function setLed(id: string, kind: '' | 'active' | 'spoofed' | 'down' | 'fail'): void {
  byId(id).className = 'led' + (kind ? ' ' + kind : '');
}

function unitLed(active: string): '' | 'active' | 'spoofed' | 'down' | 'fail' {
  switch (active) {
    case 'active': return 'active';
    case 'activating': case 'deactivating': case 'reloading': return 'spoofed';
    case 'failed': return 'fail';
    case 'inactive': return 'down';
    default: return '';
  }
}

const ACTIVE_IT: Record<string, string> = {
  active: 'attivo', inactive: 'fermo', failed: 'in errore', activating: 'in avvio',
  deactivating: 'in arresto', reloading: 'in ricarica', unknown: 'stato sconosciuto',
};
const ENABLED_IT: Record<string, string> = {
  enabled: 'parte all\'avvio', disabled: 'non parte all\'avvio', static: 'statico',
  masked: 'mascherato', 'not-found': 'non installato', unknown: '',
};

function setSwitch(id: string, checked: boolean, enabled: boolean): void {
  const b = btn(id);
  b.setAttribute('aria-checked', checked ? 'true' : 'false');
  b.disabled = !enabled;
}

function showNotice(prefix: string, text: string, cmd: string): void {
  const box = byId(prefix + '-notice');
  box.hidden = !text;
  byId(prefix + '-notice-text').textContent = text;
  byId(prefix + '-notice-cmd-wrap').hidden = !cmd;
  byId(prefix + '-notice-cmd').textContent = cmd;
}

function wireNoticeCopy(prefix: string): void {
  btn(prefix + '-notice-copy').addEventListener('click', async () => {
    try {
      await ClipboardSetText(byId(prefix + '-notice-cmd').textContent || '');
      toastFn('Comando copiato', 'ok');
    } catch { toastFn('Copia non riuscita', 'err'); }
  });
}

function asResult(r: unknown): ActionResult {
  return (r || { ok: false, message: 'nessuna risposta' }) as ActionResult;
}

// ------------------------------------------------------------
//  Tailscale
// ------------------------------------------------------------
let ts: TSState | null = null;
let tsBusy = false;

const BACKEND_IT: Record<string, [string, '' | 'active' | 'spoofed' | 'down' | 'fail']> = {
  Running: ['connesso', 'active'],
  Starting: ['in connessione…', 'spoofed'],
  NeedsLogin: ['serve il login', 'spoofed'],
  NeedsMachineAuth: ['in attesa dell\'approvazione dell\'amministratore', 'spoofed'],
  Stopped: ['disconnesso', 'down'],
  NoState: ['in avvio…', 'spoofed'],
};

function renderTailscale(): void {
  const s = ts;
  if (!s) return;
  const usable = s.supported && s.installed;
  showNotice('ts', s.message, s.installHint);

  // Servizio
  if (!usable) {
    setLed('ts-svc-led', '');
    byId('ts-svc-state').textContent = s.supported ? 'non installato' : 'non supportato';
  } else {
    setLed('ts-svc-led', unitLed(s.service.active));
    const en = ENABLED_IT[s.service.enabled] ?? s.service.enabled;
    byId('ts-svc-state').textContent =
      (ACTIVE_IT[s.service.active] ?? s.service.active) + (en ? ' · ' + en : '');
  }
  const canSvc = usable && s.hasPkexec && !tsBusy;
  btn('ts-svc-start').disabled = !canSvc || s.service.active === 'active';
  btn('ts-svc-stop').disabled = !canSvc || s.service.active !== 'active';
  setSwitch('ts-svc-boot', s.service.enabled === 'enabled',
    canSvc && (s.service.enabled === 'enabled' || s.service.enabled === 'disabled'));

  // Connessione
  const running = usable && s.service.active === 'active';
  const [label, led] = BACKEND_IT[s.backend] ?? [s.backend || '—', ''];
  if (!usable) {
    setLed('ts-conn-led', '');
    byId('ts-conn-state').textContent = '—';
  } else if (!running) {
    setLed('ts-conn-led', '');
    byId('ts-conn-state').textContent = 'servizio fermo';
  } else {
    setLed('ts-conn-led', led);
    byId('ts-conn-state').textContent = label + (s.upPending ? ' · login in attesa' : '');
  }
  btn('ts-up').disabled = !running || tsBusy || s.backend === 'Running';
  btn('ts-down').disabled = !running || tsBusy || s.backend === 'Stopped';
  const login = btn('ts-login');
  login.hidden = !(running && s.authUrl && s.backend !== 'Running');
  login.dataset.url = s.authUrl || '';

  const health = byId('ts-health');
  health.innerHTML = '';
  for (const h of (running ? s.health || [] : [])) {
    const li = document.createElement('li');
    li.textContent = h;
    health.appendChild(li);
  }

  // Macchine della rete
  const ul = byId('ts-peers');
  ul.innerHTML = '';
  const peers = running ? s.peers || [] : [];
  ul.dataset.empty = !usable ? 'Tailscale non è installato.'
    : !running ? 'Il servizio è fermo.'
      : s.backend !== 'Running' ? 'Connettiti per vedere le macchine della rete.'
        : 'Nessuna altra macchina nella rete.';
  for (const p of peers) {
    const li = document.createElement('li');
    li.className = 'peer';
    const l = document.createElement('span');
    l.className = 'led ' + (p.online ? 'active' : 'down');
    const name = document.createElement('span');
    name.className = 'peer-name';
    name.textContent = p.name;
    name.title = p.dnsName || p.name;
    if (p.self) {
      const b = document.createElement('span');
      b.className = 'badge active';
      b.textContent = 'questo computer';
      name.appendChild(b);
    }
    const ips = document.createElement('span');
    ips.className = 'peer-ips';
    ips.textContent = (p.ips || []).join('  ');
    ips.title = (p.ips || []).join('\n');
    const os = document.createElement('span');
    os.className = 'peer-os';
    os.textContent = (p.os || '') + (p.online ? '' : ' · offline');
    li.append(l, name, ips, os);
    ul.appendChild(li);
  }
  const online = peers.filter((p) => p.online && !p.self).length;
  const others = peers.filter((p) => !p.self).length;
  byId('ts-peers-count').textContent = others ? `${online} in linea su ${others}` : '';
}

async function refreshTailscale(): Promise<void> {
  try {
    ts = (await TailscaleState()) as unknown as TSState;
  } catch (e) {
    ts = null;
    showNotice('ts', 'Stato non leggibile: ' + String(e), '');
    return;
  }
  renderTailscale();
}

async function tsAction(label: string, fn: () => Promise<unknown>): Promise<void> {
  if (tsBusy) return;
  tsBusy = true;
  renderTailscale();
  let res: ActionResult;
  try {
    res = asResult(await fn());
  } catch (e) {
    res = { ok: false, message: String(e), pending: false, authUrl: '', ipBefore: '', ipAfter: '' };
  }
  tsBusy = false;
  if (res.pending) toastFn('Serve il login: apri la pagina indicata', 'ok');
  else toastFn(label + (res.ok ? ': fatto' : ': ' + res.message), res.ok ? 'ok' : 'err');
  await refreshTailscale();
}

function wireTailscale(): void {
  btn('ts-svc-start').addEventListener('click', () => tsAction('Avvio del servizio', () => TailscaleService('start')));
  btn('ts-svc-stop').addEventListener('click', () => tsAction('Arresto del servizio', () => TailscaleService('stop')));
  btn('ts-svc-boot').addEventListener('click', () => {
    const on = btn('ts-svc-boot').getAttribute('aria-checked') === 'true';
    void tsAction('Avvio automatico', () => TailscaleService(on ? 'disable' : 'enable'));
  });
  btn('ts-up').addEventListener('click', () => tsAction('Connessione', () => TailscaleConnection('up')));
  btn('ts-down').addEventListener('click', () => tsAction('Disconnessione', () => TailscaleConnection('down')));
  btn('ts-login').addEventListener('click', () => {
    const url = btn('ts-login').dataset.url;
    if (url) void openURL(url);
  });
  wireNoticeCopy('ts');
}

// ------------------------------------------------------------
//  OpenVPN
// ------------------------------------------------------------
let ov: OVState | null = null;
let ovBusy = false;
let ovSelected = ''; // unità scelta nel menu

function currentTunnel(): Tunnel | null {
  const list = ov?.tunnels || [];
  return list.find((t) => t.unit === ovSelected) || list[0] || null;
}

function renderPicker(list: Tunnel[]): void {
  const box = byId('ov-picker');
  // Il menu non si ridisegna a ogni controllo dei LED: si chiuderebbe in mano
  // all'utente. Si rifà solo se cambia l'elenco o i segni "all'avvio".
  const key = list.map((t) => t.unit + ':' + t.enabled).join('|') + '#' + list.length;
  if (box.dataset.key === key) {
    const sel = box.querySelector('select') as HTMLSelectElement | null;
    if (sel) sel.disabled = ovBusy;
    return;
  }
  box.dataset.key = key;
  box.innerHTML = '';
  if (list.length === 0) {
    const p = document.createElement('p');
    p.className = 'tunnel-empty';
    p.innerHTML = 'Nessun tunnel configurato. Per aggiungerne uno copia il file del provider in ' +
      '<code>/etc/openvpn/client/</code> con estensione <code>.conf</code>, per esempio:<br>' +
      '<code>sudo cp ufficio.ovpn /etc/openvpn/client/ufficio.conf</code><br>poi premi Aggiorna.';
    box.appendChild(p);
    return;
  }
  if (list.length === 1) {
    const span = document.createElement('span');
    span.className = 'tunnel-name';
    span.textContent = list[0].name + (list[0].legacy ? ' (openvpn@)' : '');
    box.appendChild(span);
    return;
  }
  const sel = document.createElement('select');
  sel.className = 'tunnel-select';
  for (const t of list) {
    const o = document.createElement('option');
    o.value = t.unit;
    o.textContent = (t.enabled === 'enabled' ? '● ' : '') + t.name +
      (t.legacy ? ' (openvpn@)' : '') + (t.enabled === 'enabled' ? ' — all\'avvio' : '');
    sel.appendChild(o);
  }
  sel.value = currentTunnel()?.unit || list[0].unit;
  sel.disabled = ovBusy;
  sel.addEventListener('change', () => { ovSelected = sel.value; renderOpenVPN(); });
  box.appendChild(sel);
}

function renderOpenVPN(): void {
  const s = ov;
  if (!s) return;
  const usable = s.supported && s.installed;
  showNotice('ov', s.message, s.installHint);
  const list = usable ? s.tunnels || [] : [];
  if (!list.some((t) => t.unit === ovSelected)) ovSelected = list[0]?.unit || '';
  renderPicker(list);

  const t = currentTunnel();
  if (!usable) {
    setLed('ov-led', '');
    byId('ov-state').textContent = s.supported ? 'OpenVPN non installato' : 'non supportato';
  } else if (!t) {
    setLed('ov-led', '');
    byId('ov-state').textContent = 'nessun tunnel';
  } else {
    setLed('ov-led', unitLed(t.active));
    const en = ENABLED_IT[t.enabled] ?? t.enabled;
    byId('ov-state').textContent = (t.active === 'active' ? 'acceso' : t.active === 'inactive' ? 'spento'
      : ACTIVE_IT[t.active] ?? t.active) + (en ? ' · ' + en : '');
  }
  byId('ov-unit').textContent = t ? t.unit : 'openvpn-client@<nome>';
  const can = usable && !!t && s.hasPkexec && !ovBusy;
  btn('ov-start').disabled = !can || t?.active === 'active';
  btn('ov-stop').disabled = !can || t?.active === 'inactive';
  setSwitch('ov-boot', t?.enabled === 'enabled', can && (t?.enabled === 'enabled' || t?.enabled === 'disabled'));
  btn('ov-refresh').disabled = ovBusy;
  btn('ov-ip-now').disabled = ovBusy || !s.supported;
  btn('ov-search-root').hidden = !(usable && s.unreadable && !s.searchedRoot);
  btn('ov-search-root').disabled = ovBusy || !s.hasPkexec;

  const notes = byId('ov-notes');
  notes.innerHTML = '';
  for (const n of s.notes || []) {
    const li = document.createElement('li');
    li.textContent = n;
    notes.appendChild(li);
  }
}

async function refreshOpenVPN(): Promise<void> {
  try {
    ov = (await OpenVPNState()) as unknown as OVState;
  } catch (e) {
    ov = null;
    showNotice('ov', 'Stato non leggibile: ' + String(e), '');
    return;
  }
  renderOpenVPN();
}

async function ovAction(label: string, fn: () => Promise<unknown>): Promise<void> {
  if (ovBusy) return;
  ovBusy = true;
  renderOpenVPN();
  let res: ActionResult;
  try {
    res = asResult(await fn());
  } catch (e) {
    res = { ok: false, message: String(e), pending: false, authUrl: '', ipBefore: '', ipAfter: '' };
  }
  ovBusy = false;
  if (res.ipBefore) byId('ov-ip-before').textContent = res.ipBefore;
  if (res.ipAfter) byId('ov-ip-after').textContent = res.ipAfter;
  toastFn(label + (res.ok ? ': fatto' : ': ' + res.message), res.ok ? 'ok' : 'err');
  await refreshOpenVPN();
}

function wireOpenVPN(): void {
  const unit = () => currentTunnel()?.unit || '';
  btn('ov-start').addEventListener('click', () => {
    byId('ov-ip-before').textContent = '…';
    byId('ov-ip-after').textContent = '—';
    void ovAction('Accensione', () => OpenVPNTunnel(unit(), 'start'));
  });
  btn('ov-stop').addEventListener('click', () => {
    byId('ov-ip-before').textContent = '…';
    byId('ov-ip-after').textContent = '—';
    void ovAction('Spegnimento', () => OpenVPNTunnel(unit(), 'stop'));
  });
  btn('ov-boot').addEventListener('click', () => {
    const on = btn('ov-boot').getAttribute('aria-checked') === 'true';
    void ovAction('Avvio automatico', () => OpenVPNTunnel(unit(), on ? 'disable' : 'enable'));
  });
  btn('ov-refresh').addEventListener('click', () => { void refreshOpenVPN(); });
  btn('ov-search-root').addEventListener('click', () => ovAction('Ricerca', () => OpenVPNSearchPrivileged()));
  btn('ov-ip-now').addEventListener('click', async () => {
    if (ovBusy) return;
    ovBusy = true;
    renderOpenVPN();
    try {
      const r = asResult(await PublicIP('openvpn'));
      byId('ov-ip-after').textContent = r.ok ? r.ipAfter : 'non letto';
    } catch { /* l'errore è già in console */ }
    ovBusy = false;
    renderOpenVPN();
  });
  wireNoticeCopy('ov');
}

// ------------------------------------------------------------
//  Controllo periodico: solo per il toolkit visibile
// ------------------------------------------------------------
let pollTimer = 0;
let pollView = '';

async function pollOnce(view: string): Promise<void> {
  if (view === 'tailscale-view') {
    if (!tsBusy) await refreshTailscale();
  } else if (view === 'openvpn-view') {
    if (!ovBusy) await refreshOpenVPN();
  }
}

function schedule(view: string): void {
  window.clearTimeout(pollTimer);
  pollTimer = window.setTimeout(async () => {
    if (pollView !== view) return;
    // La finestra nascosta o ridotta a icona non ha bisogno dei LED.
    if (!document.hidden) await pollOnce(view);
    if (pollView === view) schedule(view);
  }, POLL_MS);
}

/** Da chiamare a ogni cambio di toolkit: avvia o ferma il controllo dei LED. */
export function vpnViewChanged(view: string): void {
  window.clearTimeout(pollTimer);
  pollView = '';
  if (view !== 'tailscale-view' && view !== 'openvpn-view') return;
  pollView = view;
  void pollOnce(view).then(() => { if (pollView === view) schedule(view); });
}

/** Mostra i toolkit se il backend li dichiara disponibili (solo Linux). */
export async function initVPN(toast: Toast): Promise<void> {
  toastFn = toast;
  let avail = { tailscale: false, openvpn: false };
  try {
    avail = (await VPNToolkits()) as unknown as typeof avail;
  } catch {
    return; // fuori da Wails (prova statica del frontend) o binding assente
  }
  byId('nav-tailscale').hidden = !avail.tailscale;
  byId('nav-openvpn').hidden = !avail.openvpn;
  if (!avail.tailscale && !avail.openvpn) return;

  wireConsoleButtons();
  if (avail.tailscale) wireTailscale();
  if (avail.openvpn) wireOpenVPN();
  // Binding Wails: la chiamata stessa può lanciare in modo sincrono
  // (PROBLEMI-NOTI.md §8bis).
  try {
    EventsOn('vpn:console', (l: ConsoleLine) => {
      if (!l) return;
      appendConsole(l.toolkit === 'tailscale' ? 'ts-console' : 'ov-console', l);
    });
  } catch { /* fuori da Wails */ }
}
