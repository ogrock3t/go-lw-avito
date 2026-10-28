# Trip Service

HTTP-сервис на Go для управления поездками. Сервис создает поездки, возвращает их по `id` и завершает поездки. Данные хранятся в PostgreSQL.

## Требования

- Go 1.26
- `tripgoctl`
- PostgreSQL

## Быстрый запуск

```bash
tripgoctl cluster start
tripgoctl environment start
```

`tripgoctl environment start` создает локальный `.env`. Файл `.env` не
коммитится. Пример переменных лежит в `.env.example`.

```bash
make migrate
make run
```

## Makefile

```bash
make generate        # сгенерировать OpenAPI-код
make migrate         # алиас на migrate-up
make migrate-up      # применить миграции
make migrate-down    # откатить последнюю миграцию
make migrate-status  # показать статус миграций
make run             # запустить сервис
make test            # go test -race ./...
```

## Переменные окружения

| Переменная | Описание |
|---|---|
| `HTTP_ADDR` | Адрес HTTP-сервера |
| `HTTP_READ_TIMEOUT` | Таймаут чтения запроса |
| `HTTP_READ_HEADER_TIMEOUT` | Таймаут чтения заголовков |
| `HTTP_WRITE_TIMEOUT` | Таймаут записи ответа |
| `HTTP_IDLE_TIMEOUT` | Таймаут неактивного соединения |
| `LOG_LEVEL` | Уровень логирования |
| `SHUTDOWN_TIMEOUT` | Таймаут graceful shutdown |
| `DATABASE_URL` | Строка подключения к PostgreSQL |
| `DATABASE_MAX_CONNS` | Максимум соединений в пуле |
| `DATABASE_MIN_CONNS` | Минимум соединений в пуле |
| `DATABASE_MAX_CONN_LIFETIME` | Время жизни соединения |
| `DATABASE_CONNECT_TIMEOUT` | Таймаут подключения к БД |
| `DATABASE_QUERY_TIMEOUT` | Таймаут SQL-запросов |

## Реализованные ручки

| Метод | Путь | Что делает |
|---|---|---|
| `POST` | `/api/v1/trips` | Создает поездку |
| `GET` | `/api/v1/trips/{tripId}` | Возвращает поездку по `tripId` |
| `POST` | `/api/v1/trips/{tripId}/finish` | Завершает активную поездку |
| `GET` | `/health` | Liveness probe: возвращает `200`, пока процесс жив, без обращения к БД |
| `GET` | `/ready` | Readiness probe: проверяет PostgreSQL и возвращает `200`, если БД доступна, иначе `503` |

Ошибки API возвращаются в формате `application/problem+json`.

## Принятые решения

### Работа с БД

Подключение к PostgreSQL сделано через `pgxpool`. На старте сервис выполняет `Ping`; если БД недоступна, сервис не запускается.

SQL строится через `squirrel` с placeholders `$1`, `$2`.

### Корректное завершение работы

Сервис обрабатывает `SIGINT` и `SIGTERM`: перестает принимать новые HTTP-запросы, ждет завершения активных запросов в пределах `SHUTDOWN_TIMEOUT` и закрывает пул PostgreSQL.

### Менеджер транзакций

Менеджер транзакций реализует интерфейс:

```go
type TxManager interface {
    Do(ctx context.Context, fn func(ctx context.Context) error) error
}
```

`Do` открывает транзакцию, кладёт `pgx.Tx` в `context` и вызывает callback с
новым context. Если callback возвращает ошибку или паникует, выполняется
`ROLLBACK`. Если callback завершился успешно, выполняется `COMMIT`.

Repository не принимает транзакцию аргументом. Он достаёт executor из context:
если транзакция есть, запрос выполняется через неё; если нет — через
`pgxpool.Pool`.

Вложенный `Do` переиспользует уже существующую транзакцию.

### Уровень изоляции

Для транзакций выбран `ReadCommitted`.

Этого достаточно для лабораторной, потому что конкурентные инварианты защищены
на уровне SQL-операций и ограничений БД:

- запрет двух активных поездок на одного водителя обеспечивается partial unique
  index;
- завершение поездки выполняется атомарным `UPDATE ... WHERE status = 'active'`.

### Запрет двух активных поездок

В БД создан partial unique index:

```sql
CREATE UNIQUE INDEX one_active_trip_per_driver
ON trips (driver_id)
WHERE status = 'active';
```

PostgreSQL гарантирует это ограничение при конкурентных запросах. Ошибка PostgreSQL `23505` преобразуется в доменную ошибку `driver_busy`, которая на HTTP слое возвращается как `409`.

### Создание поездки

`CreateTrip` генерирует `id` на стороне сервиса, выставляет статус `active`, `started_at = now()` и `finished_at = NULL`.

В одной транзакции выполняются `INSERT` в `trips` и `INSERT` в
`trip_status_history`.

### Завершение поездки

Завершение выполняется атомарным `UPDATE ... WHERE id = $1 AND status = 'active'`.  Если строка не обновилась, дополнительным чтением различаются `404 trip_not_found` и `409 trip_completed`.