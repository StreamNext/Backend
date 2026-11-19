package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
)

const (
	minQueryLen    = 2
	maxQueryLen    = 100
	maxSearchPage  = 25
	searchCacheTTL = 10 * time.Minute
	searchCacheMax = 500
)

type CatalogDeps struct {
	Source CatalogSource
	Repo   CatalogRepository
	Users  UserRepository // only to read the caller's language
	Now    func() time.Time
	Log    *zap.Logger
}

// Catalog serves search, series and episodes in the caller's language. A series we hold is read from our
// database and never refreshed here (the tvdb-sync job does that); one we don't hold yet is fetched from the
// source on this first request and stored.
type Catalog struct {
	CatalogDeps
	cache *searchCache
}

func NewCatalog(d CatalogDeps) *Catalog {
	return &Catalog{CatalogDeps: d, cache: newSearchCache(d.Now)}
}

func (c *Catalog) Search(ctx context.Context, userID uuid.UUID, query string, page int) (SearchResults, error) {
	query = strings.TrimSpace(query)
	if n := utf8.RuneCountInString(query); n < minQueryLen || n > maxQueryLen {
		return SearchResults{}, invalid("query must be 2 to 100 characters")
	}
	if page == 0 {
		page = 1
	}
	if page < 1 || page > maxSearchPage {
		return SearchResults{}, invalid("page must be 1 to 25")
	}
	lang, err := c.language(ctx, userID)
	if err != nil {
		return SearchResults{}, err
	}

	// The cache holds the source's answer, which is the same for everyone; it is localized per caller.
	key := fmt.Sprintf("%d|%s", page, strings.ToLower(query))
	res, ok := c.cache.get(key)
	if !ok {
		if res, err = c.Source.Search(ctx, query, page); err != nil {
			return SearchResults{}, c.sourceError(err)
		}
		c.cache.put(key, res)
	}
	out := SearchResults{Results: make([]SeriesSummary, 0, len(res.Hits)), Page: page, HasMore: res.HasMore}
	for _, h := range res.Hits {
		out.Results = append(out.Results, localizeSummary(h, lang))
	}
	return out, nil
}

func (c *Catalog) Series(ctx context.Context, userID uuid.UUID, id int64) (SeriesDetail, error) {
	lang, err := c.language(ctx, userID)
	if err != nil {
		return SeriesDetail{}, err
	}
	d, err := c.ensure(ctx, id)
	if err != nil {
		return SeriesDetail{}, err
	}
	return localizeDetail(d, lang), nil
}

func (c *Catalog) Episodes(ctx context.Context, userID uuid.UUID, id int64, season *int) ([]Episode, error) {
	if season != nil && *season < 0 {
		return nil, invalid("season must be 0 or more")
	}
	lang, err := c.language(ctx, userID)
	if err != nil {
		return nil, err
	}
	if _, err := c.ensure(ctx, id); err != nil {
		return nil, err
	}
	rows, err := c.Repo.GetEpisodes(ctx, id, season)
	if err != nil {
		return nil, fmt.Errorf("read episodes: %w", err)
	}
	out := make([]Episode, 0, len(rows))
	for _, e := range rows {
		out = append(out, localizeEpisode(e, lang))
	}
	return out, nil
}

// ensure returns the stored series, fetching and storing it first if we don't hold it yet.
func (c *Catalog) ensure(ctx context.Context, id int64) (SeriesData, error) {
	d, err := c.Repo.GetSeries(ctx, id)
	if err == nil {
		return d, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SeriesData{}, fmt.Errorf("read series: %w", err)
	}
	fetched, err := c.Source.FetchSeries(ctx, id)
	if err != nil {
		return SeriesData{}, c.sourceError(err)
	}
	if err := c.Repo.Save(ctx, fetched, c.Now()); err != nil {
		return SeriesData{}, fmt.Errorf("store series: %w", err)
	}
	c.Log.Info("series stored on first request", zap.Int64("series_id", id), zap.Int("episodes", len(fetched.Episodes)))
	if d, err = c.Repo.GetSeries(ctx, id); err != nil {
		return SeriesData{}, fmt.Errorf("read stored series: %w", err)
	}
	return d, nil
}

func (c *Catalog) language(ctx context.Context, userID uuid.UUID) (Language, error) {
	u, err := c.Users.GetByID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return "", errInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	if u.Language.Valid() {
		return u.Language, nil
	}
	return LanguageEnglish, nil
}

// sourceError turns the source's failures into answers the caller can act on, and logs the unexpected ones.
func (c *Catalog) sourceError(err error) error {
	switch {
	case errors.Is(err, ErrSourceNotFound):
		return apperr.New(apperr.NotFound, "series not found")
	case errors.Is(err, ErrSourceDisabled):
		return apperr.New(apperr.Unavailable, "the catalog is not configured on this server")
	case errors.Is(err, ErrSourceUnavailable):
		c.Log.Error("catalog source unavailable", zap.Error(err))
		return apperr.New(apperr.Unavailable, "the catalog is unavailable right now, try again shortly")
	default:
		return fmt.Errorf("catalog source: %w", err)
	}
}

// searchCache holds search pages for ten minutes.
// shortcut: in memory and per instance, bounded in size; move it to a shared cache if search traffic grows.
type searchCache struct {
	now func() time.Time
	mu  sync.Mutex
	m   map[string]cachedSearch
}

type cachedSearch struct {
	page    SearchPage
	expires time.Time
}

func newSearchCache(now func() time.Time) *searchCache {
	return &searchCache{now: now, m: map[string]cachedSearch{}}
}

func (c *searchCache) get(key string) (SearchPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || !c.now().Before(e.expires) {
		delete(c.m, key)
		return SearchPage{}, false
	}
	return e.page, true
}

func (c *searchCache) put(key string, page SearchPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= searchCacheMax {
		for k, e := range c.m { // make room: drop what has expired, and everything if that wasn't enough
			if !c.now().Before(e.expires) {
				delete(c.m, k)
			}
		}
		if len(c.m) >= searchCacheMax {
			c.m = map[string]cachedSearch{}
		}
	}
	c.m[key] = cachedSearch{page: page, expires: c.now().Add(searchCacheTTL)}
}
