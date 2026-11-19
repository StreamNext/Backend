package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

const (
	syncCursorKey = "tvdb_updates_cursor"
	// TheTVDB refuses a `since` older than three months; stay clear of it.
	maxCursorAge = 80 * 24 * time.Hour
	// The next run reads a little before the last run started, so nothing slips between runs.
	cursorMargin = 5 * time.Minute
	// Series not refreshed for this long are refreshed anyway, in case the changes feed missed something.
	safetyRefreshAge   = 30 * 24 * time.Hour
	safetyRefreshBatch = 50
)

type CatalogSyncDeps struct {
	Source CatalogSource
	Repo   CatalogRepository
	State  SyncStateRepository
	Now    func() time.Time
	Log    *zap.Logger
}

// CatalogSync keeps the series we store in step with the source, so the API never has to refresh anything.
type CatalogSync struct{ CatalogSyncDeps }

func NewCatalogSync(d CatalogSyncDeps) *CatalogSync { return &CatalogSync{d} }

type SyncResult struct {
	Refreshed, Deleted, Failed int
	FullRefresh                bool // the cursor was missing or too old, so every stored series was refreshed
}

// Run applies the source's changes since the last run to the series we store. The cursor only advances after
// a run with no failures, so a failed run is simply repeated; refreshing a series twice is harmless.
func (s *CatalogSync) Run(ctx context.Context) (SyncResult, error) {
	start := s.Now()
	var res SyncResult

	stored, err := s.Repo.IDs(ctx)
	if err != nil {
		return res, fmt.Errorf("list stored series: %w", err)
	}
	have := make(map[int64]bool, len(stored))
	for _, id := range stored {
		have[id] = true
	}
	done := map[int64]bool{} // refreshed or deleted in this run

	cursor, haveCursor, err := s.cursor(ctx)
	if err != nil {
		return res, err
	}
	if !haveCursor || start.Sub(cursor) > maxCursorAge {
		res.FullRefresh = true
		for _, id := range stored {
			s.refresh(ctx, id, &res, done)
		}
	} else {
		updates, err := s.Source.SeriesUpdates(ctx, cursor.Add(-cursorMargin))
		if err != nil {
			return res, fmt.Errorf("read updates: %w", err)
		}
		for _, u := range updates {
			if !have[u.ID] || done[u.ID] { // only series we store matter
				continue
			}
			if u.Deleted {
				s.remove(ctx, u.ID, &res, done)
			} else {
				s.refresh(ctx, u.ID, &res, done)
			}
		}
	}

	stale, err := s.Repo.Stale(ctx, start.Add(-safetyRefreshAge), safetyRefreshBatch)
	if err != nil {
		return res, fmt.Errorf("list stale series: %w", err)
	}
	for _, id := range stale {
		if !done[id] {
			s.refresh(ctx, id, &res, done)
		}
	}

	s.Log.Info("tvdb sync finished", zap.Int("refreshed", res.Refreshed), zap.Int("deleted", res.Deleted),
		zap.Int("failed", res.Failed), zap.Bool("full_refresh", res.FullRefresh))
	if res.Failed > 0 {
		return res, fmt.Errorf("%d series could not be refreshed; the cursor was not advanced", res.Failed)
	}
	if err := s.State.Set(ctx, syncCursorKey, start.UTC().Format(time.RFC3339), start); err != nil {
		return res, fmt.Errorf("save cursor: %w", err)
	}
	return res, nil
}

func (s *CatalogSync) cursor(ctx context.Context) (time.Time, bool, error) {
	raw, err := s.State.Get(ctx, syncCursorKey)
	if errors.Is(err, ErrNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read cursor: %w", err)
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil { // a damaged cursor is treated like a missing one: refresh everything
		s.Log.Warn("unreadable sync cursor, doing a full refresh", zap.String("value", raw))
		return time.Time{}, false, nil
	}
	return t, true, nil
}

// refresh re-fetches one stored series in full. A series the source no longer knows is removed. Failures are
// counted and logged, and never stop the run.
func (s *CatalogSync) refresh(ctx context.Context, id int64, res *SyncResult, done map[int64]bool) {
	d, err := s.Source.FetchSeries(ctx, id)
	switch {
	case errors.Is(err, ErrSourceNotFound):
		s.remove(ctx, id, res, done)
		return
	case err != nil:
		s.fail(id, "fetch", err, res)
		return
	}
	if err := s.Repo.Save(ctx, d, s.Now()); err != nil {
		s.fail(id, "save", err, res)
		return
	}
	res.Refreshed++
	done[id] = true
}

func (s *CatalogSync) remove(ctx context.Context, id int64, res *SyncResult, done map[int64]bool) {
	if err := s.Repo.Delete(ctx, id); err != nil {
		s.fail(id, "delete", err, res)
		return
	}
	res.Deleted++
	done[id] = true
	s.Log.Info("series removed: the source no longer has it", zap.Int64("series_id", id))
}

func (s *CatalogSync) fail(id int64, step string, err error, res *SyncResult) {
	res.Failed++
	s.Log.Error("series sync failed", zap.Int64("series_id", id), zap.String("step", step), zap.Error(err))
}
