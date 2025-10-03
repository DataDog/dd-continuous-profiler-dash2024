package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/credits", creditsHandler)

	version := os.Getenv("DD_VERSION")
	if version == "" {
		version = "(not set)"
	}

	log.Printf("Running version %s with pid %d", version, os.Getpid())
	log.Println("Server starting on http://127.0.0.1:8081")

	err := http.ListenAndServe("127.0.0.1:8081", nil)
	if err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func creditsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Credits endpoint was hit")

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"message": "Credits endpoint hit"}`)
}
