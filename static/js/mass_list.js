/* ============================================================
   MassSpectraWorker — Mass List frontend
   API contract preserved: POST /api/plot  (JSON body)
   ============================================================ */

'use strict';

const API_URL = '/api/plot';
const HISTORY_KEY = 'massListHistory';
const MAX_HISTORY = 10;

/* ---------- DOM ---------- */
const $ = (id) => document.getElementById(id);

const form         = $('mass-list-form');
const submitBtn    = $('submit-btn');
const submitLabel  = $('submit-label');

const statusEl     = $('status');
const statusText   = $('status-text');

const resultMeta   = $('result-meta');
const resultTools  = $('result-tools');

const stateEmpty   = $('state-empty');
const stateLoading = $('state-loading');
const stateImage   = $('state-image');
const stateError   = $('state-error');
const plotImage    = $('plot-image');
const errorMsg     = $('error-msg');
const errorHint    = $('error-hint');

const downloadBtn  = $('download-btn');
const toggleJson   = $('toggle-json');
const jsonDrawer   = $('json-drawer');
const jsonContent  = $('json-content');
const copyJsonBtn  = $('copy-json');

const historyList  = $('history-list');
const historyEmpty = $('history-empty');
const clearHistory = $('clear-history');

const toast        = $('toast');
const toastText    = $('toast-text');

/* Holds the current blob URL so we can revoke + download */
let currentObjectUrl = null;
let lastFormat = 'png';
let lastName = 'spectrum';

/* ============================================================
   Status pill
   ============================================================ */
const STATUS_LABELS = {
  ready:   'Готов',
  loading: 'Обработка',
  success: 'Готово',
  error:   'Ошибка',
};
function setStatus(state, text) {
  statusEl.dataset.state = state;
  statusText.textContent = text || STATUS_LABELS[state] || state;
}

/* ============================================================
   Viewport state machine
   ============================================================ */
function showState(name) {
  stateEmpty.classList.toggle('hidden', name !== 'empty');
  stateLoading.classList.toggle('hidden', name !== 'loading');
  stateImage.classList.toggle('hidden', name !== 'image');
  stateError.classList.toggle('hidden', name !== 'error');
}

function showLoading() {
  showState('loading');
  resultTools.classList.add('hidden');
  jsonDrawer.classList.add('hidden');
  resultMeta.textContent = '';
  submitBtn.disabled = true;
  submitLabel.textContent = 'Обработка…';
  setStatus('loading');
}

function endLoading() {
  submitBtn.disabled = false;
  submitLabel.textContent = 'Сгенерировать масс-лист';
}

/* ============================================================
   Collect form data  (payload shape MUST match Go backend)
   ============================================================ */
function collectFormData() {
  let bruttoDict = {};
  try {
    const parsed = JSON.parse($('brutto-dict').value);
    for (const [key, value] of Object.entries(parsed)) {
      if (Array.isArray(value) && value.length >= 2) {
        bruttoDict[key] = { min: value[0], max: value[1] };
      } else if (value && typeof value === 'object' && value.min !== undefined) {
        bruttoDict[key] = { min: value.min, max: value.max };
      }
    }
  } catch (e) {
    console.warn('Не удалось разобрать brutto_dict:', e);
    bruttoDict = {};
  }

  return {
    spectra_name:    $('spectra-name').value.trim(),
    spectra_path:    $('spectra-path').value.trim(),
    low_percentile:  parseFloat($('low-percentile').value)  || 99.4,
    high_percentile: parseFloat($('high-percentile').value) || 99.95,
    rel_error:       parseFloat($('rel-error').value)        || 0.5,
    charge_max:      parseInt($('charge-max').value)         || 1,
    brutto_dict:     bruttoDict,
    protocole:       $('protocol').value,
    width:           parseInt($('width').value)  || 10,
    height:          parseInt($('height').value) || 6,
    dpi:             parseInt($('dpi').value)     || 100,
    format:          $('format').value,
    options:         {},
  };
}

/* ============================================================
   Submit
   ============================================================ */
form.addEventListener('submit', async (e) => {
  e.preventDefault();

  const requestData = collectFormData();
  if (!requestData.spectra_name) {
    showError('Укажите имя спектра', 'Поле «Имя спектра» обязательно.');
    return;
  }

  lastFormat = requestData.format;
  lastName = requestData.spectra_name;
  showLoading();

  let response;
  try {
    response = await fetch(API_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(requestData),
    });

    if (!response.ok) {
      const errText = await response.text();
      throw new Error(errText || `HTTP ${response.status}`);
    }

    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) {
      const jsonData = await response.json();
      showJsonResponse(jsonData, requestData);
    } else {
      const blob = await response.blob();
      showImageResponse(blob, requestData);
    }

    addToHistory(requestData, true);
  } catch (err) {
    console.error(err);
    showError('Не удалось построить график', err.message);
    if (response) addToHistory(requestData, false);
  } finally {
    endLoading();
  }
});

