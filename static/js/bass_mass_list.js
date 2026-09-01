/* ============================================================
   MassSpectraWorker — Batch Mass List frontend
   API contract:
     POST   /api/batch                    -> 202 { job_id, status, message }
     GET    /api/batch/{id}                -> JobProgress
     GET    /api/batch/{id}/items?page=&limit= -> { items, pagination }
     GET    /api/batch/{id}/events         -> SSE (init via default "message",
                                               item_done/item_failed/job_done/ping
                                               as named events)
     GET    /api/batch/{id}/results.zip    -> file download
     POST   /api/batch/{id}/cancel         -> { job_id, status, message }
   ============================================================ */

'use strict';

const HISTORY_KEY = 'batchMassListHistory';
const MAX_HISTORY = 15;
const ITEMS_LIMIT = 50;
const TERMINAL_STATUSES = ['done', 'failed', 'cancelled'];

/* ---------- DOM ---------- */
const $ = (id) => document.getElementById(id);

const form        = $('batch-form');
const submitBtn   = $('submit-btn');
const submitLabel = $('submit-label');

const statusEl   = $('status');
const statusText = $('status-text');

const resultMeta = $('result-meta');

const modeBtnFolder = $('mode-btn-folder');
const modeBtnItems  = $('mode-btn-items');
const modePaneFolder = $('mode-pane-folder');
const modePaneItems  = $('mode-pane-items');

const stateEmpty  = $('state-empty');
const stateActive = $('state-active');

const jobIdChip     = $('job-id-chip');
const jobPill        = $('job-pill');
const jobStatusText  = $('job-status-text');
const progressFill   = $('progress-fill');
const statTotal   = $('stat-total');
const statDone    = $('stat-done');
const statFailed  = $('stat-failed');
const statElapsed = $('stat-elapsed');

const downloadZipBtn = $('download-zip-btn');
const cancelBtn       = $('cancel-btn');
const refreshBtn      = $('refresh-btn');

const itemsBody = $('items-body');
const itemsPaginationInfo = $('items-pagination-info');
const itemsPrevBtn = $('items-prev');
const itemsNextBtn = $('items-next');

const historyList  = $('history-list');
const historyEmpty = $('history-empty');
const clearHistoryBtn = $('clear-history');

const toast     = $('toast');
const toastText = $('toast-text');

/* ---------- Local state ---------- */
let currentJobId = null;
let currentPage = 1;
let itemsTotalPages = 1;
let es = null;
let pollTimer = null;
let itemsRefreshTimer = null;
let sourceMode = 'folder';

/* ============================================================
   Status pill (topbar)
   ============================================================ */
const STATUS_LABELS = {
  ready: 'Готов', loading: 'Обработка', success: 'Готово', error: 'Ошибка',
};
function setTopStatus(state, text) {
  statusEl.dataset.state = state;
  statusText.textContent = text || STATUS_LABELS[state] || state;
}

/* ============================================================
   Toast
   ============================================================ */
let toastTimer = null;
function showToast(text) {
  toastText.textContent = text;
  toast.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove('show'), 2200);
}

/* ============================================================
   Source mode switch
   ============================================================ */
function setMode(mode) {
  sourceMode = mode;
  modeBtnFolder.classList.toggle('active', mode === 'folder');
  modeBtnItems.classList.toggle('active', mode === 'items');
  modePaneFolder.classList.toggle('hidden', mode !== 'folder');
  modePaneItems.classList.toggle('hidden', mode !== 'items');
}
modeBtnFolder.addEventListener('click', () => setMode('folder'));
modeBtnItems.addEventListener('click', () => setMode('items'));

/* ============================================================
   Parse "имя = путь" / "путь" textarea into [{name, path}]
   ============================================================ */
function parseItemsText(text) {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const eqIdx = line.indexOf('=');
      if (eqIdx !== -1) {
        return {
          name: line.slice(0, eqIdx).trim(),
          path: line.slice(eqIdx + 1).trim(),
        };
      }
      const base = line.split(/[\\/]/).pop() || line;
      return { name: base, path: line };
    })
    .filter((item) => item.path);
}

