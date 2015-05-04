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

	"github.com/arussellsaw/telemetry"
	"github.com/fatih/color"
	"github.com/op/go-logging"
)

type container struct {
	cache cacher
}

type configParams struct {
	backend   string
	listen    string
	redis     string
	expire    int
	freshness int
}

type cacher interface {
	initialize() error
	get(string) (string, string, error)
	set(string, string) error
	lock(string) (bool, error)
	unlock(string) error
}

func (c container) cacheHandler(w http.ResponseWriter, r *http.Request) {
	var cacheStatus string
	var cacheResponse string
	var response string
	var err error
	if r.Method == "POST" {
		tel.Counter.Add("postcache.requests.post", float32(1))
		body, _ := ioutil.ReadAll(r.Body)
		identifier := []byte(fmt.Sprintf("%s%s", body, r.URL.Path))
		sum := md5.Sum(identifier)
		hash := hex.EncodeToString(sum[:16])
		start := time.Now()
		cacheResponse, cacheStatus, err = c.cache.get(hash)
		tel.Average.Add("postcache.cache.speed", float32(time.Since(start).Nanoseconds()))
		if err != nil {
			log.Error(err.Error())
			return
		}
		switch cacheStatus {
		case "HIT":
			log.Debug(fmt.Sprintf("%s %s", hash, color.CyanString(cacheStatus)))
			tel.Counter.Add("postcache.cache.hit", float32(1))
			w.Header().Set("X-Postcache", cacheStatus)
			w.Write([]byte(cacheResponse))
		case "STALE":
			log.Debug(fmt.Sprintf("%s %s", hash, color.WhiteString(cacheStatus)))
			tel.Counter.Add("postcache.cache.stale", float32(1))
			go c.asyncUpdate(hash, r, string(body))
			w.Header().Set("X-Postcache", cacheStatus)
			w.Write([]byte(cacheResponse))
		case "MISS":
			log.Debug(fmt.Sprintf("%s %s", hash, color.RedString(cacheStatus)))
			tel.Counter.Add("postcache.cache.miss", float32(1))
			w.Header().Set("X-postcache", cacheStatus)
			response, err = c.getResponse(hash, r, string(body))
			if err != nil {
				break
			}
			c.cache.set(hash, response)
			w.Write([]byte(response))
		}
	} else {
		w.Header().Set("X-postcache", "CANT-CACHE")
		tel.Counter.Add("postcache.cache.nocache", float32(1))
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = "http"
				req.URL.Host = config.backend
			},
		}
		proxy.ServeHTTP(w, r)
	}
}

func (c container) asyncUpdate(hash string, r *http.Request, requestBody string) {
	lock, err := c.cache.lock(hash)
	if err != nil {
		return
	}
	if lock == true {
		log.Debug(fmt.Sprintf("%s %s", hash, color.BlueString("UPDATE")))
		defer c.cache.unlock(hash)
		resp, err := c.getResponse(hash, r, requestBody)
		if err != nil {
			log.Error("backend request failed")
			return
		}
		c.cache.set(hash, resp)
	} else {
		log.Debug("%s %s", hash, color.RedString("LOCKED"))
	}
}

func (c container) getResponse(hash string, r *http.Request, body string) (string, error) {
	var response string
	var urlComponents = []string{
		"http://",
		config.backend,
		r.URL.Path,
	}

	var start = time.Now()

	backendURL := strings.Join(urlComponents, "")
	httpClient := http.Client{Timeout: time.Duration(600 * time.Second)}
	resp, httperror := httpClient.Post(backendURL, "application/JSON", strings.NewReader(body))
	tel.Average.Add("postcache.backend.requesttime", float32((time.Since(start).Nanoseconds())/1000000))
	if httperror == nil {
		if resp.StatusCode != 200 {
			log.Error(backendURL)
			log.Error(string(body))
			err := fmt.Errorf("Backend error code: %v ", resp.StatusCode)
			log.Error(err.Error())
			return "", err
		}

		responseBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error(fmt.Sprintf("body read failed: %s : %s", hash, err.Error()))
			return "", err
		}

		response = string(responseBody)
		if response != "" {
			err = c.cache.set(hash, response)
			if err != nil {
				log.Error(fmt.Sprintf("key set failed: %s : %s", hash, err.Error()))
				return response, err
			}
			log.Debug(fmt.Sprintf("%s %s", hash, color.GreenString("SET")))
		} else {
			log.Error("Empty backend response")
			fmt.Println(resp)
		}

	} else {
		log.Error(fmt.Sprintf("Backend request failure: %s", httperror.Error()))
		return "", httperror
	}
	return response, nil
}

var config configParams
var tel = *telemetry.New(":9000", (time.Second * 5))
var log = logging.MustGetLogger("postcache")
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
	//var cache = new(redisCache)
	var cache = new(nativeCache)
	cache.initialize()

	log.Info("Telemetry on 0.0.0.0:9000")
	tel.Average.New("postcache.backend.requesttime", (600 * time.Second))
	tel.Counter.New("postcache.cache.hit", (60 * time.Second))
	tel.Counter.New("postcache.cache.miss", (60 * time.Second))
	tel.Counter.New("postcache.cache.stale", (60 * time.Second))
	tel.Counter.New("postcache.cache.nocache", (60 * time.Second))
	tel.Counter.New("postcache.requests.post", (60 * time.Second))
	tel.Current.New("postcache.native.cache.items", (0 * time.Second))
	tel.Counter.New("postcache.native.cache.culls", (600 * time.Second))
	tel.Average.New("postcache.cache.speed", (300 * time.Second))

	log.Info("Listening on 0.0.0.0:%s", config.listen)
	http.HandleFunc("/", container{cache}.cacheHandler)
	http.ListenAndServe(fmt.Sprintf(":%s", config.listen), nil)
}
