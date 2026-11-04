package speech

import (
	"errors"
	"strings"
	"testing"
)

func TestNewEmptyIsNoop(t *testing.T) {
	for _, name := range []string{"", "noop"} {
		engine, err := New(name, nil)
		if err != nil {
			t.Fatalf("New(%q): %v", name, err)
		}
		if _, ok := engine.(Noop); !ok {
			t.Fatalf("New(%q) = %T, want Noop", name, engine)
		}
		if _, err := engine.Transcribe("x.wav"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Transcribe error = %v, want ErrUnavailable", err)
		}
	}
}

func TestRegisterAndNew(t *testing.T) {
	const name = "test-engine"
	var got Options
	Register(name, func(opts Options) (Engine, error) {
		got = opts
		return Noop{}, nil
	})

	engine, err := New(name, Options{"language": "hu"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := engine.(Noop); !ok {
		t.Fatalf("New = %T, want Noop", engine)
	}
	if got["language"] != "hu" {
		t.Fatalf("factory options = %v, want language=hu", got)
	}

	names := Names()
	found := false
	for _, n := range names {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Names() = %v, want %q", names, name)
	}
}

func TestNewUnknown(t *testing.T) {
	_, err := New("definitely-not-registered", nil)
	if err == nil {
		t.Fatal("New(unknown) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "unknown engine") {
		t.Fatalf("error = %v, want unknown engine", err)
	}
}

func TestFactoryError(t *testing.T) {
	const name = "test-failing-engine"
	want := errors.New("boom")
	Register(name, func(Options) (Engine, error) { return nil, want })

	_, err := New(name, nil)
	if !errors.Is(err, want) {
		t.Fatalf("New error = %v, want %v", err, want)
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	t.Run("empty name", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("Register with empty name did not panic")
			}
		}()
		Register("", func(Options) (Engine, error) { return Noop{}, nil })
	})

	t.Run("nil factory", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("Register with nil factory did not panic")
			}
		}()
		Register("test-nil-factory", nil)
	})

	t.Run("duplicate", func(t *testing.T) {
		const name = "test-duplicate-engine"
		Register(name, func(Options) (Engine, error) { return Noop{}, nil })
		defer func() {
			if recover() == nil {
				t.Fatal("duplicate Register did not panic")
			}
		}()
		Register(name, func(Options) (Engine, error) { return Noop{}, nil })
	})
}
