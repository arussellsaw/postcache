package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNativeCache(t *testing.T) {
	cache := new(nativeCache)
	cache.initialize()
	lock, _ := cache.lock("464b88c2115e632e9711c9a66d27d705")
	if lock == true {
		cache.set("464b88c2115e632e9711c9a66d27d705", "response")
		cache.unlock("464b88c2115e632e9711c9a66d27d705")
	} else {
		t.Error("unable to lock cache")
	}
	response, status, _ := cache.get("464b88c2115e632e9711c9a66d27d705")
	assert.Equal(t, "response", response)
	assert.Equal(t, "STALE", status)
}
