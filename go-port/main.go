package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var MOVIES = cache(loadMovies)

// Fix 1
// var CREDITS = loadCredits
var CREDITS = cache(loadCredits)

// Fix 2
var CREDITS_BY_MOVIE_ID = cache(func() map[string][]Credit {
	result := make(map[string][]Credit)
	for _, credit := range CREDITS() {
		result[credit.Id] = append(result[credit.Id], credit)
	}
	return result
})

func main() {
	http.HandleFunc("/", randomMovieHandler)
	http.HandleFunc("/credits", creditsHandler)
	http.HandleFunc("/movies", moviesHandler)

	addr := "127.0.0.1:8082"
	version := os.Getenv("DD_VERSION")
	if version == "" {
		version = "(not set)"
	}

	// Warm these up at application start
	MOVIES()
	CREDITS()

	log.Printf("Running version %s with pid %d; Server starting on http://%s", version, os.Getpid(), addr)

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func randomMovieHandler(w http.ResponseWriter, r *http.Request) {
	replyJSON(w, MOVIES()[rand.Intn(len(MOVIES()))])
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

	var moviesWithCredits []MovieWithCredits
	for _, movie := range movies {
		moviesWithCredits = append(moviesWithCredits, MovieWithCredits{
			Movie:   movie,
			Credits: creditsForMovie(movie),
		})
	}

	replyJSON(w, moviesWithCredits)
}

// Fix 2
// func creditsForMovie(movie Movie) []Credit {
// 	credits := CREDITS()
// 	var movieCredits []Credit
// 	for _, credit := range credits {
// 		if credit.Id == movie.Id {
// 			movieCredits = append(movieCredits, credit)
// 		}
// 	}
// 	return movieCredits
// }

// Fix 2
func creditsForMovie(movie Movie) []Credit {
	return CREDITS_BY_MOVIE_ID()[movie.Id]
}

func moviesHandler(w http.ResponseWriter, r *http.Request) {
	movies := MOVIES()
	movies = sortByDescReleaseDate(movies)
	query := r.URL.Query().Get("q")
	if query == "" {
		query = r.URL.Query().Get("query")
	}
	if query != "" {
		var filteredMovies []Movie
		for _, movie := range movies {
			pattern, err := regexp.Compile(".*" + strings.ToUpper(query) + ".*")
			if err == nil && pattern.MatchString(strings.ToUpper(movie.Title)) {
				filteredMovies = append(filteredMovies, movie)
			}
		}
		movies = filteredMovies
	}
	replyJSON(w, movies)
}

func sortByDescReleaseDate(movies []Movie) []Movie {
	sortedMovies := make([]Movie, len(movies))
	copy(sortedMovies, movies)
	sort.Slice(sortedMovies, func(i, j int) bool {
		dateI, errI := time.Parse("1999-12-31", sortedMovies[i].ReleaseDate)
		if errI != nil {
			dateI = time.Time{}
		}
		dateJ, errJ := time.Parse("1999-12-31", sortedMovies[j].ReleaseDate)
		if errJ != nil {
			dateJ = time.Time{}
		}
		return dateI.After(dateJ)
	})
	return sortedMovies
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

func loadCredits() []Credit {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		panic("Failed to load credit data: " + err.Error())
	}
	defer client.Disconnect(ctx)

	creditsCollection := client.Database("moviesDB").Collection("credits")
	cursor, err := creditsCollection.Find(ctx, map[string]interface{}{}, options.Find().SetBatchSize(5000))
	if err != nil {
		panic("Failed to load credit data: " + err.Error())
	}
	defer cursor.Close(ctx)

	var credits []Credit
	for cursor.Next(ctx) {
		var credit Credit
		if err := cursor.Decode(&credit); err != nil {
			panic("Failed to load credit data: " + err.Error())
		}
		credits = append(credits, credit)
	}

	if err := cursor.Err(); err != nil {
		panic("Failed to load credit data: " + err.Error())
	}

	return credits
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

type Credit struct {
	Id   string   `json:"id"`
	Crew []string `json:"crew"`
	Cast []string `json:"cast"`
}

type MovieWithCredits struct {
	Movie   Movie    `json:"movie"`
	Credits []Credit `json:"credits"`
}

func cache[T any](fn func() T) func() T {
	var once sync.Once
	var result T
	return func() T {
		once.Do(func() { result = fn() })
		return result
	}
}

func replyJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}
