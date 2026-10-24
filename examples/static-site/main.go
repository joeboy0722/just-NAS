// SPDX-License-Identifier: MIT
package main

import (
	"log"
	"net/http"
	"time"
)

func main() {
	s := http.Server{Addr: ":8080", Handler: http.FileServer(http.Dir("/srv/content")), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(s.ListenAndServe())
}
