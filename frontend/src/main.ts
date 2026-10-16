// SPDX-License-Identifier: MIT

// ============================================================
//  NetworkToolkit — Renderer (frontend)
// ============================================================
// Il CSS entra nella build SOLO se importato qui: spostandolo in src/ senza
// questo import l'app si aprirebbe senza stili (trappola nota dai progetti
// gemelli). Vedi PROBLEMI-NOTI.md §3.7.
import './style.css';

// La fascia trascinabile in cima (#drag-region) e il suo spazio servono solo
// su macOS, dove la barra del titolo è nascosta (TitleBarHiddenInset). Su
// Windows e Linux c'è la barra nativa e restava una striscia vuota.
document.documentElement.dataset.os = navigator.platform.toLowerCase().includes('mac')
  ? 'darwin' : navigator.platform.toLowerCase().includes('win') ? 'windows' : 'linux';

import {
  GetVersion,
  GetCapabilities,
  ListInterfaces,
  SwitchTemporary,
  SwitchPermanent,
  Reset,
  StartSpeedtest,
  StopSpeedtest,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime';
import { initVPN, vpnViewChanged } from './vpn';

// --- Tipi lato UI (specchio del JSON restituito dal Go) ---
interface UIface {
  id: string;
  name: string;
  displayName: string;
  kind: string;
  currentMac: string;
  permanentMac: string;
  isUp: boolean;
  isActive: boolean;
  spoofed: boolean;
  canReset: boolean;
}
interface Caps { os: string; canTemporary: boolean; canPermanent: boolean; }
interface OpResult {
  ok: boolean;
  log: string[];
  results: { id: string; ok: boolean; currentMac: string; message: string }[];
  interfaces: UIface[];
}

const byId = (id: string) => document.getElementById(id) as HTMLElement;

let ifaces: UIface[] = [];
let caps: Caps = { os: '', canTemporary: true, canPermanent: true };
const selected = new Set<string>();
let firstLoad = true;
let busy = false;

// --- Console ---
function logLine(text: string, cls = ''): void {
  const con = byId('console');
  const span = document.createElement('span');
  if (cls) span.className = cls;
  span.textContent = text + '\n';
  con.appendChild(span);
  con.scrollTop = con.scrollHeight;
}
function logFromBackend(line: string): void {
  let cls = '';
  if (/ERRORE|FALLIT|rifiutat/i.test(line)) cls = 'err';
  else if (/\bOK\b/.test(line)) cls = 'ok';
  else if (/ATTENZIONE|NOTA/i.test(line)) cls = 'warn';
  else cls = 'dim';
  logLine(line, cls);
}
function stamp(): string {
  return new Date().toLocaleTimeString('it-IT');
}

// --- Toast ---
let toastTimer: number | undefined;
function toast(msg: string, kind: 'ok' | 'err'): void {
  const t = byId('toast');
  t.textContent = msg;
  t.className = 'show ' + kind;
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (t.className = kind), 3200);
}

// --- Rendering schede di rete ---
function ledClass(it: UIface): string {
  if (it.isActive) return 'led active';
  if (it.spoofed || (it.permanentMac && it.permanentMac !== it.currentMac)) return 'led spoofed';
  if (it.isUp) return 'led up';
  return 'led down';
}

function ifaceIcon(kind: string): string {
  if (kind === 'wifi') {
    return `<svg class="iface-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.55a11 11 0 0 1 14 0"/><path d="M1.42 9a16 16 0 0 1 21.16 0"/><path d="M8.53 16.11a6 6 0 0 1 6.95 0"/><line x1="12" y1="20" x2="12.01" y2="20"/></svg>`;
  }
  return `<svg class="iface-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="7" width="20" height="10" rx="2"/><path d="M6 11h.01M10 11h.01M14 11h.01"/></svg>`;
}

