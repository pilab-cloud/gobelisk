package sip

import (
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type item[T comparable] struct {
	value      T
	lastAccess int64
}

// TTLMap is a generic key[K]-value[T] map that expires items after a given TTL
type TTLMap[K comparable, T comparable] struct {
	m      map[K]*item[T]
	l      sync.Mutex
	ticker *time.Ticker
	maxTTL int
}

// New creates a new TTLMap with the given length and TTL. The TTL is in seconds.
func NewTTLMap[K comparable, T comparable](maxTTL int) *TTLMap[K, T] {
	m := &TTLMap[K, T]{
		m:      make(map[K]*item[T]),
		maxTTL: maxTTL,
	}

	// Start a goroutine to clean up expired items
	go m.expirationHandler()

	return m
}

// Len returns the number of items in the map
func (m *TTLMap[K, T]) Len() int {
	return len(m.m)
}

// expirationHandler is a goroutine that periodically checks for expired items and removes them.
func (m *TTLMap[K, T]) expirationHandler() {
	m.ticker = time.NewTicker(time.Second)

	for now := range m.ticker.C {
		m.l.Lock()

		// Iterate over all items
		for k, v := range m.m {
			if now.Unix()-v.lastAccess > int64(m.maxTTL) {
				// Expired, remove it
				delete(m.m, k)

				// If the value implements io.Closer, close it
				val := any(v.value)
				if closer, ok := val.(io.Closer); ok {
					closer.Close()
				}

				log.Info().Msgf("Removed expired item: %v - %+v", k, v.value)
			}
		}

		m.l.Unlock()
	}
}

func (m *TTLMap[K, T]) PutWithTTL(k K, v T, ttl int) {
	m.l.Lock()
	defer m.l.Unlock()

	m.m[k] = &item[T]{value: v, lastAccess: time.Now().Unix() + (int64(m.maxTTL) - int64(ttl))}
}

// Print out the items in the map
func (m *TTLMap[K, T]) debug() {
	m.l.Lock()
	defer m.l.Unlock()

	for k, v := range m.m {
		log.Info().Interface("key", k).
			Interface("value", v.value).
			Msgf("Item: %T - %T", k, v.value)
	}
}

// Put adds a new item to the map or updates an existing one.
func (m *TTLMap[K, T]) Put(k K, v T) {
	m.l.Lock()
	defer m.l.Unlock()

	it, ok := m.m[k]
	if !ok {
		m.m[k] = &item[T]{value: v, lastAccess: time.Now().Unix()}
	} else {
		it.value = v
		it.lastAccess = time.Now().Unix()
	}
}

// Get retrieves the value for the given key and returns true if the item exists, false otherwise.
func (m *TTLMap[K, T]) Get(k K) (T, bool) {
	var v T

	if it, ok := m.m[k]; ok {
		if time.Now().Unix()-it.lastAccess > int64(m.maxTTL) {
			delete(m.m, k)
			return v, false
		}
		v = it.value
		it.lastAccess = time.Now().Unix()

		return v, true
	}

	return v, false
}

func (m *TTLMap[K, T]) Delete(k K) {
	m.l.Lock()
	defer m.l.Unlock()

	delete(m.m, k)
}

func (m *TTLMap[K, T]) Keys() []K {
	keys := make([]K, 0, len(m.m))

	i := 0
	for k := range m.m {
		keys[i] = k
		i++
	}

	return keys
}

// get and delete
func (m *TTLMap[K, T]) GetAndDelete(k K) (T, bool) {
	m.l.Lock()
	defer m.l.Unlock()

	v, ok := m.m[k]
	if ok {
		delete(m.m, k)

		return v.value, ok
	}

	// Not found :'(
	var r T

	return r, ok
}

// Close stops the expiration handler and releases any resources held by the TTLMap.
func (m *TTLMap[K, T]) Close() error {
	if m.ticker != nil {
		m.ticker.Stop()
		m.ticker = nil
	}

	return nil
}
