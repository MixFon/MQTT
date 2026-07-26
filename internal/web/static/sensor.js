// Детальный график одного датчика (room+metric), открывается по клику на
// карточку из дашборда (index.html/app.js). Читает room/metric из query-строки
// адреса, дальше работает как отдельная страница — свой период и шаг
// агрегации, поверх того же REST API (/api/readings), что и главный дашборд.
// Таблица типов метрик (TYPES/detectType/icon) — общая, из metrics.js.

// INTERVALS — доступные шаги агрегации. Список фиксированный (не свободный
// ввод), поэтому минимум в 30 секунд соблюдается структурно — меньшего
// варианта в списке просто нет. Формат value — то, что понимает Postgres
// INTERVAL и валидатор на сервере (internal/api/handlers.go).
const INTERVALS = [
  { value: '30 seconds', label: '30 секунд', seconds: 30 },
  { value: '1 minute', label: '1 минута', seconds: 60 },
  { value: '5 minutes', label: '5 минут', seconds: 300 },
  { value: '15 minutes', label: '15 минут', seconds: 900 },
  { value: '30 minutes', label: '30 минут', seconds: 1800 },
  { value: '1 hour', label: '1 час', seconds: 3600 },
  { value: '3 hours', label: '3 часа', seconds: 10800 },
  { value: '6 hours', label: '6 часов', seconds: 21600 },
  { value: '12 hours', label: '12 часов', seconds: 43200 },
  { value: '1 day', label: '1 день', seconds: 86400 },
];

// RANGE_PRESETS — готовые периоды; "custom" — произвольный, задаётся полями
// from-input/to-input. defaultIntervalSeconds — шаг, который подставляется
// при выборе пресета (пользователь может изменить его вручную после).
const RANGE_PRESETS = [
  { value: '24h', label: '24 часа', ms: 24 * 3600 * 1000, defaultIntervalSeconds: 300 },
  { value: 'week', label: 'Неделя', ms: 7 * 24 * 3600 * 1000, defaultIntervalSeconds: 3600 },
  { value: 'custom', label: 'Свой период', ms: null, defaultIntervalSeconds: null },
];

const AUTO_REFRESH_MS = 30000; // для 24ч/недели — период, привязанный к "сейчас", обновляем как на дашборде
const MAX_POINTS_HINT = 400; // ориентир при автоподборе шага под произвольный период

const params = new URLSearchParams(location.search);
const room = params.get('room');
const metric = params.get('metric');

const detailHeadEl = document.getElementById('detail-head');
const statusEl = document.getElementById('detail-status');

if (!room || !metric) {
  detailHeadEl.innerHTML = '<h1>Датчик не указан</h1>';
  document.querySelector('.detail-controls').remove();
  statusEl.textContent = 'В адресе страницы должны быть параметры room и metric.';
} else {
  initDetailPage(room, metric);
}

