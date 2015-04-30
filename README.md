# postcache

A very aggressive stupid caching proxy (belligerent caching?).

Designed to be used with KairosDB to alleviate load on the Kairos/Cassandra.  

Caches response body from POST requests in redis for 5 minutes, returns body from cache on identical requests.

requires redis-server on localhost:6379  

Usage:  
```./postcache kairosdb.example.com:8080 ```

will start postcache running on localhost:8081 (currently not configurable)

Cache hit/miss can be seen via headers

    X-Postcache: [HIT, MISS, STALE]

* HIT: 'fresh' cache found in redis, returned without triggering refresh
* MISS: no cache found, request passed through to backend, cache updated
* STALE: cache found, but expired, returned 'stale' cache and trigger async refresh
