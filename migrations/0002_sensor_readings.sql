-- narrow-схема: одна строка на метрику, а не колонка на метрику.
-- Новый тип датчика — новое значение metric, без ALTER TABLE.
CREATE TABLE sensor_readings (
    time   TIMESTAMPTZ NOT NULL,
    room   TEXT NOT NULL,
    metric TEXT NOT NULL,
    value  REAL NOT NULL
);

SELECT create_hypertable('sensor_readings', 'time');

CREATE INDEX ON sensor_readings (room, metric, time DESC);
