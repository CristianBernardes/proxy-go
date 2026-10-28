package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     any
	expiresAt time.Time
}

// TTLCache é um cache em memória thread-safe com expiração por chave.
type TTLCache struct {
	mu    sync.RWMutex
	store map[string]entry
}

// NewTTLCache cria um cache vazio pronto para uso.
func NewTTLCache() *TTLCache {
	return &TTLCache{store: make(map[string]entry)}
}

// Get retorna o valor armazenado sob key, se existir e ainda não tiver expirado.
func (c *TTLCache) Get(key string) (any, bool) {
	c.mu.RLock()
	e, ok := c.store[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		c.Invalidate(key)
		return nil, false
	}
	return e.value, true
}

// Set armazena value sob key com o TTL informado.
func (c *TTLCache) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = entry{value: value, expiresAt: time.Now().Add(ttl)}
}

// Invalidate remove a chave do cache, se existir.
func (c *TTLCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}
