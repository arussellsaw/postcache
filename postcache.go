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

		body, _ := ioutil.ReadAll(r.Body)
		identifier := []byte(fmt.Sprintf("%s%s", body, r.URL.Path))
		sum := md5.Sum(identifier)
		hash := hex.EncodeToString(sum[:16])
		cacheResponse, cacheStatus, err = c.cache.get(hash)
		if err != nil {
			log.Error(err.Error())
			return
		}
		switch cacheStatus {
		case "HIT":
			log.Debug(fmt.Sprintf("%s %s", hash, color.CyanString(cacheStatus)))
			w.Header().Set("X-Postcache", cacheStatus)
			w.Write([]byte(cacheResponse))
		case "STALE":
			log.Debug(fmt.Sprintf("%s %s", hash, color.WhiteString(cacheStatus)))
			go c.asyncUpdate(hash, r)
			w.Header().Set("X-Postcache", cacheStatus)
			w.Write([]byte(cacheResponse))
		case "MISS":
			log.Debug(fmt.Sprintf("%s %s", hash, color.RedString("MISS")))
			w.Header().Set("X-postcache", cacheStatus)
			response, err = c.getResponse(hash, r)
			c.cache.set(hash, response)
			w.Write([]byte(response))
		}
	} else {
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

func (c container) asyncUpdate(hash string, r *http.Request) {
	lock, err := c.cache.lock(hash)
	if err != nil {
		return
	}
	if lock == true {
		defer c.cache.unlock(hash)
		resp, err := c.getResponse(hash, r)
		if err != nil {
			return
		}
		c.cache.set(hash, resp)
	}
}

func (c container) getResponse(hash string, r *http.Request) (string, error) {
	var body []byte
	var response string
	var err error
	var urlComponents = []string{
		"http://",
		config.backend,
		r.URL.Path,
	}

	body, err = ioutil.ReadAll(r.Body)
	if err != nil {
		log.Error(err.Error())
		return "", err
	}

	backendURL := strings.Join(urlComponents, "")
	httpClient := http.Client{Timeout: time.Duration(600 * time.Second)}
	resp, httperror := httpClient.Post(backendURL, "application/JSON", strings.NewReader(string(body)))

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
	}
	return response, nil
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

	var cache = new(redisCache)
	cache.initialize()

	log.Info("listening on 0.0.0.0:%s", config.listen)

	http.HandleFunc("/", container{cache}.cacheHandler)
	http.ListenAndServe(fmt.Sprintf(":%s", config.listen), nil)
}
