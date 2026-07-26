// Дашборд показаний датчиков. Общается с сервером только через REST API
// (/api/rooms, /api/metrics, /api/latest, /api/readings), без сторонних
// JS-библиотек. Разметка карточек, gauge и спарклайн адаптированы из
// макета design_handoff_mqtt_sensors/ (mqtt-dashboard.js) — алгоритм
// отрисовки перенесён как референс, а не сама логика: там тип датчика
// угадывался по подстроке в произвольном sensorId и данные шли по MQTT
// прямо из браузера, здесь тип определяется по имени metric (оно уже
// осмысленное) и данные идут через REST API этого проекта.

const REFRESH_MS = 30000; // полный опрос rooms/metrics/latest/readings
const RERENDER_MS = 10000; // лёгкая перерисовка (относительное время, offline) без сети
const OFFLINE_MS = 3 * 60 * 1000; // нет показаний дольше — карточка считается "нет данных"
const HISTORY_POINTS = 40; // сколько последних точек показывать на спарклайне

// ICONS — иконки в стиле Lucide (inline SVG-path), как в макете дашборда.
const ICONS = {
  temperature: '<path d="M14 4v10.54a4 4 0 1 1-4 0V4a2 2 0 0 1 4 0Z"/>',
  humidity: '<path d="M12 22a7 7 0 0 0 7-7c0-2-1-3.9-3-5.5s-3.5-4-4-6.5c-.5 2.5-2 4.9-4 6.5C6 11.1 5 13 5 15a7 7 0 0 0 7 7Z"/>',
  co2: '<path d="M17.7 7.7a2.5 2.5 0 1 1 1.8 4.3H2"/><path d="M9.6 4.6A2 2 0 1 1 11 8H2"/><path d="M12.6 19.4A2 2 0 1 0 14 16H2"/>',
  pressure: '<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>',
  lux: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/>',
  door: '<path d="M3 21h18"/><path d="M6 21V4a1 1 0 0 1 1-1h7l5 4v14"/><circle cx="12.5" cy="12" r=".7" fill="currentColor" stroke="none"/>',
  generic: '<circle cx="12" cy="12" r="9"/><path d="M12 8v4l3 2"/>',
};

