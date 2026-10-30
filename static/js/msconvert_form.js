/* ============================================================
   MassSpectraWorker — поля «Конвертация (msconvert)»
   Используется в форме пакетной задачи и в окне пересчёта спектра.
   Должно совпадать с Go model.ConvertParams:
     peak_picking: vendor | cwt | none
     ms_levels, cwt_snr, cwt_peak_space,
     threshold_type: "" | absolute | bpi-relative | tic-relative | count, threshold,
     mz_min, mz_max, keep_zero_samples, extra_filters: []
   API:
     GET  /api/msconvert          -> {configured, found, path, defaults}
     POST /api/msconvert/preview  {convert} -> {params, filters, command, tag, error}
   ============================================================ */

'use strict';

const MsConvertForm = (() => {
  const ALGO_NOTES = {
    vendor: 'Алгоритм прибора. Порог задаёт прибор (у Bruker — доля от главного пика), поэтому сильный загрязнитель может срезать слабые пики.',
    cwt: 'Вейвлет ProteoWizard: порог по сигнал/шум, не зависит от главного пика. Для FTICR держите расстояние между пиками ≤ 0,001.',
    none: 'Без центроидирования: в обработку уходит профильный спектр, пики выделит Python по перцентилям.',
  };
  const THRESHOLD_NOTES = {
    '': '',
    absolute: 'Абсолютная интенсивность: оставить пики не слабее значения, например 2e7.',
    'bpi-relative': 'Доля от самого сильного пика: 0.002 = 0,2%.',
    'tic-relative': 'Доля от суммарного ионного тока: 0.001 = 0,1%.',
    count: 'Сколько самых интенсивных пиков оставить, например 5000.',
  };

  let defaults = {
    peak_picking: 'vendor', ms_levels: '1-', cwt_snr: 1, cwt_peak_space: 0.001,
  };
  const timers = {};
  const seq = {};

  const el = (px, name) => document.getElementById(`${px}-${name}`);

  function html(px) {
    return `
      <div class="field">
        <label class="lbl" for="${px}-algo">Центроидирование</label>
        <select id="${px}-algo">
          <option value="vendor">Vendor — алгоритм прибора</option>
          <option value="cwt">CWT — порог по сигнал/шум</option>
          <option value="none">Нет — профильный спектр</option>
        </select>
        <div class="field__note" id="${px}-algo-note"></div>
      </div>
      <div class="field__row" id="${px}-cwt-row">
        <div class="field">
          <label class="lbl" for="${px}-snr">Сигнал/шум <span class="hint">snr</span></label>
          <input type="number" id="${px}-snr" min="0.1" max="1000" step="any">
        </div>
        <div class="field">
          <label class="lbl" for="${px}-space">Мин. расстояние <span class="hint">m/z</span></label>
          <input type="number" id="${px}-space" min="0.0001" max="10" step="any">
        </div>
      </div>
      <div class="field__row">
        <div class="field">
          <label class="lbl" for="${px}-thr-type">Порог интенсивности</label>
          <select id="${px}-thr-type">
            <option value="">Нет</option>
            <option value="absolute">Абсолютный</option>
            <option value="bpi-relative">Доля от главного пика</option>
            <option value="tic-relative">Доля от TIC</option>
            <option value="count">Число пиков</option>
          </select>
        </div>
        <div class="field">
          <label class="lbl" for="${px}-thr">Значение</label>
          <input type="text" id="${px}-thr" inputmode="decimal" placeholder="2e7" autocomplete="off">
        </div>
      </div>
      <div class="field__note" id="${px}-thr-note" style="margin: -8px 0 12px;"></div>
      <div class="field__row">
        <div class="field">
          <label class="lbl" for="${px}-mzmin">m/z от</label>
          <input type="number" id="${px}-mzmin" min="0" step="any" placeholder="весь диапазон">
        </div>
        <div class="field">
          <label class="lbl" for="${px}-mzmax">m/z до</label>
          <input type="number" id="${px}-mzmax" min="0" step="any" placeholder="весь диапазон">
        </div>
      </div>
      <div class="field__row">
        <div class="field">
          <label class="lbl" for="${px}-levels">Уровни MS <span class="hint">1, 1-, 1-2</span></label>
          <input type="text" id="${px}-levels" autocomplete="off" spellcheck="false">
        </div>
        <div class="field" id="${px}-zero-wrap" style="display:flex; align-items:flex-end; padding-bottom: 11px;">
          <label class="ip-check"><input type="checkbox" id="${px}-keepzero"> Оставить нулевые точки</label>
        </div>
      </div>
      <div class="field">
        <label class="lbl" for="${px}-extra">Доп. фильтры <span class="hint">по одному на строку</span></label>
        <textarea id="${px}-extra" rows="2" spellcheck="false" placeholder="scanTime [0,600]"></textarea>
        <div class="field__note">Любые фильтры msconvert без <code>--filter</code>, например <code>scanTime [0,600]</code>.</div>
      </div>
      <div class="cmd-preview">
        <div class="cmd-preview__head"><span>Команда msconvert</span><span id="${px}-tag"></span></div>
        <code id="${px}-cmd">—</code>
        <div class="cv-cmd-error hidden" id="${px}-cmd-error"></div>
      </div>`;
  }

  function refresh(px) {
    const algo = el(px, 'algo').value;
    el(px, 'algo-note').textContent = ALGO_NOTES[algo] || '';
    el(px, 'cwt-row').classList.toggle('hidden', algo !== 'cwt');
    el(px, 'zero-wrap').style.visibility = algo === 'none' ? 'visible' : 'hidden';
    const thrType = el(px, 'thr-type').value;
    el(px, 'thr').disabled = !thrType;
    if (!thrType) el(px, 'thr').value = '';
    el(px, 'thr-note').textContent = THRESHOLD_NOTES[thrType] || '';
    el(px, 'thr-note').classList.toggle('hidden', !thrType);
  }

  function mount(container, px) {
    container.innerHTML = html(px);
    container.addEventListener('input', () => { refresh(px); schedulePreview(px); });
    container.addEventListener('change', () => { refresh(px); schedulePreview(px); });
    fill(px, defaults);
  }

  function fill(px, c) {
    c = { ...defaults, ...(c || {}) };
    el(px, 'algo').value = c.peak_picking || 'vendor';
    el(px, 'snr').value = c.cwt_snr ?? '';
    el(px, 'space').value = c.cwt_peak_space ?? '';
    el(px, 'thr-type').value = c.threshold_type || '';
    el(px, 'thr').value = c.threshold_type ? fmtNum(c.threshold) : '';
    el(px, 'mzmin').value = c.mz_min || '';
    el(px, 'mzmax').value = c.mz_max || '';
    el(px, 'levels').value = c.ms_levels || '1-';
    el(px, 'keepzero').checked = !!c.keep_zero_samples;
    el(px, 'extra').value = (c.extra_filters || []).join('\n');
    refresh(px);
    schedulePreview(px, 0);
  }

  function num(px, name, label) {
    const v = el(px, name).value.trim().replace(',', '.');
    if (v === '') return undefined;
    const n = Number(v);
    if (!Number.isFinite(n)) throw new Error(`${label}: нужно число`);
    return n;
  }

  // Параметры для API; бросает Error с понятным текстом при неверном числе
  function read(px) {
    const algo = el(px, 'algo').value;
    const thrType = el(px, 'thr-type').value;
    const c = {
      peak_picking: algo,
      ms_levels: el(px, 'levels').value.trim() || undefined,
      extra_filters: el(px, 'extra').value.split('\n').map((s) => s.trim()).filter(Boolean),
    };
    if (algo === 'cwt') {
      c.cwt_snr = num(px, 'snr', 'Сигнал/шум');
      c.cwt_peak_space = num(px, 'space', 'Мин. расстояние');
    }
    if (thrType) {
      c.threshold_type = thrType;
      c.threshold = num(px, 'thr', 'Порог интенсивности');
      if (c.threshold === undefined) throw new Error('Порог интенсивности: укажите значение');
    }
    const mzMin = num(px, 'mzmin', 'm/z от');
    const mzMax = num(px, 'mzmax', 'm/z до');
    if (mzMin) c.mz_min = mzMin;
    if (mzMax) c.mz_max = mzMax;
    if (algo === 'none' && el(px, 'keepzero').checked) c.keep_zero_samples = true;
    if (!c.extra_filters.length) delete c.extra_filters;
    return c;
  }

  function showPreview(px, { command = '', tag = '', error = '' }) {
    el(px, 'cmd').textContent = command || '—';
    el(px, 'tag').textContent = tag ? `кэш ${tag}` : '';
    const errEl = el(px, 'cmd-error');
    errEl.textContent = error;
    errEl.classList.toggle('hidden', !error);
  }

  function schedulePreview(px, delay = 250) {
    clearTimeout(timers[px]);
    timers[px] = setTimeout(() => preview(px), delay);
  }

  async function preview(px) {
    let c;
    try {
      c = read(px);
    } catch (err) {
      showPreview(px, { error: err.message });
      return;
    }
    const my = (seq[px] = (seq[px] || 0) + 1);
    try {
      const res = await fetch('/api/msconvert/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(c),
      });
      if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
      const data = await res.json();
      if (my === seq[px]) showPreview(px, data); // ответ на устаревший запрос не показываем
    } catch (err) {
      if (my === seq[px]) showPreview(px, { error: 'Предпросмотр недоступен: ' + err.message });
    }
  }

  // Краткая подпись для истории прогонов
  function summary(c) {
    if (!c) return '';
    const parts = [];
    if (c.peak_picking === 'cwt') parts.push(`cwt snr=${c.cwt_snr} Δ=${c.cwt_peak_space}`);
    else if (c.peak_picking === 'none') parts.push('профиль');
    else parts.push(c.peak_picking || 'vendor');
    if (c.threshold_type) parts.push(`${c.threshold_type} ${fmtNum(c.threshold)}`);
    if (c.mz_max) parts.push(`m/z ${c.mz_min}–${c.mz_max}`);
    if (c.extra_filters && c.extra_filters.length) parts.push(`+${c.extra_filters.length} фильтр.`);
    return 'msconvert: ' + parts.join(' ');
  }

  // Умолчания сервера (PEAK_PICKING, CWT_SNR…) и проверка MSCONVERT_PATH
  async function loadInfo(px, warnEl) {
    try {
      const res = await fetch('/api/msconvert');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const info = await res.json();
      if (info.defaults) defaults = { ...defaults, ...info.defaults };
      fill(px, defaults);
      if (warnEl) {
        let msg = '';
        if (!info.configured) msg = 'MSCONVERT_PATH не задан: файлы .d/.raw не будут конвертированы.';
        else if (!info.found) msg = `msconvert не найден по пути <code>${escapeHtml(info.path)}</code>.`;
        warnEl.innerHTML = msg;
        warnEl.classList.toggle('hidden', !msg);
      }
    } catch (err) {
      if (warnEl) {
        warnEl.textContent = 'Не удалось получить настройки msconvert: ' + err.message;
        warnEl.classList.remove('hidden');
      }
    }
  }

  // 20000000 → 2e7: большие пороги интенсивности читаются проще
  function fmtNum(n) {
    if (n === undefined || n === null || n === '') return '';
    const v = Number(n);
    if (!Number.isFinite(v)) return String(n);
    return Math.abs(v) >= 1e5 ? v.toExponential().replace('e+', 'e') : String(v);
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]));
  }

  return { mount, fill, read, summary, loadInfo, schedulePreview, getDefaults: () => ({ ...defaults }) };
})();
