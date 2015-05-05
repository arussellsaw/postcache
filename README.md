# postcache

A very aggressive stupid caching reverse proxy (belligerent caching?).

Designed to be used with KairosDB to alleviate load on Kairos/Cassandra.  

Caches response body from POST requests for 5 minutes, returns body from cache on identical requests.

###Usage:  
```./postcache -b 'kairosdb.example.com:8080' ```
####Flags:
* `-b` `127.0.0.1:8080`
    * the host to forward requests to
* `-l` `8081`
    * port to listen on
* `-r` `127.0.0.1:6379`
    * address of redis-server (if redisCache cacher is used)
* `-e` `7200`
    * TTL of keys in redis (seconds)
* `-f` `300`
    * Age of cache before it is considered STALE and updated (seconds)

Cache hit/miss can be seen via headers

    X-Postcache: [HIT, MISS, STALE]

* HIT: 'fresh' cache found in redis, returned without triggering refresh
* MISS: no cache found, request passed through to backend, cache updated
* STALE: cache found, but expired, returned 'stale' cache and trigger async refresh
