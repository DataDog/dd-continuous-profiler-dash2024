package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mongotrace "github.com/DataDog/dd-trace-go/contrib/go.mongodb.org/mongo-driver/v2/mongo"
	httptrace "github.com/DataDog/dd-trace-go/contrib/net/http/v2"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/DataDog/dd-trace-go/v2/profiler"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/natefinch/lumberjack.v2"
)

var logger *slog.Logger

var movies = cache(loadMovies)
var credits = loadCredits

// creditsByMovieId goes in here!

func main() {
	logWriter := &lumberjack.Logger{
		Filename:   "debug.log",
		MaxSize:    10,
		MaxBackups: 2,
	}

	handler := slog.NewTextHandler(logWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger = slog.New(handler)

	if err := profiler.Start(); err != nil {
		panic("starting profiling: " + err.Error())
	}
	if err := tracer.Start(); err != nil {
		panic("starting tracing: " + err.Error())
	}

	mux := httptrace.NewServeMux()
	mux.HandleFunc("/", randomMovieHandler)
	mux.HandleFunc("/credits", creditsHandler)
	mux.HandleFunc("/movies", moviesHandler)
	mux.HandleFunc("/old-movies", oldMoviesHandler)

	addr := "127.0.0.1:8082"
	version := os.Getenv("DD_VERSION")
	if version == "" {
		version = "(not set)"
	}

	// Warm these up at application start
	movies(context.Background())
	credits(context.Background())

	log.Printf("Running version %s with pid %d; Server starting on http://%s", version, os.Getpid(), addr)

	err := http.ListenAndServe(addr, mux)
	if err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func randomMovieHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	replyJSON(w, movies(ctx)[rand.Intn(len(movies(ctx)))])
}

func creditsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	movies := movies(ctx)
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
			Credits: creditsForMovie(ctx, movie),
		})
	}

	replyJSON(w, moviesWithCredits)
}

func creditsForMovie(ctx context.Context, movie Movie) []Credit {
	credits := credits(ctx)
	var movieCredits []Credit
	for _, credit := range credits {
		if credit.Id == movie.Id {
			movieCredits = append(movieCredits, credit)
		}
	}
	return movieCredits
}

func moviesHandler(w http.ResponseWriter, r *http.Request) {
	movies := movies(r.Context())
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

func oldMoviesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year := r.URL.Query().Get("year")
	if year == "" {
		year = "2010"
	}
	nStr := r.URL.Query().Get("n")
	if nStr == "" {
		nStr = "10"
	}
	limit, _ := strconv.Atoi(nStr)

	var oldMovies []Movie
	for _, movie := range movies(ctx) {
		if isOlderThan(year, movie) {
			oldMovies = append(oldMovies, movie)
		}
	}
	logger.Debug("Found the following oldMovies", "oldMovies", oldMovies)

	var limitedMovies []Movie
	for i, movie := range oldMovies {
		if i >= limit {
			break
		}
		limitedMovies = append(limitedMovies, movie)
	}
	logger.Debug("With limit, the result was", "limit", limit, "result", limitedMovies)

	replyJSON(w, limitedMovies)
}

func isOlderThan(year string, movie Movie) bool {
	result := movie.ReleaseDate < year
	logger.Debug("Is movie older than year?", "movie", movie, "year", year, "result", result)
	return result
}

func loadMovies(_ context.Context) []Movie {
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

func loadCredits(ctx context.Context) []Credit {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	opts := options.Client().ApplyURI("mongodb://localhost:27017")
	opts.Monitor = mongotrace.NewMonitor()
	client, err := mongo.Connect(ctx, opts)
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

func (m Movie) String() string {
	data, _ := json.MarshalIndent(m, "", "  ")
	return string(data)
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

func cache[T any](fn func(context.Context) T) func(context.Context) T {
	var once sync.Once
	var result T
	return func(ctx context.Context) T {
		once.Do(func() { result = fn(ctx) })
		return result
	}
}

func replyJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	jsonData, _ := json.MarshalIndent(data, "", "  ")
	w.Write(jsonData)
}
