package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

var MOVIES = cache(loadMovies)

func main() {
	http.HandleFunc("/credits", creditsHandler)

	addr := "127.0.0.1:8082"
	version := os.Getenv("DD_VERSION")
	if version == "" {
		version = "(not set)"
	}

	log.Printf("Running version %s with pid %d; Server starting on http://%s", version, os.Getpid(), addr)

	err := http.ListenAndServe(addr, nil)
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

func loadMovies() []Movie {
	file, err := os.Open("../movies-v2.json.gz")
	if err != nil {
		panic("Failed to load movie data: " + err.Error())
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		panic("Failed to load movie data: " + err.Error())
	}
	defer gzipReader.Close()
	var movies []Movie
	if err := json.NewDecoder(gzipReader).Decode(&movies); err != nil {
		panic("Failed to load movie data: " + err.Error())
	}
	return movies
}

type Movie struct {
	Id            string `json:"id"`
	OriginalTitle string `json:"originalTitle"`
	Overview      string `json:"overview"`
	ReleaseDate   string `json:"releaseDate"`
	Tagline       string `json:"tagline"`
	Title         string `json:"title"`
	VoteAverage   string `json:"voteAverage"`
}

func cache(fn func() []Movie) func() []Movie {
	var once sync.Once
	var result []Movie
	return func() []Movie {
		once.Do(func() { result = fn() })
		return result
	}
}
