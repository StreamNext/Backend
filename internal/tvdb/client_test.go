package tvdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/tvdb/stub"
)

func stubServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(stub.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// fake is a hand-built TheTVDB: login hands out numbered tokens, and handle serves everything else.
type fake struct {
	logins   atomic.Int32
	requests atomic.Int32
	handle   func(w http.ResponseWriter, r *http.Request, token string)
	loginErr int // status to answer login with instead of a token
	noToken  bool
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/v4/login" {
		n := f.logins.Add(1)
		switch {
		case f.loginErr != 0:
			w.WriteHeader(f.loginErr)
		case f.noToken:
			_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
		default:
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"token":"token-%d"}}`, n)
		}
		return
	}
	f.requests.Add(1)
	f.handle(w, r, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}

func ok(w http.ResponseWriter, body string) { _, _ = w.Write([]byte(body)) }

func TestLoginHappensOnceAndTheTokenIsReused(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		assert.Equal(t, "token-1", token)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		ok(w, `{"data":{"episodes":[]},"links":{"next":null}}`)
	}}
	c, _ := newClient(t, f, "my-key")
	for i := 0; i < 3; i++ {
		_, err := c.episodes(ctx(), "/series/1/episodes/default")
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, f.logins.Load())
}

func TestTheLoginSendsTheKey(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/login" {
			b := make([]byte, 200)
			n, _ := r.Body.Read(b)
			gotBody = string(b[:n])
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			ok(w, `{"data":{"token":"t"}}`)
			return
		}
		ok(w, `{"data":[],"links":{"next":null}}`)
	}))
	defer srv.Close()
	c := New(Config{APIKey: "secret-key", BaseURL: srv.URL + "/v4/"}, srv.Client(), time.Now) // trailing slash is tolerated
	_, err := c.SeriesUpdates(ctx(), epoch)
	require.NoError(t, err)
	assert.JSONEq(t, `{"apikey":"secret-key"}`, gotBody)
}

func TestTheTokenIsRenewedAfterTwentyFiveDays(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		ok(w, `{"data":{"episodes":[]},"links":{"next":null}}`)
	}}
	c, clk := newClient(t, f, "k")
	_, _ = c.episodes(ctx(), "/x")
	clk.Advance(25*24*time.Hour - time.Hour)
	_, _ = c.episodes(ctx(), "/x")
	assert.EqualValues(t, 1, f.logins.Load(), "still inside 25 days")
	clk.Advance(2 * time.Hour)
	_, _ = c.episodes(ctx(), "/x")
	assert.EqualValues(t, 2, f.logins.Load(), "renewed")
}

func TestARejectedTokenIsRenewedOnceAndTheCallRetried(t *testing.T) {
	f := &fake{}
	f.handle = func(w http.ResponseWriter, r *http.Request, token string) {
		if token == "token-1" { // TheTVDB has invalidated the first token
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ok(w, `{"data":{"episodes":[]},"links":{"next":null}}`)
	}
	c, _ := newClient(t, f, "k")
	_, err := c.episodes(ctx(), "/x")
	require.NoError(t, err)
	assert.EqualValues(t, 2, f.logins.Load())
	assert.EqualValues(t, 2, f.requests.Load(), "the call was made twice: rejected, then accepted")
}

func TestAnAlwaysRejectedTokenGivesUp(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) { w.WriteHeader(http.StatusUnauthorized) }}
	c, _ := newClient(t, f, "k")
	_, err := c.episodes(ctx(), "/x")
	requireErr(t, err, service.ErrSourceUnavailable)
	assert.EqualValues(t, 2, f.logins.Load(), "one renewal, not a loop")
}

func TestLoginProblemsAreUnavailableAndNeverEchoTheKey(t *testing.T) {
	for name, f := range map[string]*fake{
		"bad key":   {loginErr: http.StatusUnauthorized},
		"login 500": {loginErr: http.StatusInternalServerError},
		"no token":  {noToken: true},
	} {
		t.Run(name, func(t *testing.T) {
			f.handle = func(w http.ResponseWriter, r *http.Request, token string) {
				t.Fatal("no request should follow a failed login")
			}
			c, _ := newClient(t, f, "super-secret-key")
			_, err := c.SeriesUpdates(ctx(), epoch)
			requireErr(t, err, service.ErrSourceUnavailable)
			assert.NotContains(t, err.Error(), "super-secret-key")
		})
	}
}

func TestStatusCodesMapToTheServicesErrors(t *testing.T) {
	cases := map[int]error{
		http.StatusNotFound:            service.ErrSourceNotFound,
		http.StatusInternalServerError: service.ErrSourceUnavailable,
		http.StatusBadGateway:          service.ErrSourceUnavailable,
		http.StatusTooManyRequests:     service.ErrSourceUnavailable,
		http.StatusBadRequest:          service.ErrSourceUnavailable,
	}
	for status, want := range cases {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
				w.WriteHeader(status)
				ok(w, `{"status":"failure","message":"nope"}`)
			}}
			c, _ := newClient(t, f, "k")
			_, err := c.Raw(ctx(), "/series/1/extended")
			requireErr(t, err, want)
			if want == service.ErrSourceUnavailable {
				assert.Contains(t, err.Error(), fmt.Sprint(status))
			}
		})
	}
}

func TestMalformedAndOversizedBodiesAreUnavailable(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) { ok(w, `<html>not json</html>`) }}
	c, _ := newClient(t, f, "k")
	_, err := c.FetchSeries(ctx(), 1)
	requireErr(t, err, service.ErrSourceUnavailable)
	_, err = c.Search(ctx(), "x", 1)
	requireErr(t, err, service.ErrSourceUnavailable)
	_, err = c.SeriesUpdates(ctx(), epoch)
	requireErr(t, err, service.ErrSourceUnavailable)
}

func TestAnUnreachableServerIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := New(Config{APIKey: "k", BaseURL: srv.URL + "/v4"}, srv.Client(), time.Now)
	srv.Close()
	_, err := c.Search(ctx(), "x", 1)
	requireErr(t, err, service.ErrSourceUnavailable)
}

func TestWithoutAnAPIKeyEveryCallIsDisabled(t *testing.T) {
	c, _ := newClient(t, &fake{handle: func(http.ResponseWriter, *http.Request, string) { t.Fatal("must not be called") }}, "")
	_, err := c.Search(ctx(), "x", 1)
	requireErr(t, err, service.ErrSourceDisabled)
	_, err = c.FetchSeries(ctx(), 1)
	requireErr(t, err, service.ErrSourceDisabled)
	_, err = c.SeriesUpdates(ctx(), epoch)
	requireErr(t, err, service.ErrSourceDisabled)
}

func TestTheDefaultBaseURLIsTheRealAPI(t *testing.T) {
	assert.Equal(t, "https://api4.thetvdb.com/v4", New(Config{APIKey: "k"}, http.DefaultClient, time.Now).baseURL)
}

func TestACancelledContextStopsTheCall(t *testing.T) {
	c, _ := newClient(t, stub.Handler(), "k")
	cancelled, cancel := contextWithCancel()
	cancel()
	_, err := c.Search(cancelled, "x", 1)
	requireErr(t, err, service.ErrSourceUnavailable)
}

func TestABadURLIsUnavailable(t *testing.T) {
	c := New(Config{APIKey: "k", BaseURL: "http://bad host"}, http.DefaultClient, time.Now)
	_, err := c.Search(ctx(), "x", 1)
	requireErr(t, err, service.ErrSourceUnavailable)
}

// --- paging --------------------------------------------------------------------------------

func TestEpisodePagesAreNumberedFromZeroAndAllCollected(t *testing.T) {
	var pages []string
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		switch page {
		case "0":
			ok(w, `{"data":{"episodes":[{"id":1},{"id":2}]},"links":{"next":"https://x/?page=1"}}`)
		case "1":
			ok(w, `{"data":{"episodes":[{"id":3}]},"links":{"next":"https://x/?page=2"}}`)
		default:
			ok(w, `{"data":{"episodes":[{"id":4}]},"links":{"next":null}}`)
		}
	}}
	c, _ := newClient(t, f, "k")
	got, err := c.episodes(ctx(), "/series/1/episodes/default")
	require.NoError(t, err)
	assert.Len(t, got, 4)
	assert.Equal(t, []string{"0", "1", "2"}, pages)
}

func TestAnEndlessFeedIsCutOff(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		if strings.HasPrefix(r.URL.Path, "/v4/updates") {
			ok(w, `{"data":[],"links":{"next":"more"}}`)
			return
		}
		ok(w, `{"data":{"episodes":[]},"links":{"next":"more"}}`)
	}}
	c, _ := newClient(t, f, "k")
	_, err := c.episodes(ctx(), "/x")
	requireErr(t, err, service.ErrSourceUnavailable)
	_, err = c.SeriesUpdates(ctx(), epoch)
	requireErr(t, err, service.ErrSourceUnavailable)
}

func TestSearchHasMoreFollowsTheTotal(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		q := r.URL.Query()
		assert.Equal(t, "series", q.Get("type"))
		assert.Equal(t, "20", q.Get("limit"))
		assert.Equal(t, "the walking dead", q.Get("query"))
		ok(w, fmt.Sprintf(`{"data":[{"tvdb_id":"1","name":"A"},{"tvdb_id":"nope","name":"skipped: not a series id"},{"tvdb_id":"2","name":"B"}],"links":{"total_items":%d}}`, 45))
	}}
	c, _ := newClient(t, f, "k")

	page, err := c.Search(ctx(), "the walking dead", 1)
	require.NoError(t, err)
	assert.Len(t, page.Hits, 2, "a record without a usable series id is skipped")
	assert.True(t, page.HasMore, "3 of 45 seen, so there is more")

	f.handle = func(w http.ResponseWriter, r *http.Request, token string) {
		assert.Equal(t, "40", r.URL.Query().Get("offset"), "page 3 starts at result 40")
		ok(w, `{"data":[{"tvdb_id":"9","name":"Z"}],"links":{"total_items":41}}`)
	}
	page, err = c.Search(ctx(), "the walking dead", 3)
	require.NoError(t, err)
	assert.False(t, page.HasMore, "the last result of 41")
}

func TestUpdatesAreReadFromAllPagesAndDeletesAreMarked(t *testing.T) {
	var seen []string
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		q := r.URL.Query()
		assert.Equal(t, "series", q.Get("type"))
		assert.Equal(t, fmt.Sprint(epoch.Unix()), q.Get("since"))
		seen = append(seen, q.Get("page"))
		if q.Get("page") == "0" {
			ok(w, `{"data":[{"recordId":10,"method":"update"},{"recordId":11,"method":"create"}],"links":{"next":"more"}}`)
			return
		}
		ok(w, `{"data":[{"recordId":12,"method":"delete"}],"links":{"next":null}}`)
	}}
	c, _ := newClient(t, f, "k")
	got, err := c.SeriesUpdates(ctx(), epoch)
	require.NoError(t, err)
	assert.Equal(t, []service.SeriesUpdate{{ID: 10}, {ID: 11}, {ID: 12, Deleted: true}}, got)
	assert.Equal(t, []string{"0", "1"}, seen)
}

func TestAFailureOnALaterPageFailsTheWholeCall(t *testing.T) {
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
		if r.URL.Query().Get("page") == "0" {
			ok(w, `{"data":{"episodes":[{"id":1}]},"links":{"next":"more"}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}}
	c, _ := newClient(t, f, "k")
	_, err := c.episodes(ctx(), "/x")
	requireErr(t, err, service.ErrSourceUnavailable)
}

func TestFetchSeriesFailsIfAnyPassFails(t *testing.T) {
	for _, failing := range []string{"/extended", "/episodes/default?", "/episodes/default/eng", "/episodes/default/spa"} {
		t.Run(failing, func(t *testing.T) {
			f := &fake{handle: func(w http.ResponseWriter, r *http.Request, token string) {
				if strings.Contains(r.URL.String(), failing) {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if strings.Contains(r.URL.Path, "/extended") {
					ok(w, `{"data":{"id":1,"name":"X","status":{"name":"Ended"}}}`)
					return
				}
				ok(w, `{"data":{"episodes":[]},"links":{"next":null}}`)
			}}
			c, _ := newClient(t, f, "k")
			_, err := c.FetchSeries(ctx(), 1)
			requireErr(t, err, service.ErrSourceUnavailable)
		})
	}
}
