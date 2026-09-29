package backup

import (
	"fmt"
	"sort"
	"time"
)

type Info struct {
	ID        int64
	CreatedAt time.Time
	Size      int64
	Pinned    bool
}

type Policy struct {
	Keep          int   `json:"keep"`
	KeepDaily     int   `json:"keepDaily"`
	KeepWeekly    int   `json:"keepWeekly"`
	MaxTotalBytes int64 `json:"maxTotalBytes"`
}

// PlanRetention is grandfather-father-son thinning plus a hard
// size budget. Pinned archives are never deleted; the newest is never deleted.
func PlanRetention(items []Info, p Policy, _ time.Time) []int64 {
	sorted := append([]Info(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		return sorted[i].ID > sorted[j].ID
	})
	keep := map[int64]bool{}
	coveredDays := map[string]bool{}
	coveredWeeks := map[string]bool{}
	dailies, weeklies, recent := 0, 0, 0
	for _, it := range sorted {
		if it.Pinned {
			keep[it.ID] = true
			continue
		}
		t := it.CreatedAt.UTC()
		day := t.Format("2006-01-02")
		y, w := t.ISOWeek()
		week := fmt.Sprintf("%d-W%02d", y, w)
		switch {
		case recent < p.Keep:
			recent++
		case !coveredDays[day] && dailies < p.KeepDaily:
			dailies++
		case !coveredWeeks[week] && !coveredDays[day] && weeklies < p.KeepWeekly:
			weeklies++
		default:
			continue
		}
		keep[it.ID] = true
		coveredDays[day] = true
		coveredWeeks[week] = true
	}
	if len(sorted) > 0 {
		keep[sorted[0].ID] = true
	}
	if p.MaxTotalBytes > 0 {
		var total int64
		for _, it := range sorted {
			if keep[it.ID] {
				total += it.Size
			}
		}
		for i := len(sorted) - 1; i > 0 && total > p.MaxTotalBytes; i-- {
			it := sorted[i]
			if keep[it.ID] && !it.Pinned {
				delete(keep, it.ID)
				total -= it.Size
			}
		}
	}
	del := []int64{}
	for _, it := range sorted {
		if !keep[it.ID] {
			del = append(del, it.ID)
		}
	}
	sort.Slice(del, func(i, j int) bool { return del[i] < del[j] })
	return del
}
