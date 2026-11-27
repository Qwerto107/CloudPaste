package server

import (
	"database/sql"

	"cloudpaste/backend/internal/audit"
)

func openAuditDatabase(dir string) (*sql.DB, error) {
	return audit.OpenDatabase(dir)
}
