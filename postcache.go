package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"io"
	"net"
	"net/http"
)

type container struct {
	redis redis.Conn
}

func (c container) cacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var hashBuffer bytes.Buffer
		scanner := bufio.NewScanner(r.Body)
		hashBuffer.WriteString(r.URL.Path)
		for scanner.Scan() {
			hashBuffer.Write(scanner.Bytes())
		}
		sum := md5.Sum(hashBuffer.Bytes())
		hash := hex.EncodeToString(sum[:16])
		repl, err := c.redis.Do("GET", hash)
		if err != nil {
			fmt.Println(err)
			return
		}
		if repl == nil {
			w.Write([]byte(c.updateCache(hash, r.Body)))
			fmt.Println("cache: MISS")
		} else {
			fmt.Println("cache: HIT")
		}
	}
}

func (c container) updateCache(hash string, body io.Reader) string {
	var response string
	var responseBuffer bytes.Buffer
	resp, error := http.Post("127.0.0.1:80/api/v1/datapoints", "application/json", body)
	if error != nil {
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
		return response
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