/* ============================================================
   Collect JobParams (must match Go model.JobParams exactly:
   percentile is an int, rel_error is 0-1, width/height/dpi are ints)
   ============================================================ */
function collectParams() {
  return {
    percentile: Math.round(parseFloat($('percentile').value) || 99),
    rel_error: parseFloat($('rel-error').value) || 0.5,
    protocol: $('protocol').value,
    brutto_dict: $('brutto-dict').value.trim(),
    width: parseInt($('width').value, 10) || 10,
    height: parseInt($('height').value, 10) || 6,
    dpi: parseInt($('dpi').value, 10) || 100,
    format: $('format').value,
  };
}

function buildRequestBody() {
  const body = { params: collectParams() };
  if (sourceMode === 'folder') {
    const folderPath = $('folder-path').value.trim();
    if (!folderPath) throw new Error('Укажите путь к папке');
    body.folder_path = folderPath;
  } else {
    const items = parseItemsText($('items-text').value);
    if (items.length === 0) throw new Error('Добавьте хотя бы один файл в список');
    body.items = items;
  }
  return body;
}

/* ============================================================
   Submit -> create job
   ============================================================ */
form.addEventListener('submit', async (e) => {
  e.preventDefault();

  let body;
  try {
    body = buildRequestBody();
  } catch (err) {
    showToast(err.message);
    return;
  }

  submitBtn.disabled = true;
  submitLabel.textContent = 'Создание задачи…';
  setTopStatus('loading', 'Создание задачи');

  try {
    const res = await fetch('/api/batch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `HTTP ${res.status}`);
    }

    const data = await res.json();
    addToHistory({
      id: data.job_id,
      timestamp: new Date().toISOString(),
      source: sourceMode === 'folder' ? body.folder_path : `${body.items.length} файл(ов)`,
    });
    renderHistory();
    showToast('Задача создана');
    trackJob(data.job_id);
  } catch (err) {
    setTopStatus('error', 'Ошибка');
    showToast('Не удалось создать задачу: ' + err.message);
  } finally {
    submitBtn.disabled = false;
    submitLabel.textContent = 'Запустить пакетную обработку';
  }
});

/* Ctrl / Cmd + Enter -> submit */
document.addEventListener('keydown', (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault();
    form.requestSubmit();
  }
});

/* ============================================================
   Tracking a job: SSE with polling fallback
   ============================================================ */
function stopTracking() {
  if (es) { es.close(); es = null; }
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  clearTimeout(itemsRefreshTimer);
}

function trackJob(jobId) {
  stopTracking();
  currentJobId = jobId;
  currentPage = 1;
  showActiveState(jobId);
  fetchStatus(jobId);
  fetchItems(jobId, 1);

  try {
    es = new EventSource(`/api/batch/${jobId}/events`);

    /* The initial frame from the server has no "event:" line, so it
       arrives as a default "message" — its JSON body carries
       {event: "init", data: JobProgress}. */
    es.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg && msg.event === 'init' && msg.data) applyProgress(msg.data);
      } catch (err) { /* ignore malformed frame */ }
    };

    es.addEventListener('item_done', () => {
      fetchStatus(jobId);
      scheduleItemsRefresh(jobId);
    });
    es.addEventListener('item_failed', () => {
      fetchStatus(jobId);
      scheduleItemsRefresh(jobId);
    });
    es.addEventListener('job_done', (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg && msg.data) applyProgress(msg.data);
      } catch (err) { /* ignore */ }
      fetchItems(jobId, currentPage);
      finishTracking();
    });
    es.addEventListener('ping', () => { /* keep-alive, no-op */ });

    es.onerror = () => {
      if (es) { es.close(); es = null; }
      startPolling(jobId);
    };
  } catch (err) {
    startPolling(jobId);
  }
}

function startPolling(jobId) {
  if (pollTimer) return;
  pollTimer = setInterval(async () => {
    const progress = await fetchStatus(jobId);
    if (progress && TERMINAL_STATUSES.includes(progress.status)) {
      fetchItems(jobId, currentPage);
      finishTracking();
    }
  }, 2000);
}

