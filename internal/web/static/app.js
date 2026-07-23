// Дашборд показаний датчиков. Общается с сервером только через REST API
// (/api/rooms, /api/metrics, /api/latest, /api/readings), без сторонних
// JS-библиотек — график рисуется вручную на canvas.

const REFRESH_MS = 30000;

const roomSelect = document.getElementById('room-select');
const cardsEl = document.getElementById('cards');

let currentRoom = null;

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

// loadRooms загружает список комнат и выбирает первую по умолчанию.
async function loadRooms() {
  const rooms = await fetchJSON('/api/rooms');
  roomSelect.innerHTML = '';
  for (const room of rooms) {
    const opt = document.createElement('option');
    opt.value = room;
    opt.textContent = room;
    roomSelect.appendChild(opt);
  }

  if (rooms.length === 0) {
    cardsEl.innerHTML = '<p>Показаний ещё не поступало.</p>';
    return;
  }

  roomSelect.value = rooms[0];
  await loadRoom(rooms[0]);
}

// loadRoom пересоздаёт карточки метрик для выбранной комнаты и загружает данные.
async function loadRoom(room) {
  currentRoom = room;
  const metrics = await fetchJSON(`/api/metrics?room=${encodeURIComponent(room)}`);
  cardsEl.innerHTML = '';
  for (const metric of metrics) {
    cardsEl.appendChild(buildCard(metric));
  }
  await refreshRoom();
}

// buildCard создаёт разметку карточки одной метрики: текущее значение и график.
function buildCard(metric) {
  const card = document.createElement('section');
  card.className = 'card';
  card.dataset.metric = metric;
  card.innerHTML = `
    <h2>${metric}</h2>
    <div class="value">—</div>
    <canvas width="320" height="120"></canvas>
  `;
  return card;
}

// refreshRoom обновляет текущие значения и графики во всех карточках
// выбранной комнаты, не пересоздавая саму разметку.
async function refreshRoom() {
  if (!currentRoom) {
    return;
  }

  const latest = await fetchJSON(`/api/latest?room=${encodeURIComponent(currentRoom)}`);
  const latestByMetric = Object.fromEntries(latest.map((r) => [r.metric, r]));

  for (const card of cardsEl.querySelectorAll('.card')) {
    const metric = card.dataset.metric;
    const reading = latestByMetric[metric];
    card.querySelector('.value').textContent = reading ? formatValue(reading.value) : '—';

    const buckets = await fetchJSON(
      `/api/readings?room=${encodeURIComponent(currentRoom)}&metric=${encodeURIComponent(metric)}`
    );
    drawChart(card.querySelector('canvas'), buckets);
  }
}

// formatValue округляет значение метрики до одного знака после запятой.
function formatValue(value) {
  return Number(value).toFixed(1);
}

// drawChart рисует простой линейный график по точкам {time, value} на canvas,
// масштабируя значения в диапазон [0, height].
function drawChart(canvas, buckets) {
  const ctx = canvas.getContext('2d');
  const { width, height } = canvas;
  ctx.clearRect(0, 0, width, height);

  if (buckets.length < 2) {
    return;
  }

  const values = buckets.map((b) => b.value);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;

  ctx.beginPath();
  buckets.forEach((bucket, i) => {
    const x = (i / (buckets.length - 1)) * width;
    const y = height - ((bucket.value - min) / range) * height;
    if (i === 0) {
      ctx.moveTo(x, y);
    } else {
      ctx.lineTo(x, y);
    }
  });
  ctx.strokeStyle = getComputedStyle(document.documentElement).getPropertyValue('--accent');
  ctx.lineWidth = 2;
  ctx.stroke();
}

roomSelect.addEventListener('change', () => loadRoom(roomSelect.value));

loadRooms();
setInterval(refreshRoom, REFRESH_MS);
