package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"net/http"
	"os"
	"strings"
	"time"
)

type container struct {
	pool *redis.Pool
}

func (c container) cacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var hashBuffer bytes.Buffer
		var bodyBuffer bytes.Buffer
		scanner := bufio.NewScanner(r.Body)
		hashBuffer.WriteString(r.URL.Path)
		for scanner.Scan() {
			hashBuffer.Write(scanner.Bytes())
			bodyBuffer.Write(scanner.Bytes())
		}
		sum := md5.Sum(hashBuffer.Bytes())
		hash := hex.EncodeToString(sum[:16])
		redisConn := c.pool.Get()
		defer redisConn.Close()
		repl, err := redisConn.Do("GET", hash)
		if err != nil {
			fmt.Println(err)
			return
		}
		if repl == nil {
			var urlComponents = []string{
				"http://",
				os.Args[1],
				r.URL.Path,
			}
			backendURL := strings.Join(urlComponents, "")
			fmt.Printf("cache: MISS - updating from backend : %s", backendURL)
			w.Write([]byte(c.updateCache(hash, bodyBuffer.String(), backendURL)))
		} else {
			fmt.Printf("cache: HIT %s \n", hash)
			w.Write(repl.([]byte))
		}
	}
}

func (c container) updateCache(hash string, body string, backendURL string) string {
	var response string
	var err error
	var responseBuffer bytes.Buffer
	redisConn := c.pool.Get()
	defer redisConn.Close()
	resp, httperror := http.Post(backendURL, "application/JSON", strings.NewReader(body))
	if httperror == nil {
		if resp.StatusCode != 200 {
			fmt.Printf("Backend error code: %v \n", resp.StatusCode)
			fmt.Println(resp)
			return response
		}
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			responseBuffer.Write(scanner.Bytes())
		}
		response = responseBuffer.String()
		_, err = redisConn.Do("SET", hash, responseBuffer.String())
		if err != nil {
			fmt.Println(err)
			return response
		}
		_, err = redisConn.Do("EXPIRE", hash, 300)
		if err != nil {
			fmt.Println(err)
			return response
		}
	} else {
		fmt.Println(httperror)
		fmt.Println("backend request failure")
	}
	return response
}

func main() {
	pool := newPool("localhost:6379")
	http.HandleFunc("/", container{pool}.cacheHandler)
	http.ListenAndServe(":8081", nil)
}

func newPool(server string) *redis.Pool {
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
			return err
		},
	}
}
