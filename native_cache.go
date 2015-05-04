package main

import (
	"fmt"
	"sync"
	"time"
)

type nativeCache struct {
	store      map[string]item
	freshness  time.Duration
	expire     time.Duration
	globalLock sync.Mutex
	locks      map[string]bool
}

type item struct {
	payload   []byte
	timestamp time.Time
}

func (c *nativeCache) initialize() error {
	log.Info("Native cache initialized")
	c.store = make(map[string]item)
	c.locks = make(map[string]bool)
	c.freshness = time.Duration((config.freshness * 1000000000))
	c.expire = time.Duration((config.expire * 1000000000))
	go c.maintainance()
	return nil
}

func (c *nativeCache) get(hash string) (string, string, error) {
	var cacheStatus string
	item, found := c.store[hash]
	if found == false {
		return "", "MISS", nil
	}
	if time.Since(item.timestamp) > c.freshness {
		cacheStatus = "STALE"
	} else {
		cacheStatus = "HIT"
	}
	return string(item.payload), cacheStatus, nil
}

func (c *nativeCache) set(hash string, body string) error {
	var newItem item
	c.globalLock.Lock()
	defer c.globalLock.Unlock()
	newItem.payload = []byte(body)
	newItem.timestamp = time.Now()
	c.store[hash] = newItem
	return nil
}

func (c *nativeCache) lock(hash string) (bool, error) {
	locked, found := c.locks[hash]
	if found == true {
		if locked == true {
			return false, nil
		}
		c.locks[hash] = true
		return true, nil

	}
	c.locks[hash] = true
	return true, nil
}

func (c *nativeCache) unlock(hash string) error {
	c.locks[hash] = false
	return nil
}

func (c *nativeCache) maintainance() {
	log.Debug("Native cache maintainance started")
	for {
		tel.Current.Add("postcache.native.cache.items", float32(len(c.store)))
		var culled float32
		for hash := range c.store {
			if time.Since(c.store[hash].timestamp) > c.expire {
				delete(c.store, hash)
				delete(c.locks, hash)
				culled++
				tel.Current.Add("postcache.native.cache.culls", culled)
				log.Debug(fmt.Sprintf("%s CULL", hash))
			}
		}
		time.Sleep((1 * time.Second))
	}
}
