// Package iaminfra contains shared IAM persistence contracts.
package iaminfra

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// DBTX is implemented by both *sqlx.DB and *sqlx.Tx. Transaction-scoped
// repositories receive their executor explicitly, never through context values.
type DBTX interface {
	sqlx.ExtContext
	GetContext(context.Context, any, string, ...any) error
	SelectContext(context.Context, any, string, ...any) error
	NamedExecContext(context.Context, string, any) (sql.Result, error)
}
