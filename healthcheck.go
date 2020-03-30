// Derived closely from https://github.com/Soluto/golang-docker-healthcheck-example
// Found at https://medium.com/google-cloud/dockerfile-go-healthchecks-k8s-9a87d5c5b4cb

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var timeout int

func main() {
	if len(os.Args) < 4 {
		log.Fatal("Expected URLs as command-line arguments")
		os.Exit(1)
	}

	port, _ := strconv.Atoi(os.Args[1])
	timeout, _ = strconv.Atoi(os.Args[2])

	log.Printf("Using health check timeout of %d seconds", timeout)

	mux := http.NewServeMux()

	// Handle "/all" tests
	mux.HandleFunc("/all", HandleRequest)

	// Handle for "/self"
	mux.HandleFunc("/self", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "Self: Ready", http.StatusOK)
	})

	// Handle everything else
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "Not Found", http.StatusNotFound)
	})

	log.Printf("Starting http server on 'http://localhost:%d'", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), mux))
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("--- begin ---")

	client := http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	success := true

	for i := 3; i < len(os.Args); i++ {
		url := os.Args[i]
		log.Printf("Testing url: '%s'...", url)

		resp, err := client.Get(url)

		if err != nil {
			log.Printf("Error when calling '%s': %s", url, err)
			success = false
		}

		if err == nil {
			log.Printf("'%s' returned status code %d", url, resp.StatusCode)
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				log.Printf(" * Test succeeded")
			} else {
				log.Printf(" * Marking test as failed")
				success = false
			}
		}
	}

	if success {
		log.Print("All: Health check succeeded")
		fmt.Fprintln(w, "All: Ready", http.StatusOK)
	} else {
		log.Print("All: Health check failed")
		http.Error(w, "All: Health check failed", http.StatusServiceUnavailable)
	}

	log.Printf("--- end ---")
}
