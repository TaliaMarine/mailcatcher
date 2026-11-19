package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/TaliaMarine/mailcatcher/internal/database"
)

type Worker struct {
	db            *database.DB
	retentionDays int
	logger        *slog.Logger
}

func NewWorker(db *database.DB, retentionDays int, logger *slog.Logger) *Worker {
	return &Worker{
		db:            db,
		retentionDays: retentionDays,
		logger:        logger,
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("retention worker started", "retention_days", w.retentionDays)

	// Run once at startup
	w.run(ctx)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("retention worker stopped")
			return
		case <-ticker.C:
			w.run(ctx)
		}
	}
}

func (w *Worker) run(ctx context.Context) {
	cutoff := time.Now().UTC().AddDate(0, 0, -w.retentionDays)
	deleted, err := w.db.DeleteMessagesOlderThan(ctx, cutoff)
	if err != nil {
		w.logger.Error("retention cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		w.logger.Info("retention cleanup completed", "deleted", deleted, "cutoff", cutoff)
	}
}
