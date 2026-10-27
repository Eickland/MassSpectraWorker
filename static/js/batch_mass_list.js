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

const galleryEl      = document.getElementById("gallery");
const galleryEmptyEl = document.getElementById("gallery-empty");
const galleryCountEl = document.getElementById("gallery-count");

const GEAR_SVG = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></svg>';

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
  ready: 'Готов', loading: 'Обработка', success: 'Готово', error: 'Ошибка', converting: 'Конвертация', processing: 'Извлечение'
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

function addGalleryItem(raw) {
  const id = raw.id ?? raw.item_id;
  const result_url = raw.result_url;
  const spectra_name = raw.spectra_name;
  if (!result_url || id == null) return;
  const existing = galleryEl.querySelector(`[data-item-id="${CSS.escape(String(id))}"]`);
  if (existing) {
    // Спектр пересчитан или выбран другой прогон — обновляем картинку и ссылки
    if (existing.dataset.resultUrl !== result_url) {
      existing.dataset.resultUrl = result_url;
      existing.querySelector('.gallery-card__img').src = result_url;
      existing.querySelectorAll('.gallery-card__actions a').forEach((a) => { a.href = result_url; });
    }
    return;
  }

  const card = document.createElement("div");
  card.className = "gallery-card";
  card.dataset.itemId = id;
  card.dataset.resultUrl = result_url;

  const img = document.createElement("img");
  img.className = "gallery-card__img";
  img.loading = "lazy";
  img.src = result_url;
  img.alt = spectra_name || id;
  img.addEventListener("click", () => openLightbox(card.dataset.resultUrl, img.alt));

  const meta = document.createElement("div");
  meta.className = "gallery-card__meta";

  const name = document.createElement("span");
  name.className = "gallery-card__name";
  name.title = spectra_name || "";
  name.textContent = spectra_name || id;

  const actions = document.createElement("div");
  actions.className = "gallery-card__actions";
  actions.innerHTML = `
    <button type="button" class="item-edit" data-item-id="${escapeHtml(String(id))}" title="Параметры и пересчёт спектра">${GEAR_SVG}</button>
    <a href="${result_url}" target="_blank" rel="noopener" title="Открыть в новой вкладке">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
    </a>
    <a href="${result_url}" download title="Скачать">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
    </a>`;

  meta.appendChild(name);
  meta.appendChild(actions);
  card.appendChild(img);
  card.appendChild(meta);
  galleryEl.appendChild(card);

  galleryEmptyEl.classList.add("hidden");
  galleryCountEl.textContent = galleryEl.children.length;
}

function resetGallery() {
  galleryEl.innerHTML = "";
  galleryCountEl.textContent = "0";
  galleryEmptyEl.classList.remove("hidden");
}

function openLightbox(src, alt) {
  const lb = document.getElementById("lightbox");
  const im = document.getElementById("lightbox-img");
  im.src = src; im.alt = alt || "";
  lb.classList.remove("hidden");
}
document.getElementById("lightbox-close")?.addEventListener("click", () => {
  document.getElementById("lightbox").classList.add("hidden");
  document.getElementById("lightbox-img").src = "";
});

// ==== SSE ====




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
   Brutto dict: '{"C": [4, 50], ...}' -> {C: {min: 4, max: 50}, ...}
   Пустое поле -> null (Python возьмёт словарь по умолчанию).
   ============================================================ */
function parseBruttoDict(text) {
  const trimmed = (text || '').trim();
  if (!trimmed) return null;
  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch (e) {
    throw new Error('Брутто-словарь: некорректный JSON (' + e.message + ')');
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Брутто-словарь: ожидается объект вида {"C": [4, 50]}');
  }
  const out = {};
  for (const [el, v] of Object.entries(parsed)) {
    let min, max;
    if (Array.isArray(v) && v.length === 2) {
      [min, max] = v;
    } else if (v && typeof v === 'object' && 'min' in v && 'max' in v) {
      ({ min, max } = v);
    } else {
      throw new Error(`Брутто-словарь: для "${el}" нужен [min, max]`);
    }
    if (!Number.isInteger(min) || !Number.isInteger(max) || min > max) {
      throw new Error(`Брутто-словарь: для "${el}" нужны целые min ≤ max`);
    }
    out[el] = { min, max };
  }
  return out;
}