// initDetailPage разворачивает страницу для конкретной пары room/metric:
// строит шапку, вкладки периода, список шагов, вешает обработчики и
// запускает первую загрузку данных.
function initDetailPage(room, metric) {
  const def = TYPES[detectType(metric)];
  document.title = `${def.label} — ${room}`;
  detailHeadEl.innerHTML = `${icon(def.icon, 22)}<div><div class="kicker">${room}</div><h1>${def.label}</h1></div>`;

  const rangeTabsEl = document.getElementById('range-tabs');
  const customRangeEl = document.getElementById('custom-range');
  const fromInputEl = document.getElementById('from-input');
  const toInputEl = document.getElementById('to-input');
  const applyRangeBtn = document.getElementById('apply-range');
  const intervalSelectEl = document.getElementById('interval-select');
  const chartCanvas = document.getElementById('detail-chart');

  let rangeMode = '24h';
  let lastBuckets = [];
  let autoRefreshTimer = null;
  let resizeTimer = null;

  renderRangeTabs();
  renderIntervalOptions();
  setIntervalBySeconds(RANGE_PRESETS[0].defaultIntervalSeconds);

  rangeTabsEl.querySelectorAll('input[name="range"]').forEach((input) => {
    input.addEventListener('change', (e) => setRangeMode(e.target.value));
  });
  intervalSelectEl.addEventListener('change', refetch);
  applyRangeBtn.addEventListener('click', () => {
    const { from, to } = currentRange();
    setIntervalBySeconds(suggestIntervalSeconds(to.getTime() - from.getTime()));
    refetch();
  });
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => drawAxisChart(chartCanvas, lastBuckets, def), 150);
  });

  refetch();

  // renderRangeTabs строит сегментированный переключатель периода
  // ("24 часа" / "Неделя" / "Свой период").
  function renderRangeTabs() {
    rangeTabsEl.innerHTML = RANGE_PRESETS.map(
      (p) => `<label class="seg-opt"><input type="radio" name="range" value="${p.value}" ${p.value === rangeMode ? 'checked' : ''}><span>${p.label}</span></label>`
    ).join('');
  }

  // renderIntervalOptions заполняет select доступными шагами агрегации.
  function renderIntervalOptions() {
    intervalSelectEl.innerHTML = INTERVALS.map((iv) => `<option value="${iv.value}">${iv.label}</option>`).join('');
  }

  // setIntervalBySeconds выбирает в select пункт с ближайшим известным
  // значением seconds (используется при переключении периода).
  function setIntervalBySeconds(seconds) {
    const match = INTERVALS.find((iv) => iv.seconds === seconds) || INTERVALS[2];
    intervalSelectEl.value = match.value;
  }

  // suggestIntervalSeconds подбирает шаг так, чтобы на график не пришлось
  // больше ~MAX_POINTS_HINT точек — иначе линия становится нечитаемой, а
  // ответ сервера — избыточно большим для домашнего датчика.
  function suggestIntervalSeconds(spanMs) {
    const spanSeconds = Math.max(1, spanMs / 1000);
    for (const iv of INTERVALS) {
      if (spanSeconds / iv.seconds <= MAX_POINTS_HINT) return iv.seconds;
    }
    return INTERVALS[INTERVALS.length - 1].seconds;
  }

  // setRangeMode переключает период: показывает/скрывает поля произвольного
  // периода, подставляет дефолтный шаг для пресета и запускает (24ч/неделя)
  // или останавливает (свой период) автообновление.
  function setRangeMode(mode) {
    rangeMode = mode;
    customRangeEl.classList.toggle('hidden', mode !== 'custom');

    if (mode === 'custom') {
      stopAutoRefresh();
      if (!fromInputEl.value) {
        const now = new Date();
        fromInputEl.value = toLocalInputValue(new Date(now.getTime() - 24 * 3600 * 1000));
        toInputEl.value = toLocalInputValue(now);
      }
    } else {
      const preset = RANGE_PRESETS.find((p) => p.value === mode);
      setIntervalBySeconds(preset.defaultIntervalSeconds);
      startAutoRefresh();
    }
    refetch();
  }

  // currentRange вычисляет действующие from/to для текущего режима периода.
  function currentRange() {
    const now = new Date();
    if (rangeMode === '24h') return { from: new Date(now.getTime() - RANGE_PRESETS[0].ms), to: now };
    if (rangeMode === 'week') return { from: new Date(now.getTime() - RANGE_PRESETS[1].ms), to: now };
    const from = fromInputEl.value ? new Date(fromInputEl.value) : new Date(now.getTime() - 24 * 3600 * 1000);
    const to = toInputEl.value ? new Date(toInputEl.value) : now;
    return { from, to };
  }

  // refetch запрашивает /api/readings под текущий период/шаг и перерисовывает график.
  async function refetch() {
    const { from, to } = currentRange();
    if (from >= to) {
      statusEl.textContent = 'Начало периода должно быть раньше конца.';
      return;
    }

    statusEl.textContent = 'Загрузка…';
    const url = `/api/readings?room=${encodeURIComponent(room)}&metric=${encodeURIComponent(metric)}&from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}&interval=${encodeURIComponent(intervalSelectEl.value)}`;
    try {
      const buckets = await fetchJSON(url);
      lastBuckets = buckets;
      drawAxisChart(chartCanvas, buckets, def);
      statusEl.textContent = buckets.length
        ? `${buckets.length} точек · обновлено ${nowTimeStr()}`
        : 'Нет показаний за выбранный период.';
    } catch (err) {
      statusEl.textContent = `Ошибка: ${err.message}`;
    }
  }

  // startAutoRefresh/stopAutoRefresh — для пресетов, привязанных к "сейчас"
  // (24ч/неделя), периодически подтягиваем свежие данные, как на дашборде.
  // Для произвольного периода в прошлом автообновление не нужно.
  function startAutoRefresh() {
    stopAutoRefresh();
    autoRefreshTimer = setInterval(refetch, AUTO_REFRESH_MS);
  }
  function stopAutoRefresh() {
    if (autoRefreshTimer) {
      clearInterval(autoRefreshTimer);
      autoRefreshTimer = null;
    }
  }
}

