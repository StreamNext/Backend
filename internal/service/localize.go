package service

// pick chooses a text for the caller's language: their own, then English, then the original.
// Name and overview fall back independently, because a translation may have one and not the other.
func pickName(tr map[Language]Text, original string, lang Language) string {
	return firstNonEmpty(tr[lang].Name, tr[LanguageEnglish].Name, original)
}

func pickOverview(tr map[Language]Text, original string, lang Language) string {
	return firstNonEmpty(tr[lang].Overview, tr[LanguageEnglish].Overview, original)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// genreNamesES names TheTVDB's genres in Spanish; TheTVDB itself only names them in English.
// A genre missing here keeps its English name.
var genreNamesES = map[int64]string{
	1: "Telenovela", 2: "Ciencia ficción", 3: "Reality", 4: "Noticias", 5: "Miniserie", 6: "Terror",
	7: "Hogar y jardín", 8: "Concurso", 9: "Gastronomía", 10: "Fantasía", 11: "Familiar", 12: "Drama",
	13: "Documental", 14: "Crimen", 15: "Comedia", 16: "Infantil", 17: "Animación", 18: "Aventura",
	19: "Acción", 21: "Deportes", 22: "Suspenso", 23: "Programa de entrevistas", 24: "Thriller",
	25: "Viajes", 26: "Western", 27: "Anime", 28: "Romance", 29: "Musical", 30: "Pódcast",
	31: "Misterio", 32: "Independiente", 33: "Historia", 34: "Bélico", 35: "Artes marciales",
	36: "Gala de premios",
}

func genreName(g Genre, lang Language) string {
	if lang == LanguageSpanish {
		if name, ok := genreNamesES[g.ID]; ok {
			return name
		}
	}
	return g.Name
}

func localizeSummary(h SearchHit, lang Language) SeriesSummary {
	return SeriesSummary{
		ID: h.ID, Name: pickName(h.Translations, h.OriginalName, lang), OriginalName: h.OriginalName,
		Overview: pickOverview(h.Translations, h.OriginalOverview, lang), Year: h.Year, Network: h.Network,
		Image: h.Image, Status: h.Status,
	}
}

func localizeDetail(d SeriesData, lang Language) SeriesDetail {
	genres := make([]GenreName, 0, len(d.Genres))
	for _, g := range d.Genres {
		genres = append(genres, GenreName{ID: g.ID, Name: genreName(g, lang)})
	}
	seasons := make([]SeasonSummary, 0, len(d.Seasons))
	for _, s := range d.Seasons {
		seasons = append(seasons, SeasonSummary{Number: s.Number, Name: s.Name, Image: s.Image, EpisodeCount: s.EpisodeCount})
	}
	days := d.AirsDays
	if days == nil {
		days = []string{}
	}
	return SeriesDetail{
		ID: d.ID, Slug: d.Slug, Name: pickName(d.Translations, d.OriginalName, lang), OriginalName: d.OriginalName,
		Overview: pickOverview(d.Translations, d.OriginalOverview, lang), Image: d.Image, Status: d.Status,
		FirstAired: d.FirstAired, LastAired: d.LastAired, NextAired: d.NextAired, AirsTime: d.AirsTime, AirsDays: days,
		OriginalCountry: d.OriginalCountry, OriginalLanguage: d.OriginalLanguage, Network: d.Network,
		AverageRuntime: d.AverageRuntime, Genres: genres, Seasons: seasons, SyncedAt: d.SyncedAt,
	}
}

func localizeEpisode(e EpisodeData, lang Language) Episode {
	return Episode{
		ID: e.ID, SeasonNumber: e.SeasonNumber, Number: e.Number, AbsoluteNumber: e.AbsoluteNumber,
		Name: pickName(e.Translations, e.OriginalName, lang), Overview: pickOverview(e.Translations, e.OriginalOverview, lang),
		Aired: e.Aired, Runtime: e.Runtime, Image: e.Image, FinaleType: e.FinaleType,
	}
}
