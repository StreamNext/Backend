package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type syncEnv struct {
	svc    *CatalogSync
	source *fakeSource
	repo   *fakeCatalogRepo
	state  *fakeSyncState
	clock  *clock
	ctx    context.Context
}

func newSyncEnv() *syncEnv {
	e := &syncEnv{source: newFakeSource(), repo: newFakeCatalogRepo(), state: newFakeSyncState(), clock: &clock{t: epoch}, ctx: context.Background()}
	e.svc = NewCatalogSync(CatalogSyncDeps{Source: e.source, Repo: e.repo, State: e.state, Now: e.clock.Now, Log: zap.NewNop()})
	return e
}

// store puts a series in our database as if it had been opened, last synced at the given time.
func (e *syncEnv) store(id int64, syncedAt time.Time) {
	d := show(id)
	e.source.series[id] = d
	require.NoError(e.ctx2(), e.repo.Save(e.ctx, d, syncedAt))
}

func (e *syncEnv) ctx2() require.TestingT { return nopT{} }

type nopT struct{}

func (nopT) Errorf(string, ...interface{}) {}
func (nopT) FailNow()                      {}

func (e *syncEnv) cursor() string { return e.state.values[syncCursorKey] }

func (e *syncEnv) setCursor(t time.Time) {
	e.state.values[syncCursorKey] = t.UTC().Format(time.RFC3339)
}

func TestTheFirstRunWithNothingStoredJustSetsTheCursor(t *testing.T) {
	e := newSyncEnv()
	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, SyncResult{FullRefresh: true}, res)
	assert.Equal(t, epoch.Format(time.RFC3339), e.cursor())
	assert.Empty(t, e.source.fetchCalls)
}

func TestWithoutACursorEveryStoredSeriesIsRefreshed(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.store(2, epoch.Add(-time.Hour))

	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, SyncResult{Refreshed: 2, FullRefresh: true}, res)
	assert.Equal(t, epoch, e.repo.series[1].SyncedAt, "SyncedAt was moved forward")
	assert.Empty(t, e.source.updatesSince, "the changes feed is not read when everything is refreshed anyway")
}

func TestOnlyStoredSeriesThatChangedAreRefreshed(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.store(2, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))
	e.source.updates = []SeriesUpdate{{ID: 2}, {ID: 777}, {ID: 888}} // 777 and 888 are series nobody here has opened

	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, SyncResult{Refreshed: 1}, res)
	assert.Equal(t, 1, e.source.fetchCalls[2])
	assert.Zero(t, e.source.fetchCalls[1], "unchanged")
	assert.Zero(t, e.source.fetchCalls[777]+e.source.fetchCalls[888], "series we don't store are ignored")
	assert.NotContains(t, e.repo.series, int64(777))
}

func TestTheFeedIsReadFromJustBeforeTheLastRun(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	last := epoch.Add(-2 * time.Hour)
	e.setCursor(last)
	_, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, []time.Time{last.Add(-cursorMargin)}, e.source.updatesSince, "five minutes of overlap, so nothing slips between runs")
	assert.Equal(t, epoch.Format(time.RFC3339), e.cursor(), "and the cursor moves to this run's start")
}

func TestASeriesIsRefreshedOnceEvenIfListedTwice(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))
	e.source.updates = []SeriesUpdate{{ID: 1}, {ID: 1}, {ID: 1}}
	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Refreshed)
	assert.Equal(t, 1, e.source.fetchCalls[1])
}

func TestChangedEpisodesLandAndVanishedOnesAreRemoved(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))
	changed := show(1)
	changed.Episodes = changed.Episodes[:2] // two episodes disappear from TheTVDB
	changed.Episodes[0].OriginalName = "Renamed"
	changed.Episodes = append(changed.Episodes, EpisodeData{ID: 555, SeasonNumber: 3, Number: 1, OriginalName: "Brand new"})
	e.source.series[1] = changed
	e.source.updates = []SeriesUpdate{{ID: 1}}

	_, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Len(t, e.repo.episodes[1], 3)
	assert.Contains(t, e.repo.episodes[1], int64(555), "the new episode is there")
	assert.NotContains(t, e.repo.episodes[1], int64(100), "a vanished one is gone")
}

func TestADeletedSeriesIsRemoved(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.store(2, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))
	e.source.updates = []SeriesUpdate{{ID: 1, Deleted: true}, {ID: 999, Deleted: true}}

	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, SyncResult{Deleted: 1}, res)
	assert.NotContains(t, e.repo.series, int64(1))
	assert.Contains(t, e.repo.series, int64(2))
}

func TestASeriesTheSourceNoLongerKnowsIsRemoved(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))
	delete(e.source.series, 1) // TheTVDB answers 404 for it now
	e.source.updates = []SeriesUpdate{{ID: 1}}

	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, SyncResult{Deleted: 1}, res)
	assert.NotContains(t, e.repo.series, int64(1))
}