// fetchJSON запрашивает JSON-эндпоинт и бросает ошибку с текстом из {"error": "..."},
// если сервер ответил не 2xx.
async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return res.json();
}

// toLocalInputValue форматирует Date в значение для <input type="datetime-local">
// в локальном времени браузера (без таймзоны, как того требует сам input).
function toLocalInputValue(date) {
  const pad = (n) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// nowTimeStr — текущее время HH:MM:SS для строки статуса под графиком.
function nowTimeStr() {
  const d = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// formatTick форматирует подпись времени на горизонтальной оси — компактнее
// при большом периоде (не имеет смысла показывать секунды на графике за неделю).
function formatTick(ms, spanMs) {
  const d = new Date(ms);
  const pad = (n) => String(n).padStart(2, '0');
  if (spanMs <= 26 * 3600 * 1000) {
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }
  if (spanMs <= 32 * 24 * 3600 * 1000) {
    return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}`;
  }
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${String(d.getFullYear()).slice(2)}`;
}

// drawAxisChart рисует линейный график с осями на canvas: слева — значения
// метрики (сетка + подписи), снизу — время (подписи меток), поверх — сама
// линия показаний. В отличие от спарклайна на дашборде, это полноценный
// график с масштабом, а не миниатюра.
function drawAxisChart(canvas, buckets, def) {
  const cssWidth = canvas.clientWidth || canvas.width || 600;
  const cssHeight = canvas.clientHeight || canvas.height || 320;
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(cssWidth * dpr);
  canvas.height = Math.round(cssHeight * dpr);

  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, cssWidth, cssHeight);

  const style = getComputedStyle(document.documentElement);
  const colorDivider = style.getPropertyValue('--color-divider').trim();
  const colorAccent = style.getPropertyValue('--color-accent').trim();
  const colorMuted = `color-mix(in srgb, ${style.getPropertyValue('--color-text').trim()} 55%, transparent)`;
  const fontBody = style.getPropertyValue('--font-body').trim();

  ctx.font = `11px ${fontBody}`;

  if (!buckets || buckets.length === 0) {
    ctx.fillStyle = colorMuted;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('Нет данных за выбранный период', cssWidth / 2, cssHeight / 2);
    return;
  }

  const padLeft = 52;
  const padRight = 12;
  const padTop = 12;
  const padBottom = 28;
  const plotW = Math.max(10, cssWidth - padLeft - padRight);
  const plotH = Math.max(10, cssHeight - padTop - padBottom);

  const values = buckets.map((b) => b.value);
  let min = Math.min(...values);
  let max = Math.max(...values);
  if (min === max) {
    min -= 1;
    max += 1;
  }
  const valuePad = (max - min) * 0.1;
  min -= valuePad;
  max += valuePad;

  const times = buckets.map((b) => new Date(b.time).getTime());
  const tMin = times[0];
  const tMax = times[times.length - 1];
  const tSpan = Math.max(1, tMax - tMin);

  // горизонтальные линии сетки + подписи значений слева
  const yTicks = 4;
  ctx.strokeStyle = colorDivider;
  ctx.lineWidth = 1;
  ctx.fillStyle = colorMuted;
  ctx.textAlign = 'right';
  ctx.textBaseline = 'middle';
  for (let i = 0; i <= yTicks; i++) {
    const v = min + ((max - min) * i) / yTicks;
    const y = padTop + plotH - (plotH * i) / yTicks;
    ctx.beginPath();
    ctx.moveTo(padLeft, y);
    ctx.lineTo(padLeft + plotW, y);
    ctx.stroke();
    ctx.fillText(v.toFixed(def.decimals || 0), padLeft - 8, y);
  }

  // подписи времени снизу
  const xTicks = Math.min(6, buckets.length - 1) || 1;
  ctx.textAlign = 'center';
  ctx.textBaseline = 'top';
  for (let i = 0; i <= xTicks; i++) {
    const t = tMin + (tSpan * i) / xTicks;
    const x = padLeft + (plotW * i) / xTicks;
    ctx.fillText(formatTick(t, tSpan), x, padTop + plotH + 8);
  }

  // сама линия показаний
  ctx.beginPath();
  buckets.forEach((b, i) => {
    const x = padLeft + (plotW * (times[i] - tMin)) / tSpan;
    const y = padTop + plotH - (plotH * (b.value - min)) / (max - min);
    if (i === 0) ctx.moveTo(x, y);
    else ctx.lineTo(x, y);
  });
  ctx.strokeStyle = colorAccent;
  ctx.lineWidth = 2;
  ctx.stroke();
}
