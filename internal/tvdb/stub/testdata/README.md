# TheTVDB fixtures

Real TheTVDB v4 responses, recorded by `make tvdb-record` (`cmd/tvdbrecord`) and trimmed to the fields the client
reads. Descriptive text (overviews) is replaced with placeholders because this repository is public; names and
titles are kept. They drive the `tvdb` client tests and the CI stub (`cmd/tvdbstub`).

Naming: `search_<query>.json`, `series_<id>_extended.json`, `series_<id>_episodes_{original,eng,spa}.json`.
