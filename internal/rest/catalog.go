package rest

import (
	"net/http"

	"github.com/StreamNext/Backend/gen/api"
)

// SearchSeries implements GET /v1/search/series. The generated wrapper has already parsed the query
// parameters; the catalog service owns the rules (length, page range) and the user's language.
func (s *Server) SearchSeries(w http.ResponseWriter, r *http.Request, params api.SearchSeriesParams) {
	res, err := s.catalog.Search(r.Context(), userID(r.Context()), params.Query, orZero(params.Page))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISearchResults(res))
}

// GetSeries implements GET /v1/series/{seriesId}.
func (s *Server) GetSeries(w http.ResponseWriter, r *http.Request, seriesId api.SeriesId) {
	d, err := s.catalog.Series(r.Context(), userID(r.Context()), seriesId)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISeries(d))
}

// ListEpisodes implements GET /v1/series/{seriesId}/episodes.
func (s *Server) ListEpisodes(w http.ResponseWriter, r *http.Request, seriesId api.SeriesId, params api.ListEpisodesParams) {
	eps, err := s.catalog.Episodes(r.Context(), userID(r.Context()), seriesId, params.Season)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIEpisodes(eps))
}