function scheduleItemsRefresh(jobId) {
  clearTimeout(itemsRefreshTimer);
  itemsRefreshTimer = setTimeout(() => fetchItems(jobId, currentPage), 400);
}

function finishTracking() {
  if (es) { es.close(); es = null; }
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  updateHistoryEntryStatus(currentJobId);
  setTopStatus('success', 'Готово');
}

/* ============================================================
   Fetch helpers
   ============================================================ */
async function fetchStatus(jobId) {
  try {
    const res = await fetch(`/api/batch/${jobId}`);
    if (!res.ok) return null;
    const progress = await res.json();
    applyProgress(progress);
    return progress;
  } catch (err) {
    return null;
  }
}

async function fetchItems(jobId, page) {
  try {
    const res = await fetch(`/api/batch/${jobId}/items?page=${page}&limit=${ITEMS_LIMIT}`);
    if (!res.ok) return;
    const data = await res.json();
    renderItems(data.items || []);
    if (data.pagination) {
      currentPage = data.pagination.page;
      itemsTotalPages = Math.max(1, data.pagination.pages);
      itemsPaginationInfo.textContent =
        `${data.pagination.total} файл(ов) · страница ${currentPage} из ${itemsTotalPages}`;
      itemsPrevBtn.disabled = currentPage <= 1;
      itemsNextBtn.disabled = currentPage >= itemsTotalPages;
    }
  } catch (err) { /* ignore transient errors */ }
}

itemsPrevBtn.addEventListener('click', () => {
  if (currentJobId && currentPage > 1) fetchItems(currentJobId, currentPage - 1);
});
itemsNextBtn.addEventListener('click', () => {
  if (currentJobId && currentPage < itemsTotalPages) fetchItems(currentJobId, currentPage + 1);
});
refreshBtn.addEventListener('click', () => {
  if (!currentJobId) return;
  fetchStatus(currentJobId);
  fetchItems(currentJobId, currentPage);
});

/* ============================================================
   Render: progress / summary
   ============================================================ */
function showActiveState(jobId) {
  stateEmpty.classList.add('hidden');
  stateActive.classList.remove('hidden');
  jobIdChip.textContent = jobId;
  resultMeta.textContent = jobId;
  downloadZipBtn.disabled = true;
  cancelBtn.disabled = false;
  setTopStatus('loading', 'Отслеживание');
}

function applyProgress(progress) {
  if (!progress) return;
  jobPill.dataset.status = progress.status;
  jobStatusText.textContent = progress.status;

  const pct = Math.max(0, Math.min(100, progress.progress || 0));
  progressFill.style.width = pct + '%';
  progressFill.dataset.danger = progress.failed_items > 0 ? '1' : '0';

  statTotal.textContent = progress.total_items ?? 0;
  statDone.textContent = progress.done_items ?? 0;
  statFailed.textContent = progress.failed_items ?? 0;
  statElapsed.textContent = progress.elapsed || '—';

  const canDownload = (progress.done_items || 0) > 0;
  downloadZipBtn.disabled = !canDownload;
  if (canDownload && currentJobId) {
    downloadZipBtn.onclick = () => {
      window.open(`/api/batch/${currentJobId}/results.zip`, '_blank');
    };
  } else {
    downloadZipBtn.onclick = null;
  }

  const isTerminal = TERMINAL_STATUSES.includes(progress.status);
  cancelBtn.disabled = isTerminal;

  if (isTerminal) {
    setTopStatus(progress.status === 'failed' ? 'error' : 'success',
      progress.status === 'done' ? 'Готово'
      : progress.status === 'failed' ? 'Завершено с ошибками'
      : 'Отменено');
  } else {
    setTopStatus('loading', 'Обработка');
  }
}

