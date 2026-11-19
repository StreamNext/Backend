package tvdb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/StreamNext/Backend/internal/service"
)

const (
	searchPageSize = 20
	// A guard against a response that never ends; the biggest series have a few thousand episodes.
	maxPages = 200
)

// TheTVDB's three-letter codes for the languages we keep.
var languageCodes = []struct {
	code string
	lang service.Language
}{{"eng", service.LanguageEnglish}, {"spa", service.LanguageSpanish}}

var _ service.CatalogSource = (*Client)(nil)

// Search looks series up by name. page is 1-based.
func (c *Client) Search(ctx context.Context, query string, page int) (service.SearchPage, error) {
	offset := (page - 1) * searchPageSize
	q := url.Values{"query": {query}, "type": {"series"}, "limit": {strconv.Itoa(searchPageSize)}, "offset": {strconv.Itoa(offset)}}
	var resp searchResponse
	if err := c.getJSON(ctx, "/search?"+q.Encode(), &resp); err != nil {
		return service.SearchPage{}, err
	}
	out := service.SearchPage{HasMore: offset+len(resp.Data) < resp.Links.TotalItems}
	for _, r := range resp.Data {
		if hit, ok := r.toHit(); ok {
			out.Hits = append(out.Hits, hit)
		}
	}
	return out, nil
}

// FetchSeries gets a series in full: the extended record, then its episodes in the default order in the
// original language, English and Spanish (TheTVDB only returns translated episode names that way).
func (c *Client) FetchSeries(ctx context.Context, id int64) (service.SeriesData, error) {
	var resp seriesResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/series/%d/extended?meta=translations&short=true", id), &resp); err != nil {
		return service.SeriesData{}, err
	}
	d := resp.Data.toData()

	original, err := c.episodes(ctx, fmt.Sprintf("/series/%d/episodes/default", id))
	if err != nil {
		return service.SeriesData{}, err
	}
	translated := map[service.Language][]episode{}
	for _, l := range languageCodes {
		if translated[l.lang], err = c.episodes(ctx, fmt.Sprintf("/series/%d/episodes/default/%s", id, l.code)); err != nil {
			return service.SeriesData{}, err
		}
	}
	d.Episodes = mergeEpisodes(original, translated)
	return d, nil
}

// episodes reads every page of an episodes route.
func (c *Client) episodes(ctx context.Context, path string) ([]episode, error) {
	var all []episode
	for page := 0; page < maxPages; page++ {
		var resp episodesResponse
		if err := c.getJSON(ctx, fmt.Sprintf("%s?page=%d", path, page), &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Data.Episodes...)
		if resp.Links.Next == nil {
			return all, nil
		}
	}
	return nil, fmt.Errorf("%w: %s has more than %d pages", service.ErrSourceUnavailable, path, maxPages)
}

// SeriesUpdates lists the series that changed since the given time. It reads only the `series` feed: an
// episode or season change always shows up there too, and the other feeds don't name their series.
func (c *Client) SeriesUpdates(ctx context.Context, since time.Time) ([]service.SeriesUpdate, error) {
	var out []service.SeriesUpdate
	for page := 0; page < maxPages; page++ {
		var resp updatesResponse
		path := fmt.Sprintf("/updates?since=%d&type=series&page=%d", since.Unix(), page)
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return nil, err
		}
		for _, u := range resp.Data {
			out = append(out, service.SeriesUpdate{ID: u.RecordID, Deleted: u.Method == "delete"})
		}
		if resp.Links.Next == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("%w: the updates feed has more than %d pages", service.ErrSourceUnavailable, maxPages)
}
