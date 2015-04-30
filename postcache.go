package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/garyburd/redigo/redis"
	"github.com/op/go-logging"
)

type container struct {
	pool *redis.Pool
}

type configParams struct {
	backend   string
	listen    string
	redis     string
	expire    int
	freshness int
}

func (c container) cacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var cacheStatus string
		var urlComponents = []string{
			"http://",
			config.backend,
			r.URL.Path,
		}
		backendURL := strings.Join(urlComponents, "")

		body, _ := ioutil.ReadAll(r.Body)
		identifier := []byte(fmt.Sprintf("%s%s", body, r.URL.Path))
		sum := md5.Sum(identifier)
		hash := hex.EncodeToString(sum[:16])

		redisConn := c.pool.Get()
		defer redisConn.Close()

		repl, err := redisConn.Do("GET", hash)
		if err != nil {
			log.Error(err.Error())
			return
		}
		if repl == nil {
			log.Debug(fmt.Sprintf("cache: %s %s", hash, color.YellowString("MISS")))
			w.Header().Set("X-postcache", "MISS")
			response, cacheError := c.updateCache(hash, string(body), backendURL)
			if cacheError != nil {
				log.Error(cacheError.Error())
			}
			w.Write([]byte(response))
		} else {
			ttlrepl, ttlerr := redisConn.Do("TTL", hash)
			if ttlerr != nil {
				log.Error("key is gone? maybe the TTL expired before we got here.")
			} else {
				if ttlrepl.(int64) < int64((config.expire - config.freshness)) {
					cacheStatus = color.YellowString("STALE")
					go c.updateCache(hash, string(body), backendURL)
				} else {
					cacheStatus = color.CyanString("HIT")
				}
			}
			log.Debug(fmt.Sprintf("cache: %s %s ", hash, cacheStatus))
			w.Header().Set("X-postcache", cacheStatus)
			w.Write(repl.([]byte))
		}
	} else {
		log.Debug("cache: NOCACHE")
		w.Header().Set("X-postcache", "CANT-CACHE")
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = "http"
				req.URL.Host = config.backend
			},
		}
		proxy.ServeHTTP(w, r)
	}
}

func (c container) updateCache(hash string, body string, backendURL string) (string, error) {
	var response = "RESPONSE NOT SET"
	var err error
	redisConn := c.pool.Get()
	defer redisConn.Close()
	log.Debug("cache: %s %s", hash, color.BlueString("UPDATE"))
	httpClient := http.Client{Timeout: time.Duration(600 * time.Second)}
	resp, httperror := httpClient.Post(backendURL, "application/JSON", strings.NewReader(body))
	if httperror == nil {
		if resp.StatusCode != 200 {
			log.Error(fmt.Sprintf("Backend error code: %v ", resp.StatusCode))
			return response, err
		}
		requestBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error(err.Error())
		}
		response = string(requestBody)
		if string(requestBody) != "" {
			_, err = redisConn.Do("SET", hash, string(requestBody))
			if err != nil {
				log.Error(err.Error())
				return response, err
			}
			log.Debug(fmt.Sprintf("cache: %s %s", hash, color.GreenString("SET")))
			_, err = redisConn.Do("EXPIRE", hash, config.expire)
			if err != nil {
				log.Error(err.Error())
				return response, err
			}
		} else {
			log.Error("Empty backend response")
			fmt.Println(resp)
		}

	} else {
		log.Error(fmt.Sprintf("Backend request failure: %s", httperror.Error()))
	}
	return response, err
}

var config configParams
var log = logging.MustGetLogger("example")
var format = logging.MustStringFormatter(
	"%{color}%{time:15:04:05.000} >> %{level:.4s} %{color:reset} %{message}",
)

func main() {
	flag.StringVar(&config.backend, "b", "127.0.0.1:8080", "address of backend server")
	flag.StringVar(&config.listen, "l", "8081", "port to listen on")
	flag.StringVar(&config.redis, "r", "127.0.0.1:6379", "address of redis server")
	flag.IntVar(&config.expire, "e", 7200, "TTL of cache values (seconds)")
	flag.IntVar(&config.freshness, "f", 300, "age at which a cache becomes STALE (seconds)")
	flag.Parse()
	backend := logging.NewLogBackend(os.Stdout, "", 0)
	backendFormatter := logging.NewBackendFormatter(backend, format)
	logging.SetBackend(backendFormatter)

	log.Info("Postcache!")
	log.Info("listening on 0.0.0.0:%s", config.listen)
	log.Info("backend server: %s", config.backend)
	log.Info("redis cache server: %s", config.redis)

	pool := newPool(config.redis)
	http.HandleFunc("/", container{pool}.cacheHandler)
	http.ListenAndServe(fmt.Sprintf(":%s", config.listen), nil)
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
