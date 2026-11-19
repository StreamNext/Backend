package stub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rawSearch = `{"status":"success","data":[
 {"tvdb_id":"1","name":"A","overview":"A long and copyrighted description","year":"2020","network":"N","image_url":"u","thumbnail":"t","status":"Ended","type":"series","primary_language":"eng","first_air_time":"2020-01-01","aliases":["x"],"remote_ids":[1],
  "translations":{"eng":"English","spa":"Español","fra":"Français"},"overviews":{"eng":"English prose","fra":"Prose"}},
 {"tvdb_id":"2","name":"B"},{"tvdb_id":"3","name":"C"},{"tvdb_id":"4","name":"D"}],"links":{"total_items":9}}`

func parse(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func TestTrimSearch(t *testing.T) {
	out, err := TrimSearch([]byte(rawSearch))
	require.NoError(t, err)
	assert.NotContains(t, string(out), "copyrighted", "descriptive text never reaches the fixtures")
	assert.NotContains(t, string(out), "Français")
	assert.NotContains(t, string(out), "remote_ids")

	data := parse(t, out)["data"].([]any)
	require.Len(t, data, 3, "only the first three results")
	first := data[0].(map[string]any)
	assert.Equal(t, "A", first["name"], "names are kept")
	assert.Equal(t, "Overview placeholder", first["overview"])
	assert.Equal(t, map[string]any{"eng": "English", "spa": "Español"}, first["translations"])
	assert.Equal(t, map[string]any{"eng": "Overview placeholder (eng)"}, first["overviews"])
}

func TestTrimExtended(t *testing.T) {
	raw := `{"data":{"id":5,"name":"Show","slug":"show","overview":"secret prose","artworks":[{"x":1}],"characters":[1],
	 "status":{"id":2,"name":"Ended","keepUpdated":false},"genres":[{"id":12,"name":"Drama","slug":"drama","extra":1}],
	 "seasons":[{"id":9,"number":1,"name":"S1","image":"i","type":{"id":1,"name":"Aired Order","type":"official"},"companies":{}}],
	 "latestNetwork":{"name":"Net","id":4},"originalNetwork":null,
	 "translations":{"nameTranslations":[{"name":"Show","language":"eng","isPrimary":true},{"name":"Alias","language":"eng","isAlias":true},{"name":"Zeige","language":"deu"}],
	  "overviewTranslations":[{"overview":"english prose","language":"eng"},{"overview":"german prose","language":"deu"}]}}}`
	out, err := TrimExtended([]byte(raw))
	require.NoError(t, err)
	for _, leaked := range []string{"secret prose", "english prose", "german prose", "artworks", "characters", "Zeige", "keepUpdated"} {
		assert.NotContains(t, string(out), leaked)
	}
	d := parse(t, out)["data"].(map[string]any)
	assert.Equal(t, "Overview placeholder", d["overview"])
	assert.Equal(t, map[string]any{"name": "Ended"}, d["status"])
	assert.Equal(t, map[string]any{"name": "Net"}, d["latestNetwork"])
	assert.NotContains(t, d, "originalNetwork", "a null network is dropped")

	names := d["translations"].(map[string]any)["nameTranslations"].([]any)
	require.Len(t, names, 2, "German is dropped; English and the alias stay")
	assert.Equal(t, true, names[0].(map[string]any)["isPrimary"])
	assert.Equal(t, true, names[1].(map[string]any)["isAlias"], "alias flags are kept: the real data mixes aliases into names")
	over := d["translations"].(map[string]any)["overviewTranslations"].([]any)
	assert.Equal(t, "Overview placeholder (eng)", over[0].(map[string]any)["overview"])
}

func TestTrimEpisodes(t *testing.T) {
	raw := `{"data":{"series":{"name":"dropped"},"episodes":[{"id":1,"seasonNumber":1,"number":2,"name":"Title","overview":"episode prose","aired":"2020-01-01","runtime":50,"image":"i","finaleType":null,"year":"2020","nameTranslations":["eng"]},{"id":2,"overview":""}]},"links":{"next":"more","total_items":900}}`
	out, err := TrimEpisodes([]byte(raw))
	require.NoError(t, err)
	assert.NotContains(t, string(out), "episode prose")
	assert.NotContains(t, string(out), "dropped")
	assert.NotContains(t, string(out), "nameTranslations")

	m := parse(t, out)
	eps := m["data"].(map[string]any)["episodes"].([]any)
	require.Len(t, eps, 2)
	assert.Equal(t, "Title", eps[0].(map[string]any)["name"])
	assert.Equal(t, "Overview placeholder", eps[0].(map[string]any)["overview"])
	assert.Equal(t, "", eps[1].(map[string]any)["overview"], "an empty overview stays empty")
	assert.Nil(t, m["links"].(map[string]any)["next"], "recorded fixtures fit in one page")
}

func TestTrimmersRejectGarbage(t *testing.T) {
	for _, f := range []func([]byte) ([]byte, error){TrimSearch, TrimExtended, TrimEpisodes} {
		_, err := f([]byte("not json"))
		assert.Error(t, err)
	}
}

func TestSlugAndFileNames(t *testing.T) {
	assert.Equal(t, "la_casa_de_papel", Slug("  La Casa de Papel! "))
	assert.True(t, strings.HasSuffix(SearchFile("Breaking Bad"), "breaking_bad.json"))
	assert.Equal(t, "series_5_extended.json", ExtendedFile("5"))
	assert.Equal(t, "series_5_episodes_spa.json", EpisodesFile("5", "spa"))
}

func get(t *testing.T, h http.Handler, method, path, auth string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestHandlerLoginAndAuth(t *testing.T) {
	h := Handler()
	code, body := get(t, h, http.MethodPost, "/v4/login", "")
	assert.Equal(t, 200, code)
	assert.Contains(t, body, Token)

	for _, path := range []string{"/v4/search?query=x", "/v4/series/1/extended", "/v4/series/1/episodes/default", "/v4/series/1/episodes/default/eng", "/v4/updates"} {
		code, _ := get(t, h, http.MethodGet, path, "")
		assert.Equal(t, 401, code, path+" needs the token")
		code, _ = get(t, h, http.MethodGet, path, "wrong")
		assert.Equal(t, 401, code, path+" with a wrong token")
	}
}

func TestHandlerServesRecordedSeries(t *testing.T) {
	h := Handler()
	for _, path := range []string{
		"/v4/search?query=Breaking+Bad", "/v4/series/81189/extended?meta=translations", "/v4/series/81189/episodes/default?page=0",
		"/v4/series/81189/episodes/default/eng?page=0", "/v4/series/327417/episodes/default/spa?page=0",
	} {
		code, body := get(t, h, http.MethodGet, path, Token)
		assert.Equal(t, 200, code, path)
		assert.True(t, json.Valid([]byte(body)), path)
	}
}

func TestHandlerFallbacks(t *testing.T) {
	h := Handler()
	code, body := get(t, h, http.MethodGet, "/v4/series/424242/extended", Token)
	assert.Equal(t, 404, code, "an unknown series")
	assert.Contains(t, body, "NotFound")

	code, body = get(t, h, http.MethodGet, "/v4/search?query=no+such+show", Token)
	assert.Equal(t, 200, code)
	assert.Contains(t, body, `"data":[]`, "an unknown search is empty, not an error")

	_, body = get(t, h, http.MethodGet, "/v4/search?query=breaking+bad&offset=20", Token)
	assert.Contains(t, body, `"data":[]`, "the fixtures are one page")

	_, body = get(t, h, http.MethodGet, "/v4/series/81189/episodes/default?page=1", Token)
	assert.Contains(t, body, `"episodes":[]`)

	code, body = get(t, h, http.MethodGet, "/v4/updates?since=1&type=series&page=0", Token)
	assert.Equal(t, 200, code)
	assert.Contains(t, body, `"data":[]`, "the stub reports no changes")

	code, _ = get(t, h, http.MethodGet, "/v4/series/424242/episodes/default/eng", Token)
	assert.Equal(t, 404, code)
}