/* ============================================================
   Collect JobParams (must match Go model.JobParams:
   rel_error in ppm, width/height/dpi/charge_max are ints)
   ============================================================ */
function collectBatchOptions() {
  const options = {};
  for (const [id, key, label] of [
    ['sulfur-factor', 'sulfur_precision_factor', 'Точность по S'],
    ['nitrogen-factor', 'nitrogen_precision_factor', 'Точность по N'],
  ]) {
    const v = $(id).value.trim();
    if (!v) continue;
    const n = parseFloat(v);
    if (Number.isNaN(n) || n < 0) throw new Error(`${label}: нужно неотрицательное число`);
    options[key] = String(n);
  }
  return Object.keys(options).length ? options : undefined;
}

function collectParams() {
  return {
    options: collectBatchOptions(),
    low_percentile: parseFloat($('low_percentile').value) || 99.5,
    high_percentile: parseFloat($('high_percentile').value) || 99.9,
    rel_error: parseFloat($('rel-error').value) || 0.5,
    charge_max: parseInt($('charge-max').value, 10) || 1,
    protocol: $('protocol').value,
    brutto_dict: parseBruttoDict($('brutto-dict').value),
    width: parseInt($('width').value, 10) || 10,
    height: parseInt($('height').value, 10) || 6,
    dpi: parseInt($('dpi').value, 10) || 100,
    output_format: $('format').value,
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

function trackJob(jobId, { reset = true } = {}) {
  stopTracking();
  if (reset) {
    resetGallery();
    currentJobId = jobId;
    currentPage = 1;
    showActiveState(jobId);
  }
  fetchStatus(jobId);
  fetchItems(jobId, currentPage);

  try {
    es = new EventSource(`/api/batch/${jobId}/events`);

    /* The initial frame from the server has no "event:" line, so it
       arrives as a default "message" — its JSON body carries
       {event: "init", data: JobProgress}. */
    es.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg && msg.event === 'init' && msg.data) {
          applyProgress(msg.data);
          (msg.data.results || []).forEach(addGalleryItem);   // если сервер шлёт results
        }
      } catch (err) {}
    };

    es.addEventListener('item_done', (e) => {
      try {
        addGalleryItem(JSON.parse(e.data));
      } catch (err) {}
      fetchStatus(jobId);          // обновить счётчики и прогресс
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
      const url = `/api/batch/${currentJobId}/results.zip`;
      const a = document.createElement('a');
      a.href = url;
      a.download = `job_${currentJobId}_results.zip`; // подсказка имени
      document.body.appendChild(a);
      a.click();
      a.remove();
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
  items.forEach((it) => { if (it.status === 'done' && it.result_url) addGalleryItem(it); });
  if (!items.length) {
    itemsBody.innerHTML = '<div class="items-row" style="color:var(--text-dim)">Файлов пока нет</div>';
    return;
  }
  itemsBody.innerHTML = items.map((item) => `
    <div class="items-row">
      <div class="item-name" title="${escapeHtml(item.spectra_path)}">
        ${escapeHtml(item.spectra_name)}
        ${item.params ? '<span class="tag" title="У спектра свои параметры">свои</span>' : ''}
        <span class="item-path">${escapeHtml(item.spectra_path)}</span>
      </div>
      <span class="item-status-cell item-status" data-status="${item.status}">
        <span class="dot"></span>${item.status}
      </span>
      <span class="item-error" title="${escapeHtml(item.error || '')}">${escapeHtml(item.error || '')}</span>
      <button type="button" class="icon-btn item-edit" data-item-id="${escapeHtml(item.id)}" title="Параметры и пересчёт спектра">${GEAR_SVG}</button>
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
   Server-side folder browser
   (native OS file/folder dialogs can't hand back a filesystem
   path — browsers deliberately hide it — so instead this asks the
   Go server, via GET /api/browse, to list directories on the
   machine the SERVER runs on. On WSL that naturally includes
   /mnt/c, /mnt/d, etc., so a folder on the Windows side of the
   same machine is reachable; a folder on a genuinely different
   device with no shared mount is not — see the field note.)
   ============================================================ */
const browseOpenBtn   = $('browse-open-btn');
const browseModal     = $('browse-modal');
const browseCloseBtn  = $('browse-close');
const browseCancelBtn = $('browse-cancel');
const browseSelectBtn = $('browse-select');
const browseUpBtn     = $('browse-up');
const browseCurrentPathEl = $('browse-current-path');
const browseRootsEl   = $('browse-roots');
const browseListEl    = $('browse-list');
const browseErrorEl   = $('browse-error');

let browseCurrentPath = null;
let browseParentPath = null;

function openBrowseModal() {
  browseModal.classList.remove('hidden');
  loadBrowse($('folder-path').value.trim());
}

function closeBrowseModal() {
  browseModal.classList.add('hidden');
}

async function loadBrowse(path) {
  browseErrorEl.classList.add('hidden');
  try {
    const url = path ? `/api/browse?path=${encodeURIComponent(path)}` : '/api/browse';
    const res = await fetch(url);
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `HTTP ${res.status}`);
    }
    const data = await res.json();
    browseCurrentPath = data.path;
    browseParentPath = data.parent || null;
    browseCurrentPathEl.textContent = data.path;
    browseUpBtn.disabled = !browseParentPath;
    renderBrowseRoots(data.roots || []);
    renderBrowseList(data.entries || []);
  } catch (err) {
    browseErrorEl.textContent = 'Не удалось открыть папку: ' + err.message;
    browseErrorEl.classList.remove('hidden');
  }
}

function renderBrowseRoots(roots) {
  if (!roots.length) {
    browseRootsEl.innerHTML = '';
    return;
  }
  browseRootsEl.innerHTML = roots.map((r) => `
    <button type="button" class="browse-root-item" data-path="${escapeHtml(r.path)}">${escapeHtml(r.name)}</button>
  `).join('');
  browseRootsEl.querySelectorAll('.browse-root-item').forEach((btn) => {
    btn.addEventListener('click', () => loadBrowse(btn.dataset.path));
  });
}

function renderBrowseList(entries) {
  if (!entries.length) {
    browseListEl.innerHTML = '<div class="browse-empty">Вложенных папок нет</div>';
    return;
  }
  browseListEl.innerHTML = entries.map((e) => `
    <button type="button" class="browse-item" data-path="${escapeHtml(e.path)}">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
      <span>${escapeHtml(e.name)}</span>
    </button>
  `).join('');
  browseListEl.querySelectorAll('.browse-item').forEach((btn) => {
    btn.addEventListener('click', () => loadBrowse(btn.dataset.path));
  });
}

browseOpenBtn.addEventListener('click', openBrowseModal);
browseCloseBtn.addEventListener('click', closeBrowseModal);
browseCancelBtn.addEventListener('click', closeBrowseModal);
browseUpBtn.addEventListener('click', () => {
  if (browseParentPath) loadBrowse(browseParentPath);
});
browseSelectBtn.addEventListener('click', () => {
  if (browseCurrentPath) {
    $('folder-path').value = browseCurrentPath;
    showToast('Папка выбрана');
  }
  closeBrowseModal();
});
browseModal.addEventListener('click', (e) => {
  if (e.target === browseModal) closeBrowseModal();
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !itemModal.classList.contains('hidden')
      && document.getElementById('lightbox').classList.contains('hidden')) closeItemModal();
  if (e.key === 'Escape' && !browseModal.classList.contains('hidden')) closeBrowseModal();
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

/* ============================================================
   Per-spectrum parameters: edit, reprocess, run history
   API:
     GET  /api/batch/{job}/items/{item}            -> {item, job_params, effective_params, custom_params, runs}
     POST /api/batch/{job}/items/{item}/reprocess  {params} -> 202
     POST /api/batch/{job}/items/{item}/runs/{run}/select
   ============================================================ */
const itemModal   = $('item-modal');
const ipTitle     = $('ip-title');
const ipParamsTag = $('ip-params-tag');
const ipStatus    = $('ip-status');
const ipError     = $('ip-error');
const ipRunsList  = $('ip-runs-list');
const ipReprocessBtn = $('ip-reprocess');
const ipUseJobBtn    = $('ip-use-job');

let ipItemId = null;
let ipDetail = null;
let ipPollTimer = null;

// Кнопки «Параметры» в таблице и в галерее
document.addEventListener('click', (e) => {
  const btn = e.target.closest('.item-edit');
  if (btn && btn.dataset.itemId) {
    e.preventDefault();
    openItemModal(btn.dataset.itemId);
  }
});

$('ip-close').addEventListener('click', closeItemModal);
itemModal.addEventListener('click', (e) => { if (e.target === itemModal) closeItemModal(); });

async function openItemModal(itemId) {
  if (!currentJobId) return;
  ipItemId = itemId;
  ipDetail = null;
  ipTitle.textContent = 'Загрузка…';
  ipRunsList.innerHTML = '';
  setIpError('');
  itemModal.classList.remove('hidden');
  const ok = await loadItemDetail({ fillForm: true });
  if (ok && isBusy(ipDetail.item)) startIpPolling();
}

function closeItemModal() {
  itemModal.classList.add('hidden');
  stopIpPolling();
  ipItemId = null;
  ipDetail = null;
}

function isBusy(item) {
  return item && (item.status === 'pending' || item.status === 'processing');
}

async function loadItemDetail({ fillForm = false } = {}) {
  const itemId = ipItemId;
  try {
    const res = await fetch(`/api/batch/${currentJobId}/items/${itemId}`);
    if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
    const detail = await res.json();
    if (itemId !== ipItemId) return false; // окно уже закрыли или открыли другой спектр
    ipDetail = detail;
    renderItemHeader(detail);
    renderRuns(detail);
    if (fillForm) fillItemForm(detail.effective_params);
    return true;
  } catch (err) {
    setIpError('Не удалось загрузить спектр: ' + err.message);
    return false;
  }
}

function renderItemHeader(d) {
  ipTitle.textContent = d.item.spectra_name;
  ipTitle.title = d.item.spectra_path;
  ipParamsTag.textContent = d.custom_params ? 'свои параметры' : 'параметры задачи';
  ipParamsTag.classList.toggle('tag--muted', !d.custom_params);

  const busy = isBusy(d.item);
  ipStatus.dataset.busy = busy ? '1' : '0';
  ipStatus.textContent = busy
    ? (d.item.status === 'pending' ? 'в очереди…' : 'обрабатывается…')
    : (d.item.status === 'failed' ? 'последний прогон с ошибкой' : '');
  ipReprocessBtn.disabled = busy;
}

function paramsSummary(p) {
  if (!p) return '';
  const parts = [
    p.protocol,
    `${p.rel_error} ppm`,
    `z≤${p.charge_max}`,
    `${p.low_percentile}–${p.high_percentile}%`,
  ];
  if (p.brutto_dict) {
    parts.push(Object.entries(p.brutto_dict).map(([el, r]) => `${el}${r.min}-${r.max}`).join(' '));
  }
  const opts = p.options || {};
  if (opts.sulfur_precision_factor) parts.push(`S×${opts.sulfur_precision_factor}`);
  if (opts.nitrogen_precision_factor) parts.push(`N×${opts.nitrogen_precision_factor}`);
  if (opts.recalibrate === 'false') parts.push('без рекалибровки');
  return parts.join(' · ');
}

function formatTime(iso) {
  if (!iso) return '';
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function renderRuns(d) {
  const runs = d.runs || [];
  if (!runs.length) {
    ipRunsList.innerHTML = '<div class="empty-note">Прогонов ещё не было.</div>';
    return;
  }
  ipRunsList.innerHTML = runs.map((r) => {
    const thumb = r.result_url && /\.(png|jpe?g|svg)$/i.test(r.result_url)
      ? `<img class="run-card__thumb" src="${escapeHtml(r.result_url)}" alt="Прогон ${r.run_no}" data-full="${escapeHtml(r.result_url)}" loading="lazy">`
      : `<div class="run-card__thumb run-card__thumb--empty">${
          r.status === 'processing' ? 'обработка…' : r.status === 'failed' ? 'ошибка' : (r.result_url ? 'PDF' : 'нет картинки')}</div>`;
    const statusTag = r.status === 'failed'
      ? '<span class="tag tag--danger">ошибка</span>'
      : r.status === 'processing' ? '<span class="tag tag--muted">обработка</span>' : '';
    const canSelect = r.status === 'done' && !r.current && !isBusy(d.item);
    return `
      <div class="run-card ${r.current ? 'current' : ''}">
        ${thumb}
        <div class="run-card__body">
          <div class="run-card__title">
            Прогон #${r.run_no}
            ${r.current ? '<span class="tag">текущий</span>' : ''}
            ${statusTag}
          </div>
          <div class="run-card__meta">${escapeHtml(formatTime(r.finished_at || r.created_at))}${
            r.spectrum_rows ? ` · формул: ${r.spectrum_rows}` : ''}</div>
          <div class="run-card__params" title="${escapeHtml(paramsSummary(r.params))}">${escapeHtml(paramsSummary(r.params))}</div>
          ${r.error ? `<div class="run-card__error" title="${escapeHtml(r.error)}">${escapeHtml(r.error)}</div>` : ''}
          <div class="run-card__actions">
            ${canSelect ? `<button type="button" class="btn btn--primary btn--sm" data-select-run="${r.id}">Сделать текущим</button>` : ''}
            <button type="button" class="btn btn--ghost btn--sm" data-load-run="${r.id}" title="Подставить параметры этого прогона в форму">Взять параметры</button>
            ${r.result_url ? `<a class="btn btn--ghost btn--sm" href="${escapeHtml(r.result_url)}" target="_blank" rel="noopener">Открыть</a>` : ''}
          </div>
        </div>
      </div>`;
  }).join('');
}

ipRunsList.addEventListener('click', async (e) => {
  const thumb = e.target.closest('.run-card__thumb[data-full]');
  if (thumb) { openLightbox(thumb.dataset.full, ipTitle.textContent); return; }

  const loadBtn = e.target.closest('[data-load-run]');
  if (loadBtn && ipDetail) {
    const run = (ipDetail.runs || []).find((r) => r.id === loadBtn.dataset.loadRun);
    if (run) { fillItemForm(run.params); showToast(`Параметры прогона #${run.run_no} в форме`); }
    return;
  }

  const selBtn = e.target.closest('[data-select-run]');
  if (selBtn) {
    selBtn.disabled = true;
    try {
      const res = await fetch(
        `/api/batch/${currentJobId}/items/${ipItemId}/runs/${selBtn.dataset.selectRun}/select`,
        { method: 'POST' });
      if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
      showToast('Прогон выбран');
      await loadItemDetail({ fillForm: true });
      if (ipDetail && ipDetail.item.result_url) {
        addGalleryItem({ id: ipDetail.item.id, spectra_name: ipDetail.item.spectra_name, result_url: ipDetail.item.result_url });
      }
      fetchStatus(currentJobId);
      fetchItems(currentJobId, currentPage);
    } catch (err) {
      setIpError('Не удалось выбрать прогон: ' + err.message);
      selBtn.disabled = false;
    }
  }
});

/* ---------- form <-> params ---------- */
function bruttoToText(dict) {
  if (!dict || !Object.keys(dict).length) return '';
  const parts = Object.entries(dict).map(([el, r]) => `"${el}": [${r.min}, ${r.max}]`);
  return '{' + parts.join(', ') + '}';
}

function fillItemForm(p) {
  if (!p) return;
  const opts = p.options || {};
  $('ip-low').value = p.low_percentile ?? '';
  $('ip-high').value = p.high_percentile ?? '';
  $('ip-protocol').value = p.protocol || 'non_tmds';
  $('ip-rel').value = p.rel_error ?? '';
  $('ip-charge').value = p.charge_max ?? 1;
  $('ip-recal').checked = opts.recalibrate !== 'false';
  $('ip-brutto').value = bruttoToText(p.brutto_dict);
  $('ip-sulfur').value = opts.sulfur_precision_factor ?? '';
  $('ip-nitrogen').value = opts.nitrogen_precision_factor ?? '';
  $('ip-width').value = p.width ?? '';
  $('ip-height').value = p.height ?? '';
  $('ip-dpi').value = p.dpi ?? '';
  $('ip-format').value = p.output_format || 'png';
  setIpError('');
}

function numOrUndefined(id, parse = parseFloat) {
  const v = $(id).value.trim();
  if (v === '') return undefined;
  const n = parse(v, 10);
  if (Number.isNaN(n)) throw new Error(`Поле «${$(id).labels?.[0]?.textContent.trim() || id}»: нужно число`);
  return n;
}

function collectItemParams() {
  // Опции, которых нет в форме (например, tmds_*), сохраняем как были
  const base = (ipDetail && ipDetail.effective_params && ipDetail.effective_params.options) || {};
  const options = { ...base, recalibrate: $('ip-recal').checked ? 'true' : 'false' };
  const sulfur = $('ip-sulfur').value.trim();
  const nitrogen = $('ip-nitrogen').value.trim();
  if (sulfur) options.sulfur_precision_factor = sulfur; else delete options.sulfur_precision_factor;
  if (nitrogen) options.nitrogen_precision_factor = nitrogen; else delete options.nitrogen_precision_factor;

  return {
    low_percentile: numOrUndefined('ip-low') ?? 0,
    high_percentile: numOrUndefined('ip-high') ?? 0,
    protocol: $('ip-protocol').value,
    rel_error: numOrUndefined('ip-rel'),
    charge_max: numOrUndefined('ip-charge', parseInt),
    brutto_dict: parseBruttoDict($('ip-brutto').value),
    options,
    width: numOrUndefined('ip-width', parseInt),
    height: numOrUndefined('ip-height', parseInt),
    dpi: numOrUndefined('ip-dpi', parseInt),
    output_format: $('ip-format').value,
  };
}

function setIpError(text) {
  ipError.textContent = text;
  ipError.title = text;
  ipError.classList.toggle('hidden', !text);
}

ipUseJobBtn.addEventListener('click', () => {
  if (ipDetail) {
    fillItemForm(ipDetail.job_params);
    showToast('В форме общие параметры задачи');
  }
});

ipReprocessBtn.addEventListener('click', async () => {
  if (!ipItemId || !currentJobId) return;
  let params;
  try {
    params = collectItemParams();
  } catch (err) {
    setIpError(err.message);
    return;
  }
  setIpError('');
  ipReprocessBtn.disabled = true;
  try {
    const res = await fetch(`/api/batch/${currentJobId}/items/${ipItemId}/reprocess`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ params }),
    });
    if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
    showToast('Спектр поставлен в очередь');
    // Задача снова в работе — переподключаемся к потоку событий, не сбрасывая галерею
    trackJob(currentJobId, { reset: false });
    await loadItemDetail();
    startIpPolling();
  } catch (err) {
    setIpError('Не удалось запустить пересчёт: ' + err.message);
    ipReprocessBtn.disabled = false;
  }
});

function startIpPolling() {
  stopIpPolling();
  ipPollTimer = setInterval(async () => {
    if (!ipItemId) { stopIpPolling(); return; }
    await loadItemDetail();
    if (ipDetail && !isBusy(ipDetail.item)) {
      stopIpPolling();
      if (ipDetail.item.status === 'done') {
        showToast('Пересчёт готов');
        if (ipDetail.item.result_url) {
          addGalleryItem({ id: ipDetail.item.id, spectra_name: ipDetail.item.spectra_name, result_url: ipDetail.item.result_url });
        }
      } else if (ipDetail.item.status === 'failed') {
        showToast('Пересчёт завершился ошибкой');
      }
      fetchItems(currentJobId, currentPage);
    }
  }, 1500);
}

function stopIpPolling() {
  if (ipPollTimer) { clearInterval(ipPollTimer); ipPollTimer = null; }
}
