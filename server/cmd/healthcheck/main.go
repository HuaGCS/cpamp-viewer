package main

import (
	"net/http"
	"os"
)

func main() {
	url := "http://127.0.0.1:18417/health"
	if value := os.Getenv("HEALTHCHECK_URL"); value != "" {
		url = value
	}
	response, err := http.Get(url)
	if err != nil || response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	_ = response.Body.Close()
}
