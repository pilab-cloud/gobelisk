package cdr

import (
	"encoding/csv"
	"io"
	"strconv"
	"sync"
)

// CSVOptions controls CSV output.
type CSVOptions struct {
	// Header writes the column header before the first record.
	Header bool
	// Comma is the field separator. Zero means ','.
	Comma rune
}

// CSVExporter writes one CSV row per record.
type CSVExporter struct {
	mu     sync.Mutex
	w      *csv.Writer
	closed bool
}

// NewCSVExporter returns a CSV exporter with a header row.
func NewCSVExporter(w io.Writer) *CSVExporter {
	return NewCSVExporterWithOptions(w, CSVOptions{Header: true})
}

// NewCSVExporterWithOptions returns a CSV exporter with explicit options.
func NewCSVExporterWithOptions(w io.Writer, opts CSVOptions) *CSVExporter {
	cw := csv.NewWriter(w)
	if opts.Comma != 0 {
		cw.Comma = opts.Comma
	}
	e := &CSVExporter{w: cw}
	if opts.Header {
		_ = cw.Write(csvHeader)
		cw.Flush()
	}
	return e
}

var csvHeader = []string{
	"call_id", "from", "to", "direction", "status", "trunk", "extension",
	"start_time", "answer_time", "end_time",
	"duration_ms", "billable_ms", "ring_ms", "metadata",
}

// Export implements Exporter.
func (e *CSVExporter) Export(r Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return io.ErrClosedPipe
	}
	row := []string{
		r.CallID,
		r.From,
		r.To,
		string(r.Direction),
		string(r.Status),
		r.Trunk,
		r.Extension,
		formatTime(r.StartTime),
		formatTime(r.AnswerTime),
		formatTime(r.EndTime),
		strconv.FormatInt(durationMS(r.Duration), 10),
		strconv.FormatInt(durationMS(r.BillableDuration), 10),
		strconv.FormatInt(durationMS(r.RingDuration), 10),
		metadataJSON(r.Metadata),
	}
	if err := e.w.Write(row); err != nil {
		return err
	}
	e.w.Flush()
	return e.w.Error()
}

// Close flushes buffered CSV data. It does not close the underlying writer.
func (e *CSVExporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	e.w.Flush()
	return e.w.Error()
}
