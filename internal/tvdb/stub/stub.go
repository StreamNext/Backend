// Package stub is a stand-in for TheTVDB that replays recorded fixtures. The tvdb client's tests and the CI
// stack (cmd/tvdbstub, so Bruno can run the catalog end to end with no API key) both use it.
package stub

import (
	"embed"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

//go:embed testdata
var fixtures embed.FS

// Token is the only login token the stub accepts.
const Token = "stub-token"

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a search query into its fixture file name part: "Breaking Bad" -> "breaking_bad".
func Slug(query string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(query), "_"), "_")
}

func SearchFile(query string) string      { return "search_" + Slug(query) + ".json" }
func ExtendedFile(id string) string       { return "series_" + id + "_extended.json" }
func EpisodesFile(id, lang string) string { return "series_" + id + "_episodes_" + lang + ".json" }

// Handler serves the recorded fixtures under /v4, the way TheTVDB would.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v4/login", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"status":"success","data":{"token":%q}}`, Token))
	})
	mux.HandleFunc("GET /v4/search", authed(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "" && r.URL.Query().Get("offset") != "0" {
			writeJSON(w, http.StatusOK, emptySearch)
			return
		}
		serve(w, SearchFile(r.URL.Query().Get("query")), emptySearch)
	}))
	mux.HandleFunc("GET /v4/series/{id}/extended", authed(func(w http.ResponseWriter, r *http.Request) {
		serve(w, ExtendedFile(r.PathValue("id")), "")
	}))
	mux.HandleFunc("GET /v4/series/{id}/episodes/default", authed(episodes("original")))
	mux.HandleFunc("GET /v4/series/{id}/episodes/default/{lang}", authed(episodes("")))
	mux.HandleFunc("GET /v4/updates", authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"status":"success","data":[],"links":{"next":null,"total_items":0,"page_size":500}}`)
	}))
	return mux
}

const emptySearch = `{"status":"success","data":[],"links":{"next":null,"total_items":0,"page_size":20}}`
const emptyEpisodes = `{"status":"success","data":{"episodes":[]},"links":{"next":null,"total_items":0,"page_size":500}}`

func episodes(fixedLang string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Query().Get("page"); p != "" && p != "0" { // every fixture fits in page 0
			writeJSON(w, http.StatusOK, emptyEpisodes)
			return
		}
		lang := fixedLang
		if lang == "" {
			lang = r.PathValue("lang")
		}
		serve(w, EpisodesFile(r.PathValue("id"), lang), "")
	}
}

// serve writes a fixture; if there is none it writes fallback, or a 404 when there is no fallback.
func serve(w http.ResponseWriter, file, fallback string) {
	body, err := fixtures.ReadFile("testdata/" + file)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, string(body))
	case fallback != "":
		writeJSON(w, http.StatusOK, fallback)
	default:
		writeJSON(w, http.StatusNotFound, `{"status":"failure","message":"NotFound"}`)
	}
}

func authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+Token {
			writeJSON(w, http.StatusUnauthorized, `{"status":"failure","message":"Unauthorized"}`)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
