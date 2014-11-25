package main

import (
	"encoding/json"
	"flag"
	"github.com/oschwald/geoip2-golang"
	"github.com/pmylund/go-cache"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type Response struct {
	Data   interface{} `json:",omitempty"`
	Error  string      `json:",omitempty"`
	cached bool
}

func main() {
	var dbName = flag.String("db", "GeoLite2-City.mmdb", "File name of MaxMind GeoIP2 and GeoLite2 database")
	var lookup = flag.String("lookup", "city", "Specify which value to look up. Can be 'city' or 'country' depending on which database you load.")
	var listen = flag.String("listen", ":5000", "Listen address and port, for instance 127.0.0.1:5000")
	var threads = flag.Int("threads", runtime.NumCPU(), "Number of threads to use. Defaults to number of detected cores")
	var pretty = flag.Bool("pretty", false, "Should output be formatted with newlines and intentation")
	var cacheSecs = flag.Int("cache", 60, "How many seconds should requests be cached. Set to 0 to disable")
	flag.Parse()

	runtime.GOMAXPROCS(*threads)
	var memCache *cache.Cache
	if *cacheSecs > 0 {
		memCache = cache.New(time.Duration(*cacheSecs)*time.Second, 1*time.Second)
	}
	lookupCity := true
	if *lookup == "country" {
		lookupCity = false
	} else if *lookup != "city" {
		log.Fatalf("lookup parameter should be either 'city', or 'country', it is '%s'", *lookup)
	}

	db, err := geoip2.Open(*dbName)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	log.Println("Loaded database " + *dbName)

	// We dereference this to avoid a pretty big penalty under heavy load.
	prettyL := *pretty

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		var ipText string
		// We don't need the body
		req.Body.Close()

		// Prepare the response and queue sending the result.
		res := &Response{}
		defer func() {
			var j []byte
			var err error
			if prettyL {
				j, err = json.MarshalIndent(res, "", "  ")
			} else {
				j, err = json.Marshal(res)
			}
			if err != nil {
				log.Fatal(err)
			}
			if memCache != nil && !res.cached {
				memCache.Set(ipText, j, 0)
			}
			w.Write(j)
		}()

		ipText = req.URL.Query().Get("ip")
		if ipText == "" {
			ipText = strings.Trim(req.URL.Path, "/")
		}
		ip := net.ParseIP(ipText)
		if ip == nil {
			res.Error = "unable to decode ip"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if memCache != nil {
			v, found := memCache.Get(ipText)
			if found {
				res.cached = true
				res.Data = json.RawMessage(v.([]byte))
				return
			}
		}
		if lookupCity {
			result, err := db.City(ip)
			if err != nil {
				res.Error = err.Error()
				return
			}
			res.Data = result
		} else {
			result, err := db.Country(ip)
			if err != nil {
				res.Error = err.Error()
				return
			}
			res.Data = result
		}
	})

	log.Println("Listening on " + *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
}