func TestAFailureDoesNotStopTheRunOrAdvanceTheCursor(t *testing.T) {
	e := newSyncEnv()
	for id := int64(1); id <= 3; id++ {
		e.store(id, epoch.Add(-time.Hour))
	}
	before := epoch.Add(-time.Hour)
	e.setCursor(before)
	e.source.updates = []SeriesUpdate{{ID: 1}, {ID: 2}, {ID: 3}}
	e.source.failSeries[2] = ErrSourceUnavailable

	res, err := e.svc.Run(e.ctx)
	require.Error(t, err)
	assert.Equal(t, SyncResult{Refreshed: 2, Failed: 1}, res, "the other two were still refreshed")
	assert.Equal(t, before.UTC().Format(time.RFC3339), e.cursor(), "so the failed one is retried next run")

	// The next run, with TheTVDB back, repeats the window and succeeds. Refreshing 1 and 3 again is harmless.
	e.clock.Advance(time.Hour)
	delete(e.source.failSeries, 2)
	res, err = e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Refreshed)
	assert.Equal(t, e.clock.Now().Format(time.RFC3339), e.cursor())
}

func TestSaveAndDeleteFailuresAreCountedToo(t *testing.T) {
	boom := errors.New("db down")
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))
	e.source.updates = []SeriesUpdate{{ID: 1}}
	e.repo.fail["Save"] = boom
	res, err := e.svc.Run(e.ctx)
	require.Error(t, err)
	assert.Equal(t, 1, res.Failed)

	e.repo.fail = failures{"Delete": boom}
	e.source.updates = []SeriesUpdate{{ID: 1, Deleted: true}}
	res, err = e.svc.Run(e.ctx)
	require.Error(t, err)
	assert.Equal(t, 1, res.Failed)
}

func TestAnOldOrDamagedCursorMeansRefreshEverything(t *testing.T) {
	for name, cursor := range map[string]string{
		"older than 80 days": epoch.Add(-81 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"unreadable":         "last tuesday",
	} {
		t.Run(name, func(t *testing.T) {
			e := newSyncEnv()
			e.store(1, epoch.Add(-time.Hour))
			e.store(2, epoch.Add(-time.Hour))
			e.state.values[syncCursorKey] = cursor
			res, err := e.svc.Run(e.ctx)
			require.NoError(t, err)
			assert.Equal(t, SyncResult{Refreshed: 2, FullRefresh: true}, res)
			assert.Empty(t, e.source.updatesSince, "TheTVDB would refuse a `since` this old")
			assert.Equal(t, epoch.Format(time.RFC3339), e.cursor())
		})
	}

	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-79 * 24 * time.Hour))
	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.False(t, res.FullRefresh, "79 days is still within the feed's reach")
}

func TestSeriesNotRefreshedForAMonthAreRefreshedAnyway(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-31*24*time.Hour)) // the changes feed never mentioned it
	e.store(2, epoch.Add(-time.Hour))
	e.setCursor(epoch.Add(-time.Hour))

	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Refreshed, "the safety net caught series 1")
	assert.Equal(t, 1, e.source.fetchCalls[1])
	assert.Zero(t, e.source.fetchCalls[2])
}

func TestTheSafetyNetIsBatchedAndNeverDoublesUp(t *testing.T) {
	e := newSyncEnv()
	for id := int64(1); id <= 60; id++ {
		e.store(id, epoch.Add(-40*24*time.Hour+time.Duration(id)*time.Minute))
	}
	e.setCursor(epoch.Add(-time.Hour))
	res, err := e.svc.Run(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, safetyRefreshBatch, res.Refreshed, "50 per run, oldest first")
	assert.Equal(t, 1, e.source.fetchCalls[1])

	// A series both updated and stale is refreshed once, not twice.
	e2 := newSyncEnv()
	e2.store(1, epoch.Add(-40*24*time.Hour))
	e2.setCursor(epoch.Add(-time.Hour))
	e2.source.updates = []SeriesUpdate{{ID: 1}}
	_, err = e2.svc.Run(e2.ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, e2.source.fetchCalls[1])
}

func TestSyncInfrastructureErrors(t *testing.T) {
	boom := errors.New("down")
	cases := map[string]func(*syncEnv){
		"list stored": func(e *syncEnv) { e.repo.fail["IDs"] = boom },
		"read cursor": func(e *syncEnv) { e.state.fail["Get"] = boom },
		"read feed":   func(e *syncEnv) { e.setCursor(epoch.Add(-time.Hour)); e.source.fail["SeriesUpdates"] = boom },
		"list stale":  func(e *syncEnv) { e.setCursor(epoch.Add(-time.Hour)); e.repo.fail["Stale"] = boom },
		"save cursor": func(e *syncEnv) { e.state.fail["Set"] = boom },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSyncEnv()
			setup(e)
			_, err := e.svc.Run(e.ctx)
			assert.Error(t, err)
		})
	}
}

func TestSyncRespectsTheSourceBeingUnreachableForTheFeed(t *testing.T) {
	e := newSyncEnv()
	e.store(1, epoch.Add(-time.Hour))
	before := epoch.Add(-time.Hour)
	e.setCursor(before)
	e.source.fail["SeriesUpdates"] = fmt.Errorf("%w: connection refused", ErrSourceUnavailable)
	_, err := e.svc.Run(e.ctx)
	require.Error(t, err)
	assert.Equal(t, before.UTC().Format(time.RFC3339), e.cursor(), "the cursor stays put")
}
