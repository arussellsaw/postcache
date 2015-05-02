package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/garyburd/redigo/redis"
)

type redisCache struct {
	pool   *redis.Pool
	ttl    int64
	expire int64
}

func (c redisCache) initialize(config configParams) error {
	c.pool = c.newPool(config.redis)
	c.ttl = int64(config.freshness)
	c.expire = int64(config.expire)
	return nil
}

func (c redisCache) get(hash string) (string, string, error) {
	var response string
	var state string
	var redis redis.Conn
	var resp interface{}
	var err error

	redis = c.pool.Get()
	defer redis.Close()

	resp, err = redis.Do("GET", hash)
	if err == nil {
		if resp != nil {
			response = resp.(string)
		} else {
			return "", "MISS", nil
		}
	} else {
		return "", "", errors.New("redis GET failed")
	}

	resp, err = redis.Do("TTL", hash)
	if err == nil {
		if resp != nil {
			if resp.(int64) < (c.expire - c.ttl) {
				state = "STALE"
			} else {
				state = "HIT"
			}
		} else {
			return "", "", errors.New("TTL not found, key has probably expired before we got here")
		}
	} else {
		return "", "", errors.New("redis TLL failed")
	}

	return response, state, err
}

func (c redisCache) set(hash string, body string) error {
	var redis redis.Conn

	redis = c.pool.Get()
	defer redis.Close()

	_, err := redis.Do("SET", hash, body)
	if err != nil {
		return errors.New("failed to SET redis cache key")
	}

	_, err = redis.Do("EXPIRE", hash, c.expire)
	if err != nil {
		return errors.New("failed to set EXPIRE on redis cache key")
	}
	return nil
}

func (c redisCache) lock(hash string) (bool, error) {
	var resp interface{}
	var err error
	var redis redis.Conn

	redis = c.pool.Get()
	defer redis.Close()

	resp, err = redis.Do("SETNX", fmt.Sprintf("lock-%s", hash), "locked")
	if err != nil {
		return false, err
	}
	if resp == 0 {
		return false, nil
	}

	resp, err = redis.Do("EXPIRE", fmt.Sprintf("lock-%s", hash), 600)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c redisCache) unlock(hash string) error {
	var redis redis.Conn

	redis = c.pool.Get()
	defer redis.Close()

	_, err := redis.Do("DEL", fmt.Sprintf("lock-%s", hash))
	if err != nil {
		return err
	}

	return nil
}

func (c redisCache) newPool(server string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", server)
			if err != nil {
				return nil, err
			}
			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			if err != nil {
			}
			return err
		},
	}
}
