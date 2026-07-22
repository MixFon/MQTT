// Package migrations встраивает .sql-файлы миграций, чтобы бинарник
// нёс схему БД в себе, без отдельного инструмента миграций.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
