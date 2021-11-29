package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"log"
	"net/http"
	"os"
	"time"
)

type NodePoolIpRequest struct {
	Project    string `json:"project"`
	Region     string `json:"region"`
	Zone       string `json:"zone"`
	Cluster    string `json:"cluster"`
	NodePool   string `json:"node_pool"`
	ExternalIP string `json:"external_ip"`
}

func main() {
	log.Print("starting server...")
	http.HandleFunc("/run", handler)

	// Determine port for HTTP service.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("defaulting to port %s", port)
	}

	// Start HTTP server.
	log.Printf("listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	traceId := uuid.New()
	if r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") == "application/json" {
			var body NodePoolIpRequest
			err := json.NewDecoder(r.Body).Decode(&body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("[%v] recieved http call: method=%v, body=%v", traceId, r.Method, body)
			go func() {
				time.Sleep(10 * time.Second)
				log.Printf("[%v] 10 seconds later...", traceId)
			}()
		} else {
			w.WriteHeader(http.StatusUnsupportedMediaType)
		}
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
