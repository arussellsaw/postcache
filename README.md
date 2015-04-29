# postcache

A very aggressive stupid caching proxy.

Designed to be used with KairosDB to alleviate load on the Kairos/Cassandra.  

Caches response body from POST requests in redis for 5 minutes, returns body from cache on identical requests.

Usage:  
```./postcache kairosdb.example.com:8080 ```

will start postcache running on localhost:8081 (currently not configurable)

Cache hit/miss can be seen via headers

    X-Postcache: [HIT, MISS, CANT-CACHE]
    X-Postcache-freshness: [FRESH, STALE] (coming soon)
