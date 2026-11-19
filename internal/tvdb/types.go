package tvdb

import "encoding/json"

// The parts of TheTVDB's responses we read. Pages are numbered from 0.

type links struct {
	Next       *string `json:"next"`
	TotalItems int     `json:"total_items"`
}

type searchResponse struct {
	Data  []searchResult `json:"data"`
	Links links          `json:"links"`
}

type searchResult struct {
	TVDBID       string    `json:"tvdb_id"`
	Name         string    `json:"name"`
	Overview     string    `json:"overview"`
	Translations stringMap `json:"translations"` // language code -> name
	Overviews    stringMap `json:"overviews"`    // language code -> overview
	Year         string    `json:"year"`
	Network      string    `json:"network"`
	ImageURL     string    `json:"image_url"`
	Thumbnail    string    `json:"thumbnail"`
	Status       string    `json:"status"`
	Type         string    `json:"type"`
}

// stringMap reads a JSON object of strings, and quietly reads anything else (null, a list) as empty.
type stringMap map[string]string

func (m *stringMap) UnmarshalJSON(data []byte) error {
	var obj map[string]string
	if err := json.Unmarshal(data, &obj); err != nil {
		*m = nil
		return nil
	}
	*m = obj
	return nil
}

type seriesResponse struct {
	Data seriesExtended `json:"data"`
}

type seriesExtended struct {
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	Slug              string            `json:"slug"`
	Image             string            `json:"image"`
	Overview          string            `json:"overview"`
	Status            namedValue        `json:"status"`
	FirstAired        string            `json:"firstAired"`
	LastAired         string            `json:"lastAired"`
	NextAired         string            `json:"nextAired"`
	AirsTime          string            `json:"airsTime"`
	AirsDays          map[string]bool   `json:"airsDays"`
	OriginalCountry   string            `json:"originalCountry"`
	OriginalLanguage  string            `json:"originalLanguage"`
	AverageRuntime    *int              `json:"averageRuntime"`
	DefaultSeasonType int64             `json:"defaultSeasonType"`
	Genres            []genre           `json:"genres"`
	Seasons           []season          `json:"seasons"`
	OriginalNetwork   *namedValue       `json:"originalNetwork"`
	LatestNetwork     *namedValue       `json:"latestNetwork"`
	LastUpdated       string            `json:"lastUpdated"`
	Translations      seriesTranslation `json:"translations"`
}

type namedValue struct {
	Name string `json:"name"`
}

type genre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type season struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Type   struct {
		ID int64 `json:"id"`
	} `json:"type"`
}

type seriesTranslation struct {
	// TheTVDB mixes the series' aliases into the name list, flagged isAlias; only real translations count.
	NameTranslations []struct {
		Name     string `json:"name"`
		Language string `json:"language"`
		IsAlias  bool   `json:"isAlias"`
	} `json:"nameTranslations"`
	OverviewTranslations []struct {
		Overview string `json:"overview"`
		Language string `json:"language"`
		IsAlias  bool   `json:"isAlias"`
	} `json:"overviewTranslations"`
}

type episodesResponse struct {
	Data struct {
		Episodes []episode `json:"episodes"`
	} `json:"data"`
	Links links `json:"links"`
}

type episode struct {
	ID             int64   `json:"id"`
	SeasonNumber   int     `json:"seasonNumber"`
	Number         int     `json:"number"`
	AbsoluteNumber int     `json:"absoluteNumber"`
	Name           string  `json:"name"`
	Overview       string  `json:"overview"`
	Aired          string  `json:"aired"`
	Runtime        *int    `json:"runtime"`
	Image          string  `json:"image"`
	FinaleType     *string `json:"finaleType"`
}

type updatesResponse struct {
	Data []struct {
		RecordID int64  `json:"recordId"`
		Method   string `json:"method"`
	} `json:"data"`
	Links links `json:"links"`
}
