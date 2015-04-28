package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"net"
	"net/http"
	"strings"
)

type container struct {
	redis redis.Conn
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
		repl, err := c.redis.Do("GET", hash)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(hashBuffer.String())
		if repl == nil {
			w.Write([]byte(c.updateCache(hash, bodyBuffer.String())))
			fmt.Println("cache: MISS")
		} else {
			fmt.Println("cache: HIT")
			w.Write(repl.([]byte))
		}
	}
}

func (c container) updateCache(hash string, body string) string {
	var response string
	var responseBuffer bytes.Buffer
	resp, httperror := http.Post("https://127.0.0.1:80/api/v1/datapoints/query", "application/JSON", strings.NewReader(body))
	if httperror == nil {
		if resp.StatusCode != 200 {
			fmt.Printf("Backend error code: %v \n", resp.StatusCode)
			fmt.Println(resp)
			return response
		}
		fmt.Println(resp)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			responseBuffer.Write(scanner.Bytes())
		}
		response = responseBuffer.String()
		repl, err := c.redis.Do("SET", hash, responseBuffer.String())
		if err != nil {
			fmt.Println(err)
			return response
		}
		fmt.Println(repl)
		repl, err = c.redis.Do("EXPIRE", hash, 300)
		if err != nil {
			fmt.Println(err)
			return response
		}
		fmt.Println(repl)
	} else {
		fmt.Println(httperror)
		fmt.Println("backend request failure")
	}
	return response
}

func main() {
	conn, _ := net.Dial("tcp", "localhost:6379")
	redisConn := redis.NewConn(conn, 10000000, 10000000)
	defer redisConn.Close()
	http.HandleFunc("/", container{redisConn}.cacheHandler)
	http.ListenAndServe(":8081", nil)
}
