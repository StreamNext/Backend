// Command tvdbstub serves recorded TheTVDB fixtures, so the CI stack can run the catalog with no API key.
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/StreamNext/Backend/internal/tvdb/stub"
)

func main() {
	srv := &http.Server{Addr: ":8081", Handler: stub.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Println("tvdbstub listening on", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
