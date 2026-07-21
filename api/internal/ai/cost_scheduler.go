package ai

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// RunDailyCostSync re-pulls the most recent seven UTC days immediately and
// then once per day. A missing Admin-key configuration is expected and leaves
// the scheduler idle until the next cycle.
func RunDailyCostSync(ctx context.Context, service *CostService, report func(error)) {
	if service == nil {
		return
	}
	run := func() {
		now := time.Now().UTC()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
		for day := start; !day.After(now); day = day.AddDate(0, 0, 1) {
			_, err := service.Sync(ctx, day, day.AddDate(0, 0, 1))
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && report != nil {
				report(err)
			}
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