/* ============================================================
   Render results
   ============================================================ */
function showImageResponse(blob, req) {
  if (currentObjectUrl) URL.revokeObjectURL(currentObjectUrl);
  currentObjectUrl = URL.createObjectURL(blob);

  plotImage.src = currentObjectUrl;
  showState('image');
  resultTools.classList.remove('hidden');
  jsonDrawer.classList.add('hidden');

  const sizeKB = (blob.size / 1024).toFixed(1);
  const time = new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  resultMeta.textContent = `${req.format.toUpperCase()} · ${sizeKB} KB · ${time}`;

  jsonContent.textContent = JSON.stringify(
    { format: req.format, size_bytes: blob.size, spectra_name: req.spectra_name, request: req },
    null, 2
  );

  setStatus('success');
}

function showJsonResponse(data, req) {
  jsonContent.textContent = JSON.stringify(data, null, 2);

  // If server returned an inline base64 image, render it.
  if (data.image_data) {
    if (currentObjectUrl) { URL.revokeObjectURL(currentObjectUrl); currentObjectUrl = null; }
    plotImage.src = `data:${data.mime_type || 'image/png'};base64,${data.image_data}`;
    showState('image');
    resultTools.classList.remove('hidden');
  } else {
    // No image payload — surface the JSON directly.
    showState('empty');
    resultTools.classList.remove('hidden');
    jsonDrawer.classList.remove('hidden');
  }

  const parts = [];
  if (data.format) parts.push(String(data.format).toUpperCase());
  if (data.size_bytes) parts.push(`${(data.size_bytes / 1024).toFixed(1)} KB`);
  parts.push(new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' }));
  resultMeta.textContent = parts.join(' · ');

  setStatus('success');
}

function showError(msg, hint) {
  errorMsg.textContent = msg || 'Ошибка';
  errorHint.textContent = hint || '';
  showState('error');
  resultTools.classList.add('hidden');
  jsonDrawer.classList.add('hidden');
  resultMeta.textContent = '';
  setStatus('error');
}

/* ============================================================
   Toolbar actions
   ============================================================ */
downloadBtn.addEventListener('click', () => {
  const href = plotImage.src;
  if (!href) return;
  const link = document.createElement('a');
  link.href = href;
  link.download = `mass_list_${lastName || 'spectrum'}_${Date.now()}.${lastFormat}`;
  document.body.appendChild(link);
  link.click();
  link.remove();
});

toggleJson.addEventListener('click', () => {
  jsonDrawer.classList.toggle('hidden');
});

copyJsonBtn.addEventListener('click', () => {
  const text = jsonContent.textContent;
  navigator.clipboard.writeText(text)
    .then(() => showToast('JSON скопирован'))
    .catch(() => {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand('copy'); showToast('JSON скопирован'); }
      finally { ta.remove(); }
    });
});

$('error-retry').addEventListener('click', () => {
  form.requestSubmit();
});

/* ============================================================
   Toast
   ============================================================ */
let toastTimer = null;
function showToast(text) {
  toastText.textContent = text;
  toast.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove('show'), 1800);
}

/* ============================================================
   History (localStorage)
   ============================================================ */
function getHistory() {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY)) || []; }
  catch { return []; }
}

function addToHistory(req, ok) {
  const history = getHistory();
  history.unshift({
    id: Date.now(),
    timestamp: new Date().toISOString(),
    spectra_name: req.spectra_name,
    format: req.format,
    status: ok ? 'success' : 'error',
  });
  while (history.length > MAX_HISTORY) history.pop();
  localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
  renderHistory();
}

function renderHistory() {
  const history = getHistory();

  if (history.length === 0) {
    historyEmpty.classList.remove('hidden');
    historyList.classList.add('hidden');
    clearHistory.classList.add('hidden');
    return;
  }

  historyEmpty.classList.add('hidden');
  historyList.classList.remove('hidden');
  clearHistory.classList.remove('hidden');

  historyList.innerHTML = history.map((item) => {
    const time = new Date(item.timestamp).toLocaleString('ru-RU', {
      day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit',
    });
    const cls = item.status === 'success' ? 'ok' : 'err';
    return `
      <div class="hist-item">
        <span class="hist-item__dot ${cls}"></span>
        <span class="hist-item__name">${escapeHtml(item.spectra_name)}</span>
        <span class="hist-item__fmt">${escapeHtml(item.format)}</span>
        <span class="hist-item__time">${time}</span>
      </div>`;
  }).join('');
}

