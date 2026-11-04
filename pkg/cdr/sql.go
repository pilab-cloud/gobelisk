package cdr

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SQLExecutor is satisfied by *sql.DB, *sql.Tx and compatible wrappers.
type SQLExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// SQLDialect selects the placeholder syntax used by the generated INSERT.
type SQLDialect int

const (
	// SQLDialectQuestion uses ? placeholders (SQLite, MySQL, MariaDB).
	SQLDialectQuestion SQLDialect = iota
	// SQLDialectPostgres uses $1, $2, ... placeholders (PostgreSQL).
	SQLDialectPostgres
)

// SQLExporter inserts CDR rows into a database table.
//
// The default INSERT expects these columns:
//
//	call_id, from_number, to_number, direction, status, trunk, extension,
//	start_time, answer_time, end_time, duration_ms, billable_ms, ring_ms,
//	metadata
type SQLExporter struct {
	exec  SQLExecutor
	query string
}

var sqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

var sqlColumns = []string{
	"call_id", "from_number", "to_number", "direction", "status",
	"trunk", "extension", "start_time", "answer_time", "end_time",
	"duration_ms", "billable_ms", "ring_ms", "metadata",
}

// NewSQLExporter builds a SQL exporter using ? placeholders.
func NewSQLExporter(exec SQLExecutor, table string) (*SQLExporter, error) {
	return NewSQLExporterWithDialect(exec, table, SQLDialectQuestion)
}

// NewSQLExporterWithDialect builds a SQL exporter for a specific dialect.
func NewSQLExporterWithDialect(exec SQLExecutor, table string, dialect SQLDialect) (*SQLExporter, error) {
	if exec == nil {
		return nil, fmt.Errorf("cdr: nil SQL executor")
	}
	if !sqlIdentifier.MatchString(table) {
		return nil, fmt.Errorf("cdr: invalid SQL table name %q", table)
	}

	placeholders := make([]string, len(sqlColumns))
	for i := range sqlColumns {
		switch dialect {
		case SQLDialectPostgres:
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		default:
			placeholders[i] = "?"
		}
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(sqlColumns, ", "),
		strings.Join(placeholders, ", "),
	)
	return &SQLExporter{exec: exec, query: query}, nil
}

// Export implements Exporter.
func (e *SQLExporter) Export(r Record) error {
	_, err := e.exec.Exec(e.query, sqlArgs(r)...)
	return err
}

// Close implements Exporter. It does not close the underlying database handle.
func (e *SQLExporter) Close() error { return nil }

func sqlArgs(r Record) []any {
	return []any{
		r.CallID,
		r.From,
		r.To,
		string(r.Direction),
		string(r.Status),
		r.Trunk,
		r.Extension,
		sqlTime(r.StartTime),
		sqlTime(r.AnswerTime),
		sqlTime(r.EndTime),
		durationMS(r.Duration),
		durationMS(r.BillableDuration),
		durationMS(r.RingDuration),
		sqlMetadata(r.Metadata),
	}
}

func sqlTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func sqlMetadata(m map[string]string) any {
	if s := metadataJSON(m); s != "" {
		return s
	}
	return nil
}
