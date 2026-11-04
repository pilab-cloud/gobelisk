package cdr

import (
	"bufio"
	"encoding/xml"
	"io"
	"sync"
)

// XMLExporter writes a single <cdrs> document containing one <cdr> element per
// record. Close must be called to finish the document.
type XMLExporter struct {
	mu     sync.Mutex
	bw     *bufio.Writer
	enc    *xml.Encoder
	closed bool
}

// NewXMLExporter opens a CDR XML document on w.
func NewXMLExporter(w io.Writer) (*XMLExporter, error) {
	bw := bufio.NewWriter(w)
	e := &XMLExporter{
		bw:  bw,
		enc: xml.NewEncoder(bw),
	}

	if _, err := e.bw.WriteString(xml.Header); err != nil {
		return nil, err
	}
	if err := e.enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "cdrs"}}); err != nil {
		return nil, err
	}
	return e, nil
}

type xmlRecord struct {
	XMLName    xml.Name `xml:"cdr"`
	CallID     string   `xml:"call_id"`
	From       string   `xml:"from"`
	To         string   `xml:"to"`
	Direction  string   `xml:"direction"`
	Status     string   `xml:"status"`
	Trunk      string   `xml:"trunk,omitempty"`
	Extension  string   `xml:"extension,omitempty"`
	StartTime  string   `xml:"start_time"`
	AnswerTime string   `xml:"answer_time,omitempty"`
	EndTime    string   `xml:"end_time"`

	DurationMS         int64  `xml:"duration_ms"`
	BillableDurationMS int64  `xml:"billable_ms"`
	RingDurationMS     int64  `xml:"ring_ms"`
	Metadata           string `xml:"metadata,omitempty"`
}

// Export implements Exporter.
func (e *XMLExporter) Export(r Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return io.ErrClosedPipe
	}
	rec := xmlRecord{
		CallID:             r.CallID,
		From:               r.From,
		To:                 r.To,
		Direction:          string(r.Direction),
		Status:             string(r.Status),
		Trunk:              r.Trunk,
		Extension:          r.Extension,
		StartTime:          formatTime(r.StartTime),
		AnswerTime:         formatTime(r.AnswerTime),
		EndTime:            formatTime(r.EndTime),
		DurationMS:         durationMS(r.Duration),
		BillableDurationMS: durationMS(r.BillableDuration),
		RingDurationMS:     durationMS(r.RingDuration),
		Metadata:           metadataJSON(r.Metadata),
	}
	if err := e.enc.Encode(rec); err != nil {
		return err
	}
	if err := e.enc.Flush(); err != nil {
		return err
	}
	return e.bw.Flush()
}

// Close writes the closing root element and flushes the document. It does not
// close the underlying writer.
func (e *XMLExporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if err := e.enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "cdrs"}}); err != nil {
		return err
	}
	if err := e.enc.Flush(); err != nil {
		return err
	}
	return e.bw.Flush()
}
