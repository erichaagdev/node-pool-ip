package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

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

type person struct {
	Name string `json:"name"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("recieved http call: method=%v", r.Method)
	if r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") == "application/json" {
			var p person
			err := json.NewDecoder(r.Body).Decode(&p)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if p.Name == "" {
				_, _ = fmt.Fprintf(w, "Hello World!\n")
			} else {
				_, _ = fmt.Fprintf(w, "Hello %s!\n", p.Name)
			}
		} else {
			w.WriteHeader(http.StatusUnsupportedMediaType)
		}
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
