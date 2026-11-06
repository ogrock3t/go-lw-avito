# Trip Service

HTTP-сервис на Go для управления поездками. Сервис создает поездки, возвращает их по `id` и завершает поездки. Данные хранятся в PostgreSQL.

## Архитектура

Код разделён на слои и пакеты по действиям:

| Пакет | Назначение |
|---|---|
| `cmd/trip-service` | Чтение конфигурации, запуск сервера и обработка сигналов остановки |
| `internal/config` | Загрузка и валидация переменных окружения |
| `internal/container` | Создание конкретных реализаций и связывание зависимостей |
| `internal/domain/trip` | Сущности, статусы, доменные ошибки и переход состояния через `Trip.Finish` |
| `internal/model` | Структуры передачи данных между слоями |
| `internal/usecase/trip/{create,get,finish}` | Сценарии создания, получения и завершения поездки |
| `internal/http` | HTTP-обработчики, валидация, преобразование ответов и middleware |
| `internal/http/domainerror` | Единый маппинг доменных ошибок в HTTP Problem |
| `internal/repository/trip` | SQL-запросы и преобразование данных PostgreSQL |
| `internal/infra` | Пул PostgreSQL, менеджер транзакций и OpenTelemetry |
| `internal/generated` | Типы и серверные интерфейсы, сгенерированные из OpenAPI |

HTTP-обработчики вызывают usecase через интерфейсы. Usecase использует доменные
модели и узкие интерфейсы репозитория, менеджера транзакций и бизнес-метрик.
Конкретные реализации находятся в repository и infra. Домен не зависит от
HTTP, PostgreSQL и OpenTelemetry; usecase не зависит от транспорта и драйвера БД.

Интерфейсы объявлены в `contract.go` пакета-потребителя. Каждый обработчик и
usecase определяет только нужные ему методы, поэтому зависимости остаются
узкими, а реализации можно подменять в тестах. Моки генерируются через
`go:generate` в `mocks_test.go` рядом с тестируемым кодом.

Все зависимости собираются вручную в `internal/container`. Main читает
конфигурацию, вызывает `container.New`, запускает сервер и обрабатывает
остановку приложения.

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
make test            # unit-тесты с race detector и итоговым покрытием
make test-integration # интеграционные тесты репозитория и telemetry пула с PostgreSQL
```

`make test` сохраняет профиль покрытия в `coverage.out` и в конце печатает
суммарное покрытие. Пакеты `internal/domain/...` и `internal/usecase/...`
покрываются этим же запуском.

## Тестирование

Unit-тесты запускаются без PostgreSQL:

```bash
make test
```

Команда запускает тесты с `-race`, сохраняет профиль в `coverage.out` и печатает
общее покрытие. Репозитории и менеджер транзакций в unit-тестах usecase
подменяются через gomock. Для проверки требования покрытия domain/usecase не
ниже 50% отдельно по пакетам:

```bash
go test -race -cover ./internal/domain/... ./internal/usecase/...
```

Integration-тесты используют реальный PostgreSQL из окружения курса. Перед
запуском нужно поднять окружение и применить миграции:

```bash
tripgoctl cluster start
tripgoctl environment start
make migrate
make test-integration
```

`make test-integration` загружает `DATABASE_URL` из `.env` и запускает тесты
репозитория и инструментированного пула с тегом `integration`, флагами `-race`
и `-count=2`. Тесты проверяют операции с поездками, откат транзакции при ошибке
вставки истории и DB telemetry. Они создают и удаляют собственные данные;
тест пула использует временную таблицу. Запускать Go-сервис для тестов не нужно.

Проверка линтером выполняется командой `make lint`.

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
| `OTEL_SERVICE_NAME` | Имя сервиса в telemetry, например `trip-service` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Базовый адрес коллектора из `.env`, созданного `tripgoctl` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | Протокол экспорта; поддерживается только `http/protobuf` |
| `OTEL_RESOURCE_ATTRIBUTES` | Необязательные атрибуты ресурса в формате `ключ=значение` через запятую |
| `OTEL_TRACES_SAMPLER` | Политика сэмплирования: `always_on`, `always_off`, `parentbased_always_on` или `parentbased_always_off` |

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

Пул инструментирован через `otelpgx`: запросы, в том числе внутри транзакций,
создают дочерние спаны текущего HTTP-трейса. Histogram
`db.client.operation.duration` записывает длительность в секундах и работает
независимо от trace sampling. Атрибуты запросов включают `db.operation.name`,
`db.collection.name` для таблиц `trips` и `trip_status_history`, а при ошибке —
`error.type` (SQLSTATE для ошибок PostgreSQL). SQL-текст и параметры не
записываются в атрибуты спанов. Integration-тест пула использует временную
таблицу в отдельной сессии и не меняет данные приложения.

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

Бизнес-счётчики `tripgo.trips.created` и `tripgo.trips.completed` увеличиваются
только после успешного commit соответствующей операции. Ошибки репозитория,
занятый водитель, повторное завершение и ошибки commit не увеличивают их.
Счётчики не имеют лейблов с идентификаторами поездок или пользователей.

`CreateTrip` генерирует `id` на стороне сервиса, выставляет статус `active`, `started_at = now()` и `finished_at = NULL`.

В одной транзакции выполняются `INSERT` в `trips` и `INSERT` в
`trip_status_history`.

### Завершение поездки

Завершение выполняется атомарным `UPDATE ... WHERE id = $1 AND status = 'active'`.  Если строка не обновилась, дополнительным чтением различаются `404 trip_not_found` и `409 trip_completed`.
