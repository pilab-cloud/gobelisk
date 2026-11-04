package cdr

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

func sampleRecord() Record {
	start := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	return Record{
		CallID:           "call-1",
		From:             "100",
		To:               "200",
		Direction:        DirectionInternal,
		Status:           StatusAnswered,
		Trunk:            "uplink",
		Extension:        "200",
		StartTime:        start,
		AnswerTime:       start.Add(2 * time.Second),
		EndTime:          start.Add(62 * time.Second),
		Duration:         60 * time.Second,
		BillableDuration: 59 * time.Second,
		RingDuration:     2 * time.Second,
		Metadata:         map[string]string{"route": "sales,eu", "note": `said "hi"`},
	}
}

func TestNoopExporter(t *testing.T) {
	var e Exporter = Noop{}
	if err := e.Export(sampleRecord()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCSVExporter(t *testing.T) {
	var buf bytes.Buffer
	e := NewCSVExporter(&buf)
	if err := e.Export(sampleRecord()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if len(rows[0]) != len(csvHeader) {
		t.Fatalf("header columns = %d, want %d", len(rows[0]), len(csvHeader))
	}
	if rows[1][0] != "call-1" || rows[1][12] != "2000" {
		t.Errorf("unexpected row: %#v", rows[1])
	}
	if !strings.Contains(rows[1][13], `sales,eu`) {
		t.Errorf("metadata not preserved: %q", rows[1][13])
	}
}

func TestCSVExporterClosed(t *testing.T) {
	e := NewCSVExporter(io.Discard)
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.Export(sampleRecord()); err != io.ErrClosedPipe {
		t.Errorf("Export after Close = %v, want ErrClosedPipe", err)
	}
}

func TestXMLExporter(t *testing.T) {
	var buf bytes.Buffer
	e, err := NewXMLExporter(&buf)
	if err != nil {
		t.Fatalf("NewXMLExporter: %v", err)
	}
	if err := e.Export(sampleRecord()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var doc struct {
		XMLName xml.Name    `xml:"cdrs"`
		Records []xmlRecord `xml:"cdr"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
	}
	if len(doc.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(doc.Records))
	}
	got := doc.Records[0]
	if got.CallID != "call-1" || got.From != "100" || got.To != "200" {
		t.Errorf("unexpected record: %#v", got)
	}
	if got.DurationMS != 60000 || got.RingDurationMS != 2000 {
		t.Errorf("durations = %d/%d", got.DurationMS, got.RingDurationMS)
	}
	if !strings.Contains(got.Metadata, `sales,eu`) {
		t.Errorf("metadata not preserved: %q", got.Metadata)
	}
}

func TestXMLExporterEmptyDocument(t *testing.T) {
	var buf bytes.Buffer
	e, err := NewXMLExporter(&buf)
	if err != nil {
		t.Fatalf("NewXMLExporter: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var doc struct {
		XMLName xml.Name    `xml:"cdrs"`
		Records []xmlRecord `xml:"cdr"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, buf.String())
	}
	if len(doc.Records) != 0 {
		t.Errorf("records = %d, want 0", len(doc.Records))
	}
}

type fakeExecutor struct {
	query string
	args  []any
}

func (f *fakeExecutor) Exec(query string, args ...any) (sql.Result, error) {
	f.query = query
	f.args = args
	return fakeResult{}, nil
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

func TestSQLExporterQuestionDialect(t *testing.T) {
	db := &fakeExecutor{}
	e, err := NewSQLExporter(db, "cdrs")
	if err != nil {
		t.Fatalf("NewSQLExporter: %v", err)
	}
	if err := e.Export(sampleRecord()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.HasPrefix(db.query, "INSERT INTO cdrs (") {
		t.Errorf("query = %q", db.query)
	}
	if strings.Count(db.query, "?") != len(sqlColumns) {
		t.Errorf("placeholder count = %d, want %d", strings.Count(db.query, "?"), len(sqlColumns))
	}
	if len(db.args) != len(sqlColumns) {
		t.Fatalf("args = %d, want %d", len(db.args), len(sqlColumns))
	}
	if db.args[0] != "call-1" || db.args[1] != "100" || db.args[2] != "200" {
		t.Errorf("unexpected args: %#v", db.args[:3])
	}
	if db.args[10] != int64(60000) {
		t.Errorf("duration arg = %#v, want 60000", db.args[10])
	}
}

func TestSQLExporterPostgresDialect(t *testing.T) {
	db := &fakeExecutor{}
	e, err := NewSQLExporterWithDialect(db, "public.cdrs", SQLDialectPostgres)
	if err != nil {
		t.Fatalf("NewSQLExporterWithDialect: %v", err)
	}
	if err := e.Export(sampleRecord()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.Contains(db.query, "VALUES ($1, $2") {
		t.Errorf("query = %q", db.query)
	}
	if !strings.Contains(db.query, "$14") {
		t.Errorf("query = %q", db.query)
	}
}

func TestSQLExporterRejectsInvalidTable(t *testing.T) {
	if _, err := NewSQLExporter(&fakeExecutor{}, "cdrs; DROP TABLE users"); err == nil {
		t.Fatal("expected invalid table error")
	}
	if _, err := NewSQLExporter(nil, "cdrs"); err == nil {
		t.Fatal("expected nil executor error")
	}
}
