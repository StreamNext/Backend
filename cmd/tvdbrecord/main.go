// Command tvdbrecord saves real TheTVDB responses as the fixtures behind the tvdb client's tests and the CI
// stub: `make tvdb-record` (it needs TVDB_API_KEY). Responses are trimmed and descriptive text is replaced
// with placeholders, because this repository is public (see internal/tvdb/stub/trim.go).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/StreamNext/Backend/internal/tvdb"
	"github.com/StreamNext/Backend/internal/tvdb/stub"
)

// The series recorded: an English-original show and a Spanish-original one, so the language fallback is
// tested on both. Add an id here and run `make tvdb-record` to record another.
var recorded = []struct {
	id    string
	query string
}{
	{"81189", "breaking bad"},
	{"327417", "la casa de papel"},
}

func main() {
	dir := flag.String("dir", "internal/tvdb/stub/testdata", "where to write the fixtures")
	flag.Parse()
	c := tvdb.New(tvdb.Config{APIKey: os.Getenv("TVDB_API_KEY"), BaseURL: os.Getenv("TVDB_BASE_URL")}, &http.Client{Timeout: 30 * time.Second}, time.Now)
	ctx := context.Background()

	save := func(name, path string, trim func([]byte) ([]byte, error)) {
		raw, err := c.Raw(ctx, path)
		if err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		out, err := trim(raw)
		if err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(*dir, name), append(out, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %-45s (%d bytes)\n", name, len(out))
	}
	for _, s := range recorded {
		q := url.Values{"query": {s.query}, "type": {"series"}, "limit": {"3"}}
		save(stub.SearchFile(s.query), "/search?"+q.Encode(), stub.TrimSearch)
		save(stub.ExtendedFile(s.id), fmt.Sprintf("/series/%s/extended?meta=translations&short=true", s.id), stub.TrimExtended)
		save(stub.EpisodesFile(s.id, "original"), fmt.Sprintf("/series/%s/episodes/default?page=0", s.id), stub.TrimEpisodes)
		for _, lang := range []string{"eng", "spa"} {
			save(stub.EpisodesFile(s.id, lang), fmt.Sprintf("/series/%s/episodes/default/%s?page=0", s.id, lang), stub.TrimEpisodes)
		}
	}
}