// icon строит inline SVG заданного размера по имени из ICONS.
function icon(name, size) {
  size = size || 16;
  return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${ICONS[name] || ICONS.generic}</svg>`;
}

// TYPES — известные типы метрик: единица измерения, диапазон для gauge,
// точность и иконка. keywords — запасной способ определить тип по имени
// метрики, если оно не совпадает с ключом точно (например "temp_outdoor").
// generic — метрика вне этого списка всё равно рисуется (открытое множество
// метрик, см. CLAUDE.md), просто с нейтральным диапазоном 0..100.
const TYPES = {
  temperature: { label: 'Температура', unit: '°C', min: -10, max: 40, decimals: 1, icon: 'temperature', keywords: ['temp', 'температ'] },
  humidity: { label: 'Влажность', unit: '%', min: 0, max: 100, decimals: 0, icon: 'humidity', keywords: ['hum', 'влажн'] },
  co2: { label: 'CO₂', unit: 'ppm', min: 400, max: 2000, decimals: 0, icon: 'co2', keywords: ['co2', 'gas', 'air', 'возд', 'газ'] },
  pressure: { label: 'Давление', unit: 'гПа', min: 950, max: 1050, decimals: 0, icon: 'pressure', keywords: ['pres', 'давлен'] },
  lux: { label: 'Освещённость', unit: 'лк', min: 0, max: 1000, decimals: 0, icon: 'lux', keywords: ['lux', 'light', 'люкс', 'освещ'] },
  door: { label: 'Дверь/окно', unit: '', binary: true, icon: 'door', keywords: ['door', 'window', 'contact', 'окно', 'двер'] },
  generic: { label: 'Датчик', unit: '', min: 0, max: 100, decimals: 1, icon: 'generic', keywords: [] },
};

// detectType определяет тип метрики: сначала точное совпадение с ключом
// TYPES (наш backend отдаёт понятные имена вроде "temperature"), затем
// поиск по ключевым словам как задел на датчики с более сложными именами,
// иначе — generic. Датчиков типа "дверь/окно" в проекте пока нет физически,
// но код их уже умеет рисовать — задел на будущее без правок здесь.
function detectType(metric) {
  const m = (metric || '').toLowerCase();
  if (TYPES[m]) {
    return m;
  }
  for (const key of Object.keys(TYPES)) {
    if (key === 'generic') continue;
    if (TYPES[key].keywords.some((k) => m.includes(k))) return key;
  }
  return 'generic';
}

let rooms = []; // список комнат от /api/rooms
let sensors = new Map(); // ключ `${room}/${metric}` -> {room,metric,typeKey,value,time,history[]}
let currentRoom = '__all__';

const roomTabsEl = document.getElementById('room-tabs');
const metaLineEl = document.getElementById('meta-line');
const cardsEl = document.getElementById('cards');

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

// refreshAll полностью перечитывает список комнат, метрик и показаний с
// сервера и перестраивает карту sensors с нуля (чтобы пропавшие метрики
// не оставались в карте бесконечно).
async function refreshAll() {
  rooms = await fetchJSON('/api/rooms');
  const next = new Map();

  for (const room of rooms) {
    const metrics = await fetchJSON(`/api/metrics?room=${encodeURIComponent(room)}`);
    const latest = await fetchJSON(`/api/latest?room=${encodeURIComponent(room)}`);
    const latestByMetric = Object.fromEntries(latest.map((r) => [r.metric, r]));

    for (const metric of metrics) {
      const buckets = await fetchJSON(
        `/api/readings?room=${encodeURIComponent(room)}&metric=${encodeURIComponent(metric)}`
      );
      const history = buckets.slice(-HISTORY_POINTS).map((b) => b.value);
      const reading = latestByMetric[metric];

      next.set(`${room}/${metric}`, {
        room,
        metric,
        typeKey: detectType(metric),
        value: reading ? reading.value : null,
        time: reading ? new Date(reading.time).getTime() : 0,
        history,
      });
    }
  }

  sensors = next;
  renderRoomTabs();
  render();
}

// renderRoomTabs перестраивает вкладки комнат ("Все комнаты" + по одной на
// каждую известную комнату) и вешает обработчик переключения — само
// переключение только фильтрует уже загруженные данные, без обращения к серверу.
function renderRoomTabs() {
  const list = ['__all__', ...rooms.slice().sort()];
  if (!list.includes(currentRoom)) {
    currentRoom = '__all__';
  }

  roomTabsEl.innerHTML = list
    .map(
      (r) =>
        `<label class="seg-opt"><input type="radio" name="roomtab" value="${r}" ${r === currentRoom ? 'checked' : ''}><span>${r === '__all__' ? 'Все комнаты' : r}</span></label>`
    )
    .join('');

  roomTabsEl.querySelectorAll('input[name="roomtab"]').forEach((input) => {
    input.addEventListener('change', (e) => {
      currentRoom = e.target.value;
      render();
    });
  });
}

// render отрисовывает сетку карточек для текущей выбранной комнаты и строку
// "N из M датчиков в сети". Не обращается к серверу — работает по уже
// загруженным данным из sensors.
function render() {
  const list = Array.from(sensors.values())
    .filter((e) => currentRoom === '__all__' || e.room === currentRoom)
    .sort((a, b) => a.room.localeCompare(b.room) || a.metric.localeCompare(b.metric));

  if (list.length === 0) {
    cardsEl.innerHTML =
      '<div class="empty"><h3>Нет показаний</h3><p class="text-muted">Показаний ещё не поступало.</p></div>';
  } else {
    cardsEl.innerHTML = `<div class="grid">${list.map(cardHtml).join('')}</div>`;
  }

  const total = sensors.size;
  const onlineCount = Array.from(sensors.values()).filter((e) => Date.now() - e.time <= OFFLINE_MS).length;
  metaLineEl.textContent = total ? `${onlineCount} из ${total} датчиков в сети` : '—';
}

// cardHtml строит разметку одной карточки датчика: числовые метрики — gauge
// + спарклайн, бинарные ("дверь/окно") — состояние + полоска истории.
function cardHtml(entry) {
  const def = TYPES[entry.typeKey];
  const offline = entry.time === 0 || Date.now() - entry.time > OFFLINE_MS;

  let body;
  if (def.binary) {
    const isOpen = entry.value !== null && entry.value !== 0;
    const strip = entry.history
      .slice(-24)
      .map((v) => `<i class="${v ? 'on' : ''}"></i>`)
      .join('');
    body = `<div class="binary-state">${icon(def.icon, 28)}<div class="label">${entry.value === null ? '—' : isOpen ? 'Открыто' : 'Закрыто'}</div></div><div class="binary-strip">${strip || '<i></i>'}</div>`;
  } else {
    body = `<div class="gauge-wrap">${gaugeSvg(entry.value, def.min, def.max, def.decimals, def.unit)}</div>${sparklineSvg(entry.history, def.min, def.max)}`;
  }

  return `<div class="card sensor-card${offline ? ' offline' : ''}">
<div class="card-top">
<div class="card-id"><span>${icon(def.icon, 18)}</span><div class="names"><div class="card-kicker">${entry.room}</div><div class="card-title">${def.label}</div></div></div>
<span class="tag ${offline ? 'tag-neutral' : 'tag-accent'} status-tag"><span class="dot"></span>${offline ? 'нет данных' : 'в сети'}</span>
</div>
${body}
<div class="card-meta"><span class="topic">home/${entry.room}/${entry.metric}</span><span>${relTime(entry.time)}</span></div>
</div>`;
}

// polar переводит полярные координаты (центр, радиус, угол в градусах,
// 0° сверху) в декартовы — вспомогательная функция для дуги gauge.
function polar(cx, cy, r, angle) {
  const a = ((angle - 90) * Math.PI) / 180;
  return { x: cx + r * Math.cos(a), y: cy + r * Math.sin(a) };
}

// arcPath строит SVG path дуги окружности от угла start до угla end.
function arcPath(cx, cy, r, start, end) {
  const s = polar(cx, cy, r, end);
  const e = polar(cx, cy, r, start);
  const large = end - start <= 180 ? '0' : '1';
  return `M ${s.x} ${s.y} A ${r} ${r} 0 ${large} 0 ${e.x} ${e.y}`;
}

// gaugeSvg рисует круговой индикатор (дуга 270°, от -135° до 135°) с
// текущим значением по центру: серая дуга-подложка на весь диапазон,
// поверх неё — акцентная дуга на долю value в [min, max].
function gaugeSvg(value, min, max, decimals, unit) {
  const cx = 60;
  const cy = 64;
  const r = 48;
  const sw = 8;
  const START = -135;
  const END = 135;
  const frac = Math.max(0, Math.min(1, (value - min) / (max - min)));
  const valAngle = START + frac * (END - START);
  const bg = arcPath(cx, cy, r, START, END);
  const fg = arcPath(cx, cy, r, START, valAngle);
  const shown = value == null ? '—' : value.toFixed(decimals);
  return `<svg viewBox="0 0 120 100">
<path d="${bg}" fill="none" stroke="var(--color-divider)" stroke-width="${sw}" stroke-linecap="round"/>
${value != null ? `<path d="${fg}" fill="none" stroke="var(--color-accent)" stroke-width="${sw}" stroke-linecap="round"/>` : ''}
</svg><div class="gauge-readout"><div class="val">${shown}</div><div class="unit">${unit}</div></div>`;
}

// sparklineSvg рисует мини-график последних показаний без осей и подписей —
// значения нормализуются в диапазон [min, max] метрики (тот же, что у gauge).
function sparklineSvg(history, min, max) {
  if (!history || history.length < 2) {
    return '<svg class="spark" viewBox="0 0 100 30"></svg>';
  }
  const n = history.length;
  const pts = history
    .map((v, i) => {
      const x = n === 1 ? 0 : (i / (n - 1)) * 100;
      const f = Math.max(0, Math.min(1, (v - min) / (max - min)));
      const y = 28 - f * 26;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
  return `<svg class="spark" viewBox="0 0 100 30" preserveAspectRatio="none"><polyline points="${pts}" fill="none" stroke="var(--color-accent-700)" stroke-width="1.5" vector-effect="non-scaling-stroke"/></svg>`;
}

// relTime форматирует момент последнего показания как относительное время
// ("5 с назад", "3 мин назад") для футера карточки.
function relTime(ms) {
  if (!ms) return 'нет данных';
  const s = Math.round((Date.now() - ms) / 1000);
  if (s < 5) return 'только что';
  if (s < 60) return `${s} с назад`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} мин назад`;
  const h = Math.round(m / 60);
  return `${h} ч назад`;
}

refreshAll();
setInterval(refreshAll, REFRESH_MS);
setInterval(render, RERENDER_MS);