function renderInterfaces(): void {
  const ul = byId('iface-list');
  ul.innerHTML = '';
  for (const it of ifaces) {
    const li = document.createElement('li');
    li.className = 'iface' + (selected.has(it.id) ? ' selected' : '');
    li.dataset.id = it.id;

    const changed = it.permanentMac && it.permanentMac !== it.currentMac;
    const badges =
      (it.isActive ? '<span class="badge active">attiva</span>' : '') +
      (it.kind === 'wifi' ? '<span class="badge wifi">Wi-Fi</span>' : '');
    const permLine = it.permanentMac
      ? `<span class="perm"> · fabbrica ${it.permanentMac}</span>`
      : '';

    li.innerHTML = `
      <span class="${ledClass(it)}"></span>
      ${ifaceIcon(it.kind)}
      <span class="iface-main">
        <span class="iface-name">${escapeHtml(it.displayName || it.name)} ${badges}</span>
        <span class="iface-sub">${escapeHtml(it.name)}${it.isUp ? '' : ' · spenta'}</span>
        <span class="iface-macs"><span class="cur ${changed ? 'changed' : ''}">${it.currentMac}</span>${permLine}</span>
      </span>
      <span class="checkbox"></span>`;

    li.addEventListener('click', () => {
      if (busy) return;
      if (selected.has(it.id)) selected.delete(it.id);
      else selected.add(it.id);
      renderInterfaces();
      refreshButtons();
    });
    ul.appendChild(li);
  }
}

function refreshButtons(): void {
  const has = selected.size > 0;
  const anyResettable = ifaces.some((it) => selected.has(it.id) && it.canReset);
  (byId('switch-temp-btn') as HTMLButtonElement).disabled = busy || !has || !caps.canTemporary;
  (byId('switch-perm-btn') as HTMLButtonElement).disabled = busy || !has || !caps.canPermanent;
  (byId('reset-btn') as HTMLButtonElement).disabled = busy || !anyResettable;

  const temp = byId('switch-temp-btn');
  temp.hidden = !caps.canTemporary;

  let note = '';
  if (!caps.canTemporary) {
    note = 'Su Windows il cambio del MAC è sempre permanente (scrittura nel registro): il cambio temporaneo non esiste.';
  } else {
    note = 'Il cambio temporaneo si annulla al riavvio; quello permanente resta finché non premi Ripristina.';
  }
  byId('os-note').textContent = note;
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"]/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c] as string));
}

// --- Dati schede ---
async function loadInterfaces(): Promise<void> {
  try {
    const list = (await ListInterfaces()) as unknown as UIface[];
    ifaces = list || [];
  } catch (e) {
    ifaces = [];
    logLine('Impossibile leggere le schede di rete: ' + String(e), 'err');
    return;
  }
  if (firstLoad) {
    for (const it of ifaces) if (it.isActive) selected.add(it.id);
    firstLoad = false;
  } else {
    for (const id of Array.from(selected)) {
      if (!ifaces.some((it) => it.id === id)) selected.delete(id);
    }
  }
  renderInterfaces();
  refreshButtons();
}

function setBusy(on: boolean): void {
  busy = on;
  document.body.classList.toggle('busy', on);
  refreshButtons();
}

async function runOp(
  kind: 'temp' | 'perm' | 'reset',
  fn: (ids: string[]) => Promise<unknown>,
): Promise<void> {
  if (busy || selected.size === 0) return;
  const ids = Array.from(selected);
  const label = kind === 'reset' ? 'Ripristino' : kind === 'perm' ? 'Switch permanente' : 'Switch temporaneo';

  setBusy(true);
  logLine(`— ${stamp()} · ${label} su ${ids.length} scheda/e —`, 'dim');
  logLine('Conferma la password nella finestra di sistema…', 'dim');

  let res: OpResult;
  try {
    res = (await fn(ids)) as unknown as OpResult;
  } catch (e) {
    logLine('Operazione fallita: ' + String(e), 'err');
    toast('Operazione fallita', 'err');
    setBusy(false);
    await loadInterfaces();
    return;
  }

  for (const line of res.log || []) logFromBackend(line);
  if (res.interfaces) {
    ifaces = res.interfaces;
    for (const id of Array.from(selected)) {
      if (!ifaces.some((it) => it.id === id)) selected.delete(id);
    }
    renderInterfaces();
  }
  if (res.ok) {
    toast(label + ' completato', 'ok');
  } else {
    toast(label + ': controlla la console', 'err');
  }
  setBusy(false);
}

