package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/op/go-logging"
)

type container struct {
	pool *redis.Pool
}

func (c container) cacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var hashBuffer bytes.Buffer
		var bodyBuffer bytes.Buffer
		var urlComponents = []string{
			"http://",
			os.Args[1],
			r.URL.Path,
		}
		backendURL := strings.Join(urlComponents, "")

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
			log.Error(err.Error())
			return
		}
		if repl == nil {
			log.Debug(fmt.Sprintf("cache: MISS - updating from backend : %s \n", backendURL))
			w.Header().Set("X-postcache", "MISS")
			response, cacheError := c.updateCache(hash, bodyBuffer.String(), backendURL)
			if cacheError != nil {
				log.Error(cacheError.Error())
			}
			w.Write([]byte(response))
		} else {
			ttlrepl, ttlerr := redisConn.Do("TTL", hash)
			if ttlerr != nil {
				log.Error("key is gone? maybe the TTL expired before we got here.")
			} else {
				if ttlrepl.(int64) < 3300 {
					log.Debug("cache: STALE - async update from backend")
					w.Header().Set("X-postcache", "STALE")
					go c.updateCache(hash, bodyBuffer.String(), backendURL)
				}
			}
			log.Debug(fmt.Sprintf("cache: HIT %s ", hash))
			w.Header().Set("X-postcache", "HIT")
			w.Write(repl.([]byte))
		}
	} else {
		log.Debug("cache: NOCACHE")
		w.Header().Set("X-postcache", "CANT-CACHE")
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = "http"
				req.URL.Host = os.Args[1]
			},
		}
		proxy.ServeHTTP(w, r)
	}
}

func (c container) updateCache(hash string, body string, backendURL string) (string, error) {
	var response = "RESPONSE NOT SET"
	var err error
	var responseBuffer bytes.Buffer
	redisConn := c.pool.Get()
	defer redisConn.Close()
	httpClient := http.Client{Timeout: time.Duration(10 * time.Minute)}
	resp, httperror := httpClient.Post(backendURL, "application/JSON", strings.NewReader(body))
	if httperror == nil {
		if resp.StatusCode != 200 {
			log.Error(fmt.Sprintf("Backend error code: %v ", resp.StatusCode))
			return response, err
		}
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			responseBuffer.Write(scanner.Bytes())
		}
		response = responseBuffer.String()
		if responseBuffer.String() != "" {
			_, err = redisConn.Do("SET", hash, responseBuffer.String())
			if err != nil {
				log.Error(err.Error())
				return response, err
			}
			_, err = redisConn.Do("EXPIRE", hash, 3600)
			if err != nil {
				log.Error(err.Error())
				return response, err
			}
		}
	} else {
		log.Error(fmt.Sprintf("Backend request failure: %s", httperror.Error()))
	}
	log.Info(fmt.Sprintf("backend response: %s", responseBuffer.String()))
	return response, err
}

var log = logging.MustGetLogger("example")
var format = logging.MustStringFormatter(
	"%{color}%{time:15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}",
)

func main() {

	backend := logging.NewLogBackend(os.Stdout, "", 0)
	backendFormatter := logging.NewBackendFormatter(backend, format)
	logging.SetBackend(backendFormatter)

	log.Info("Postcache listening on 0.0.0.0:8081")

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
				log.Error(err.Error())
				return nil, err
			}
			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			if err != nil {
				log.Error(err.Error())
			}
			return err
		},
	}
}