clearHistory.addEventListener('click', () => {
  localStorage.removeItem(HISTORY_KEY);
  renderHistory();
});

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

/* ============================================================
   Presets
   ============================================================ */
const EXAMPLES = {
  example1: {
    spectra_name: 'Example_Spectrum_001', spectra_path: './data/example1.csv',
    low_percentile: 5, high_percentile: 95, rel_error: 0.5, charge_max: 1,
    protocole: 'non_tmds',
    brutto_dict: { C: [4, 50], H: [4, 80], O: [0, 50], N: [0, 3], C_13: [0, 1], S: [0, 3] },
    width: 10, height: 6, dpi: 100, format: 'png',
  },
  example2: {
    spectra_name: 'High_Res_Spectrum', spectra_path: './data/high_res.csv',
    low_percentile: 1, high_percentile: 99, rel_error: 0.1, charge_max: 2,
    protocole: 'tmds',
    brutto_dict: { C: [10, 100], H: [10, 150], O: [0, 30], N: [0, 5], C_13: [0, 2], S: [0, 2] },
    width: 12, height: 8, dpi: 150, format: 'png',
  },
  example3: {
    spectra_name: 'Custom_Brutto_Spectrum', spectra_path: './data/custom.csv',
    low_percentile: 10, high_percentile: 90, rel_error: 0.3, charge_max: 1,
    protocole: 'non_tmds',
    brutto_dict: { C: [1, 20], H: [1, 30], O: [0, 10], N: [0, 2], S: [0, 1] },
    width: 10, height: 6, dpi: 100, format: 'svg',
  },
};

function loadExample(type) {
  const d = EXAMPLES[type];
  if (!d) return;
  $('spectra-name').value    = d.spectra_name;
  $('spectra-path').value    = d.spectra_path;
  $('low-percentile').value  = d.low_percentile;
  $('high-percentile').value = d.high_percentile;
  $('rel-error').value       = d.rel_error;
  $('charge-max').value      = d.charge_max;
  $('protocol').value        = d.protocole;
  $('brutto-dict').value     = JSON.stringify(d.brutto_dict, null, 2);
  $('width').value           = d.width;
  $('height').value          = d.height;
  $('dpi').value             = d.dpi;
  $('format').value          = d.format;
  // Preset click is an explicit action → run it.
  setTimeout(() => form.requestSubmit(), 250);
}

document.querySelectorAll('.chip[data-example]').forEach((chip) => {
  chip.addEventListener('click', () => loadExample(chip.dataset.example));
});

/* ============================================================
   Decorative spectra (header + empty state)
   ============================================================ */
function buildSpectrum(el, count, maxH, minH) {
  if (!el) return;
  let html = '';
  for (let i = 0; i < count; i++) {
    // pseudo-random but deterministic-ish heights for a spectrum look
    const r = Math.abs(Math.sin(i * 12.9898) * 43758.5453) % 1;
    const h = Math.round(minH + r * (maxH - minH));
    const delay = (i * 0.012).toFixed(3);
    html += `<span style="height:${h}px;animation-delay:${delay}s"></span>`;
  }
  el.innerHTML = html;
}

/* header ticks use .tick class (animated via CSS) */
function buildHeaderSpectrum(el, count) {
  if (!el) return;
  let html = '';
  for (let i = 0; i < count; i++) {
    const r = Math.abs(Math.sin(i * 7.13 + 2.1) * 9301.17) % 1;
    const h = Math.round(4 + r * 24);
    const delay = (i * 0.015).toFixed(3);
    html += `<span class="tick" style="height:${h}px;animation-delay:${delay}s"></span>`;
  }
  el.innerHTML = html;
}

/* ============================================================
   Init
   ============================================================ */
document.addEventListener('DOMContentLoaded', () => {
  renderHistory();
  setStatus('ready');
  buildHeaderSpectrum($('header-spectrum'), 64);
  buildSpectrum($('empty-spectrum'), 40, 90, 10);
});

/* Ctrl / Cmd + Enter → submit */
document.addEventListener('keydown', (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault();
    form.requestSubmit();
  }
});
