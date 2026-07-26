// Общая для дашборда (app.js) и детального графика (sensor.js) таблица типов
// метрик и иконок. Подключается тегом <script> перед основным скриптом
// страницы — общий scope без модулей и сборки, как и весь остальной фронтенд.

// ICONS — иконки в стиле Lucide (inline SVG-path).
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
