// Package cdr provides call detail record (CDR) types and pluggable exporters.
//
// The PBX core only depends on the Exporter interface. Deployments can choose
// CSV, XML or SQL storage, or keep the Noop exporter when CDR persistence is
// not configured.
package cdr

import "time"

// Direction describes the call direction.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
	DirectionInternal Direction = "internal"
)

// Status describes how a call ended.
type Status string

const (
	StatusAnswered Status = "answered"
	StatusBusy     Status = "busy"
	StatusNoAnswer Status = "no_answer"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

// Record is a single call detail record.
type Record struct {
	CallID    string    `json:"call_id" xml:"call_id"`
	From      string    `json:"from" xml:"from"`
	To        string    `json:"to" xml:"to"`
	Direction Direction `json:"direction" xml:"direction"`
	Status    Status    `json:"status" xml:"status"`
	Trunk     string    `json:"trunk,omitempty" xml:"trunk,omitempty"`
	Extension string    `json:"extension,omitempty" xml:"extension,omitempty"`

	StartTime  time.Time `json:"start_time" xml:"start_time"`
	AnswerTime time.Time `json:"answer_time,omitempty" xml:"answer_time,omitempty"`
	EndTime    time.Time `json:"end_time" xml:"end_time"`

	Duration         time.Duration `json:"duration,omitempty" xml:"-"`
	BillableDuration time.Duration `json:"billable_duration,omitempty" xml:"-"`
	RingDuration     time.Duration `json:"ring_duration,omitempty" xml:"-"`

	// Metadata carries deployment-specific extra fields.
	Metadata map[string]string `json:"metadata,omitempty" xml:"-"`
}

// Exporter stores CDR records.
type Exporter interface {
	// Export stores one record.
	Export(Record) error
	// Close releases exporter resources.
	Close() error
}

// Noop is the default Exporter. It discards every record.
type Noop struct{}

// Export implements Exporter.
func (Noop) Export(Record) error { return nil }

// Close implements Exporter.
func (Noop) Close() error { return nil }
