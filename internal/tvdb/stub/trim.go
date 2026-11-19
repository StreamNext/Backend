package stub

import (
	"encoding/json"
	"fmt"
)

// The recorder saves real TheTVDB responses as test fixtures. This repository is public, so every response is
// cut down to the fields we read, and descriptive text (overviews) is replaced with placeholders. Names and
// titles are short factual labels and are kept, because the language tests need real translations.

const overviewPlaceholder = "Overview placeholder"

type obj = map[string]any

func decode(raw []byte) (obj, error) {
	var o obj
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return o, nil
}

func encode(o obj) ([]byte, error) { return json.MarshalIndent(o, "", " ") }

func pick(src obj, keys ...string) obj {
	out := obj{}
	for _, k := range keys {
		if v, ok := src[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}

// placeholder replaces a non-empty text with a placeholder, optionally tagged with the language code.
func placeholder(v any, lang string) any {
	s, _ := v.(string)
	if s == "" {
		return ""
	}
	if lang != "" {
		return overviewPlaceholder + " (" + lang + ")"
	}
	return overviewPlaceholder
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func asObj(v any) obj {
	o, _ := v.(obj)
	return o
}

// keepLanguages keeps only the English and Spanish entries of a {language code: text} object.
func keepLanguages(m obj, textIsOverview bool) obj {
	out := obj{}
	for _, code := range []string{"eng", "spa"} {
		if v, ok := m[code]; ok {
			if textIsOverview {
				v = placeholder(v, code)
			}
			out[code] = v
		}
	}
	return out
}

// TrimSearch keeps the first few results of a /search response.
func TrimSearch(raw []byte) ([]byte, error) {
	in, err := decode(raw)
	if err != nil {
		return nil, err
	}
	var results []any
	for _, item := range list(in["data"]) {
		r := asObj(item)
		out := pick(r, "tvdb_id", "name", "year", "network", "image_url", "thumbnail", "status", "type", "primary_language", "first_air_time")
		out["overview"] = placeholder(r["overview"], "")
		out["translations"] = keepLanguages(asObj(r["translations"]), false)
		out["overviews"] = keepLanguages(asObj(r["overviews"]), true)
		results = append(results, out)
		if len(results) == 3 {
			break
		}
	}
	return encode(obj{"status": "success", "data": results, "links": obj{"next": nil, "total_items": len(results), "page_size": 20}})
}

// TrimExtended keeps what we read from a /series/{id}/extended response.
func TrimExtended(raw []byte) ([]byte, error) {
	in, err := decode(raw)
	if err != nil {
		return nil, err
	}
	d := asObj(in["data"])
	out := pick(d, "id", "name", "slug", "image", "firstAired", "lastAired", "nextAired", "airsTime", "airsDays",
		"originalCountry", "originalLanguage", "averageRuntime", "defaultSeasonType", "lastUpdated")
	out["overview"] = placeholder(d["overview"], "")
	out["status"] = pick(asObj(d["status"]), "name")
	for _, k := range []string{"originalNetwork", "latestNetwork"} {
		if n := asObj(d[k]); n != nil {
			out[k] = pick(n, "name")
		}
	}

	var genres []any
	for _, g := range list(d["genres"]) {
		genres = append(genres, pick(asObj(g), "id", "name", "slug"))
	}
	out["genres"] = genres

	var seasons []any
	for _, s := range list(d["seasons"]) {
		so := asObj(s)
		season := pick(so, "id", "number", "name", "image")
		season["type"] = pick(asObj(so["type"]), "id", "type")
		seasons = append(seasons, season)
	}
	out["seasons"] = seasons

	tr := asObj(d["translations"])
	trimmed := obj{}
	var names, overviews []any
	for _, n := range list(tr["nameTranslations"]) {
		no := asObj(n)
		if lang, _ := no["language"].(string); lang == "eng" || lang == "spa" {
			names = append(names, pick(no, "name", "language", "isPrimary", "isAlias"))
		}
	}
	for _, o := range list(tr["overviewTranslations"]) {
		oo := asObj(o)
		if lang, _ := oo["language"].(string); lang == "eng" || lang == "spa" {
			p := pick(oo, "language", "isPrimary", "isAlias")
			p["overview"] = placeholder(oo["overview"], lang)
			overviews = append(overviews, p)
		}
	}
	trimmed["nameTranslations"], trimmed["overviewTranslations"] = names, overviews
	out["translations"] = trimmed
	return encode(obj{"status": "success", "data": out})
}

// TrimEpisodes keeps what we read from one page of a /series/{id}/episodes/... response.
func TrimEpisodes(raw []byte) ([]byte, error) {
	in, err := decode(raw)
	if err != nil {
		return nil, err
	}
	var episodes []any
	for _, e := range list(asObj(in["data"])["episodes"]) {
		eo := asObj(e)
		out := pick(eo, "id", "seasonNumber", "number", "absoluteNumber", "name", "aired", "runtime", "image", "finaleType")
		out["overview"] = placeholder(eo["overview"], "")
		episodes = append(episodes, out)
	}
	return encode(obj{"status": "success", "data": obj{"episodes": episodes},
		"links": obj{"next": nil, "total_items": len(episodes), "page_size": 500}})
}