// ============================================================
//  Toolkit velocità — quadranti
// ============================================================
// Questo blocco non sa NULLA di rete: riceve un numero al secondo e lo
// trasforma in movimento. È il "livello 2" dell'architettura a due livelli.

const SVG_NS = 'http://www.w3.org/2000/svg';
const GAUGE_START = 135;   // gradi: parte in basso a sinistra…
const GAUGE_SWEEP = 270;   // …e spazza 270° fino in basso a destra
// Scale a gradini: il fondo-scala si allarga quando il valore lo supera, e
// non torna più indietro durante la stessa misura (un fondo-scala che
// rimbalza avanti e indietro rende il quadrante illeggibile).
const SCALES = [10, 25, 50, 100, 250, 500, 1000, 2500, 10000];

function polar(cx: number, cy: number, r: number, deg: number): { x: number; y: number } {
  const rad = (deg * Math.PI) / 180;
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

function arcPath(cx: number, cy: number, r: number, a0: number, a1: number): string {
  const p0 = polar(cx, cy, r, a0);
  const p1 = polar(cx, cy, r, a1);
  const large = Math.abs(a1 - a0) > 180 ? 1 : 0;
  return `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)} A ${r} ${r} 0 ${large} 1 ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`;
}

function svgEl(tag: string, attrs: Record<string, string | number> = {}): SVGElement {
  const el = document.createElementNS(SVG_NS, tag);
  for (const k of Object.keys(attrs)) el.setAttribute(k, String(attrs[k]));
  return el;
}

function fmtMbps(v: number): string {
  if (v >= 100) return String(Math.round(v));
  if (v >= 10) return v.toFixed(1);
  return v.toFixed(2);
}

class Gauge {
  private readonly cx = 100;
  private readonly cy = 98;
  private readonly r = 74;
  private root: HTMLElement;
  private valueArc: SVGElement;
  private needleGroup: SVGElement;
  private valueText: SVGElement;
  private ticks: SVGElement;
  private arcLen = 1;
  private scale = SCALES[0];
  private shown = 0;
  private raf = 0;
  private settle = 0;

  constructor(root: HTMLElement, id: string) {
    this.root = root;
    const svg = svgEl('svg', { viewBox: '0 0 200 178' });

    // Gradiente proprio per ogni quadrante: id unici, niente collisioni.
    const defs = svgEl('defs');
    const grad = svgEl('linearGradient', { id: `gaugeGrad-${id}`, x1: 0, y1: 0, x2: 1, y2: 1 });
    grad.appendChild(svgEl('stop', { offset: '0', class: 'g-grad-a' }));
    grad.appendChild(svgEl('stop', { offset: '1', class: 'g-grad-b' }));
    defs.appendChild(grad);
    svg.appendChild(defs);

    const full = arcPath(this.cx, this.cy, this.r, GAUGE_START, GAUGE_START + GAUGE_SWEEP);
    svg.appendChild(svgEl('path', {
      d: full, class: 'g-track', fill: 'none', 'stroke-width': 10, 'stroke-linecap': 'round',
    }));

    this.valueArc = svgEl('path', {
      d: full, class: 'g-value', fill: 'none', 'stroke-width': 10, 'stroke-linecap': 'round',
      stroke: `url(#gaugeGrad-${id})`,
    });
    svg.appendChild(this.valueArc);

    this.ticks = svgEl('g');
    svg.appendChild(this.ticks);

    // Numero e unità vanno nell'APERTURA in basso del quadrante (i 90° che
    // l'arco non copre): al centro l'ago e il mozzo ci passerebbero sopra
    // tagliando le cifre, e a quel punto il valore non si legge più.
    this.valueText = svgEl('text', { x: this.cx, y: this.cy + 50, class: 'g-value-text' });
    this.valueText.textContent = '0.00';
    svg.appendChild(this.valueText);

    const unit = svgEl('text', { x: this.cx, y: this.cy + 66, class: 'g-unit' });
    unit.textContent = 'Mbps';
    svg.appendChild(unit);

    // Ago: disegnato fermo sulla posizione di partenza, poi ruotato.
    this.needleGroup = svgEl('g', { class: 'g-needle-group' });
    const tip = polar(this.cx, this.cy, 62, GAUGE_START);
    const left = polar(this.cx, this.cy, 6, GAUGE_START + 90);
    const right = polar(this.cx, this.cy, 6, GAUGE_START - 90);
    const tail = polar(this.cx, this.cy, 13, GAUGE_START + 180);
    this.needleGroup.appendChild(svgEl('polygon', {
      class: 'g-needle',
      points: `${tip.x.toFixed(1)},${tip.y.toFixed(1)} ${left.x.toFixed(1)},${left.y.toFixed(1)} ${tail.x.toFixed(1)},${tail.y.toFixed(1)} ${right.x.toFixed(1)},${right.y.toFixed(1)}`,
    }));
    svg.appendChild(this.needleGroup);
    svg.appendChild(svgEl('circle', { cx: this.cx, cy: this.cy, r: 7, class: 'g-hub' }));

    root.appendChild(svg);

    // La lunghezza vera dell'arco serve per far avanzare il tratto colorato
    // con stroke-dashoffset (animato dal CSS, non a mano).
    this.arcLen = (this.valueArc as SVGPathElement).getTotalLength() || 1;
    this.valueArc.setAttribute('stroke-dasharray', String(this.arcLen));
    this.valueArc.setAttribute('stroke-dashoffset', String(this.arcLen));

    this.drawTicks();
    this.applyNeedle(0);
  }

  private drawTicks(): void {
    this.ticks.innerHTML = '';
    for (let i = 0; i <= 10; i++) {
      const major = i % 2 === 0;
      const a = GAUGE_START + (GAUGE_SWEEP * i) / 10;
      const outer = polar(this.cx, this.cy, this.r - 8, a);
      const inner = polar(this.cx, this.cy, this.r - (major ? 16 : 12), a);
      this.ticks.appendChild(svgEl('line', {
        x1: outer.x.toFixed(1), y1: outer.y.toFixed(1),
        x2: inner.x.toFixed(1), y2: inner.y.toFixed(1),
        class: 'g-tick' + (major ? ' major' : ''), 'stroke-width': major ? 2 : 1.2,
        'stroke-linecap': 'round',
      }));
      if (major) {
        // Etichette tenute vicine alla corona: più verso il centro entrerebbero
        // in collisione col numero grande quando il fondo-scala ha tre cifre.
        const lp = polar(this.cx, this.cy, this.r - 21, a);
        const label = svgEl('text', { x: lp.x.toFixed(1), y: lp.y.toFixed(1), class: 'g-ticklabel' });
        const v = (this.scale * i) / 10;
        label.textContent = v >= 1000 ? `${Math.round(v / 1000)}k` : String(Math.round(v));
        this.ticks.appendChild(label);
      }
    }
  }

  private applyNeedle(frac: number): void {
    this.needleGroup.setAttribute('transform', `rotate(${(frac * GAUGE_SWEEP).toFixed(2)} ${this.cx} ${this.cy})`);
    this.valueArc.setAttribute('stroke-dashoffset', String(this.arcLen * (1 - frac)));
  }

  /** Imposta il valore in Mbps: allarga il fondo-scala se serve e anima. */
  setValue(v: number): void {
    const value = Number.isFinite(v) && v > 0 ? v : 0;
    // Fondo-scala dinamico: si allarga (con un 15% di margine) ma non si
    // restringe, così l'ago non cambia significato a metà misura.
    while (value > this.scale * 0.98 && this.scale < SCALES[SCALES.length - 1]) {
      const next = SCALES[SCALES.indexOf(this.scale) + 1];
      if (next === undefined) break;
      this.scale = next;
      this.drawTicks();
    }
    this.applyNeedle(Math.min(value / this.scale, 1));
    this.tweenNumber(value);
  }

  /** Il numero al centro insegue il valore invece di saltarci sopra, in
   *  sincrono con l'ago. */
  private tweenNumber(to: number, dur = 550): void {
    if (this.raf) cancelAnimationFrame(this.raf);
    if (this.settle) window.clearTimeout(this.settle);
    const from = this.shown;
    const t0 = performance.now();
    const step = (now: number) => {
      const p = Math.min((now - t0) / dur, 1);
      const eased = 1 - Math.pow(1 - p, 3);
      this.shown = from + (to - from) * eased;
      this.valueText.textContent = fmtMbps(this.shown);
      if (p < 1) this.raf = requestAnimationFrame(step);
      else this.raf = 0;
    };
    this.raf = requestAnimationFrame(step);
    // Rete di sicurezza: requestAnimationFrame viene strozzato o sospeso dal
    // browser (finestra in secondo piano, cattura schermo, risparmio
    // energetico) e il tween può fermarsi a metà, lasciando a video un numero
    // che non è mai esistito. Il valore finale non deve dipendere dal fatto
    // che l'animazione arrivi in fondo.
    this.settle = window.setTimeout(() => {
      if (this.raf) cancelAnimationFrame(this.raf);
      this.raf = 0;
      this.shown = to;
      this.valueText.textContent = fmtMbps(to);
    }, dur + 120);
  }

  /** Riporta a zero e al fondo-scala iniziale (inizio di una nuova misura). */
  reset(): void {
    this.scale = SCALES[0];
    this.drawTicks();
    this.setValue(0);
  }

  /** "Power-on" scenico: l'ago spazza a fondo scala e torna a zero. Puramente
   *  estetico, come su uno strumento vero che si accende.
   *  NON passa da setValue: quello allargherebbe il fondo-scala (un valore
   *  pari al fondo lo fa scattare al gradino successivo), e la spazzata
   *  lascerebbe il quadrante su una scala sbagliata prima ancora di misurare. */
  powerOn(): void {
    this.root.classList.add('powering');
    this.applyNeedle(1);
    this.tweenNumber(this.scale, 900);
    window.setTimeout(() => {
      this.applyNeedle(0);
      this.tweenNumber(0, 900);
      window.setTimeout(() => this.root.classList.remove('powering'), 950);
    }, 950);
  }
}

// --- Stato del toolkit velocità ---
let gaugeDown: Gauge | null = null;
let gaugeUp: Gauge | null = null;
let speedRunning = false;
let speedPoweredOn = false;

function setSpeedStatus(msg: string, kind: '' | 'running' | 'error' = ''): void {
  const el = byId('speed-status');
  el.textContent = msg;
  el.className = 'speed-status' + (kind ? ' ' + kind : '');
}

function setSpeedButton(label: string): void {
  byId('speed-toggle-btn').textContent = label;
}

async function startSpeed(): Promise<void> {
  if (speedRunning) return;
  gaugeDown?.reset();
  gaugeUp?.reset();
  setSpeedStatus('Avvio del motore di misura…');
  try {
    await StartSpeedtest();
  } catch (e) {
    setSpeedStatus('Avvio fallito: ' + String(e), 'error');
    return;
  }
  speedRunning = true;
  setSpeedButton('Ferma test');
  setSpeedStatus('Test in corso — il primo secondo è più basso: la connessione sta accelerando.', 'running');
}

async function stopSpeed(message = 'Test fermato.'): Promise<void> {
  if (!speedRunning) return;
  speedRunning = false;
  setSpeedButton('Avvia test');
  try {
    await StopSpeedtest();
  } catch {
    /* già morto: va bene così */
  }
  gaugeDown?.setValue(0);
  gaugeUp?.setValue(0);
  setSpeedStatus(message);
}

function wireSpeedtest(): void {
  gaugeDown = new Gauge(byId('gauge-down'), 'down');
  gaugeUp = new Gauge(byId('gauge-up'), 'up');

  byId('speed-toggle-btn').addEventListener('click', () => {
    if (speedRunning) void stopSpeed();
    else void startSpeed();
  });

  // I binding Wails vanno avvolti in try/catch anche nella CHIAMATA, non solo
  // sulla promise: fuori da un runtime Wails lanciano in modo sincrono e
  // interromperebbero il modulo (PROBLEMI-NOTI.md §8bis).
  try {
    EventsOn('speedtest:sample', (s: any) => {
      if (!speedRunning) return;
      gaugeDown?.setValue(Number(s?.downMbps) || 0);
      gaugeUp?.setValue(Number(s?.upMbps) || 0);
    });
    EventsOn('speedtest:error', (m: any) => {
      setSpeedStatus(String(m ?? 'errore sconosciuto'), 'error');
    });
    EventsOn('speedtest:stopped', () => {
      speedRunning = false;
      setSpeedButton('Avvia test');
      gaugeDown?.setValue(0);
      gaugeUp?.setValue(0);
    });
  } catch {
    /* fuori da Wails (prova statica del frontend): nessun evento */
  }
}

// --- Navigazione fra toolkit ---
function wireNav(): void {
  const navBtns = Array.from(document.querySelectorAll('.nav-btn')) as HTMLElement[];
  const views = Array.from(document.querySelectorAll('.view')) as HTMLElement[];

  for (const btn of navBtns) {
    btn.addEventListener('click', () => {
      const target = btn.dataset.target;
      if (!target) return;

      // Uscendo dal toolkit velocità si ferma la misura: un test che continua
      // a macinare traffico mentre guardi un altro strumento è traffico
      // nascosto, e questa app esiste per mostrare cosa passa dalla rete.
      if (target !== 'speed-view' && speedRunning) {
        void stopSpeed('Test fermato: hai lasciato il toolkit velocità.');
      }

      for (const b of navBtns) b.classList.remove('active');
      for (const v of views) v.classList.remove('active');
      btn.classList.add('active');
      byId(target).classList.add('active');

      // I LED dei toolkit VPN si leggono solo mentre il toolkit è visibile.
      vpnViewChanged(target);

      if (target === 'speed-view' && !speedPoweredOn) {
        speedPoweredOn = true;
        gaugeDown?.powerOn();
        gaugeUp?.powerOn();
      }
    });
  }
}

// --- Avvio ---
function wire(): void {
  byId('theme-toggle').addEventListener('click', () => {
    const cur = document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
    const next = cur === 'light' ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', next);
    try { localStorage.setItem('theme', next); } catch { /* private mode */ }
  });

  byId('refresh-btn').addEventListener('click', () => { if (!busy) loadInterfaces(); });
  byId('clear-console-btn').addEventListener('click', () => { byId('console').innerHTML = ''; });

  byId('switch-temp-btn').addEventListener('click', () => runOp('temp', SwitchTemporary));
  byId('switch-perm-btn').addEventListener('click', () => runOp('perm', SwitchPermanent));
  byId('reset-btn').addEventListener('click', () => runOp('reset', Reset));

  wireNav();
  wireSpeedtest();
}

async function init(): Promise<void> {
  wire();
  try {
    const v = await GetVersion();
    byId('version-badge').textContent = 'v' + v;
  } catch { /* fuori da Wails (test statico): nessun binding */ }
  try {
    caps = (await GetCapabilities()) as unknown as Caps;
  } catch { /* default già impostati */ }
  await initVPN(toast);
  await loadInterfaces();
  logLine('Pronto. Seleziona una scheda e premi Switcha.', 'dim');
}

window.addEventListener('DOMContentLoaded', init);
