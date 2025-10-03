package main

import (
	"compress/gzip"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
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
	movies := MOVIES()
	query := r.URL.Query().Get("q")
	if query == "" {
		query = r.URL.Query().Get("query")
	}

	if query != "" {
		pattern, err := regexp.Compile("(?i)" + query)
		if err == nil {
			var filteredMovies []Movie
			for _, movie := range movies {
				if pattern.MatchString(movie.Title) {
					filteredMovies = append(filteredMovies, movie)
				}
			}
			movies = filteredMovies
		}
	}

	replyJSON(w, movies)
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

func replyJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