function renderItems(items) {
  if (!items.length) {
    itemsBody.innerHTML = '<div class="items-row" style="color:var(--text-dim)">Файлов пока нет</div>';
    return;
  }
  itemsBody.innerHTML = items.map((item) => `
    <div class="items-row">
      <div class="item-name" title="${escapeHtml(item.spectra_path)}">
        ${escapeHtml(item.spectra_name)}
        <span class="item-path">${escapeHtml(item.spectra_path)}</span>
      </div>
      <span class="item-status-cell item-status" data-status="${item.status}">
        <span class="dot"></span>${item.status}
      </span>
      <span class="item-error" title="${escapeHtml(item.error || '')}">${escapeHtml(item.error || '')}</span>
    </div>
  `).join('');
}

function escapeHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/* ============================================================
   Cancel
   ============================================================ */
cancelBtn.addEventListener('click', async () => {
  if (!currentJobId) return;
  cancelBtn.disabled = true;
  try {
    const res = await fetch(`/api/batch/${currentJobId}/cancel`, { method: 'POST' });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `HTTP ${res.status}`);
    }
    showToast('Отмена запрошена');
    fetchStatus(currentJobId);
  } catch (err) {
    showToast('Не удалось отменить: ' + err.message);
    cancelBtn.disabled = false;
  }
});

/* ============================================================
   History (localStorage)
   ============================================================ */
function getHistory() {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY)) || []; }
  catch { return []; }
}

function saveHistory(history) {
  localStorage.setItem(HISTORY_KEY, JSON.stringify(history.slice(0, MAX_HISTORY)));
}

function addToHistory(entry) {
  const history = getHistory();
  history.unshift({ ...entry, status: 'pending' });
  saveHistory(history);
}

function updateHistoryEntryStatus(jobId) {
  if (!jobId) return;
  const history = getHistory();
  const entry = history.find((h) => h.id === jobId);
  if (entry) {
    entry.status = jobPill.dataset.status || entry.status;
    saveHistory(history);
    renderHistory();
  }
}

function renderHistory() {
  const history = getHistory();
  historyEmpty.classList.toggle('hidden', history.length > 0);
  historyList.classList.toggle('hidden', history.length === 0);
  clearHistoryBtn.classList.toggle('hidden', history.length === 0);

  historyList.innerHTML = history.map((entry) => {
    const time = new Date(entry.timestamp).toLocaleString('ru-RU', {
      day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit',
    });
    const active = entry.id === currentJobId ? 'active' : '';
    return `
      <button type="button" class="job-history-item ${active}" data-job-id="${entry.id}">
        <div class="job-history-item__top">
          <span class="job-history-item__src">${escapeHtml(entry.source || '')}</span>
          <span class="pill" data-status="${entry.status || 'pending'}" style="padding:2px 8px;font-size:10px;">
            <span class="dot"></span>${entry.status || 'pending'}
          </span>
        </div>
        <span class="job-history-item__meta">${time} · ${entry.id}</span>
      </button>
    `;
  }).join('');

  historyList.querySelectorAll('.job-history-item').forEach((btn) => {
    btn.addEventListener('click', () => trackJob(btn.dataset.jobId));
  });
}

clearHistoryBtn.addEventListener('click', () => {
  localStorage.removeItem(HISTORY_KEY);
  renderHistory();
});

/* ============================================================
   Decorative empty-state spectrum bars (same motif as mass_list)
   ============================================================ */
function buildSpectrum(el, count, maxH, minH) {
  if (!el) return;
  let html = '';
  for (let i = 0; i < count; i++) {
    const r = Math.abs(Math.sin(i * 12.9898) * 43758.5453) % 1;
    const h = Math.round(minH + r * (maxH - minH));
    const delay = (i * 0.012).toFixed(3);
    html += `<span style="height:${h}px;animation-delay:${delay}s"></span>`;
  }
  el.innerHTML = html;
}
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
  setTopStatus('ready');
  buildHeaderSpectrum($('header-spectrum'), 64);
  buildSpectrum($('empty-spectrum'), 40, 90, 10);
  renderHistory();

  // Resume tracking the most recent non-terminal job, if any.
  const history = getHistory();
  const active = history.find((h) => !TERMINAL_STATUSES.includes(h.status));
  if (active) {
    trackJob(active.id);
  }
});