package sip

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockCloser is a mock implementation of io.Closer.
type mockCloser struct {
	closed bool
}

// Close implements the io.Closer interface.
func (c *mockCloser) Close() error {
	c.closed = true
	return nil
}

func TestTTLMap(t *testing.T) {
	m := NewTTLMap[string, string](1)

	// Put an item in the map
	m.Put("key1", "value1")

	// Get the item from the map
	v, ok := m.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", v)

	m.debug()

	// Wait for the item to expire
	time.Sleep(2 * time.Second)

	// Get the item from the map again
	_, ok = m.Get("key1")
	assert.False(t, ok)

	t.Run("TestCloser", func(t *testing.T) {
		// Create a mock io.Closer
		mockCloser := &mockCloser{}

		// Create a new TTLMap using the mock io.Closer
		m := NewTTLMap[string, any](1)
		m.Put("key2", mockCloser)

		// Wait for the item to expire
		time.Sleep(2 * time.Second)

		// Give the expiration goroutine time to invoke Close.
		for i := 0; i < 50 && !mockCloser.closed; i++ {
			time.Sleep(20 * time.Millisecond)
		}

		// Check if the Close method was called on the mock io.Closer
		assert.True(t, mockCloser.closed)
	})

	t.Run("TestConcurrency", func(t *testing.T) {
		m := NewTTLMap[string, string](1)
		// Test concurrency
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				m.Put(strconv.Itoa(i), strconv.Itoa(i))
				wg.Done()
			}(i)
		}
		wg.Wait()

		assert.Equal(t, 100, m.Len())
	})

	// Close the TTLMap
	err := m.Close()
	assert.Nil(t, err)

	// Check if the ticker was stopped
	assert.Nil(t, m.ticker)
}
