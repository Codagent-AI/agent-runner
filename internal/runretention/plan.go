package runretention

import (
	"sort"
	"time"
)

type Unit struct {
	Members  []Run
	Activity time.Time
	SourceID string
	Reason   string
}

//nolint:gocognit,funlen // Eligibility combines grouping, class-specific ages, and count ordering.
func Plan(all []Run, policy Policy, now time.Time) []Unit {
	if !policy.Enabled {
		return nil
	}
	parent := make(map[string]string, len(all))
	for i := range all {
		parent[all[i].ID] = all[i].ID
	}
	var root func(string) string
	root = func(id string) string {
		if parent[id] != id {
			parent[id] = root(parent[id])
		}
		return parent[id]
	}
	join := func(a, b string) {
		if _, ok := parent[a]; !ok {
			return
		}
		if _, ok := parent[b]; !ok {
			return
		}
		parent[root(a)] = root(b)
	}
	for i := range all {
		r := &all[i]
		if r.SourceRunID != "" {
			join(r.ID, r.SourceRunID)
		}
		for _, id := range r.AuditRunIDs {
			join(r.ID, id)
		}
	}
	groups := map[string]*Unit{}
	for i := range all {
		r := &all[i]
		key := root(r.ID)
		if groups[key] == nil {
			groups[key] = &Unit{}
		}
		u := groups[key]
		u.Members = append(u.Members, *r)
		if r.LastActivity.After(u.Activity) {
			u.Activity = r.LastActivity
		}
		if _, exists := parent[r.SourceRunID]; r.SourceRunID != "" && exists {
			u.SourceID = r.SourceRunID
		}
		if len(r.AuditRunIDs) > 0 {
			u.SourceID = r.ID
		}
	}
	var countable []Unit
	var age []Unit
	for _, ptr := range groups {
		u := *ptr
		protected, allFinished, allExpired := false, true, true
		for i := range u.Members {
			r := &u.Members[i]
			if r.SourceRunID != "" {
				if _, exists := parent[r.SourceRunID]; !exists {
					protected = true
				}
			}
			switch {
			case r.Class.protected():
				protected = true
			case r.Class == Finished:
				allExpired = allExpired && expired(u.Activity, policy.MaxAge, now)
			case r.Class == Stateless:
				allFinished = false
				allExpired = allExpired && expired(u.Activity, policy.MaxAge, now)
			case r.Class == Resumable:
				allFinished = false
				allExpired = allExpired && expired(u.Activity, policy.ResumableMaxAge, now)
			}
		}
		if protected {
			continue
		}
		if allFinished {
			countable = append(countable, u)
		}
		if allExpired {
			u.Reason = "age"
			age = append(age, u)
		}
	}
	sort.Slice(countable, func(i, j int) bool {
		if countable[i].Activity.Equal(countable[j].Activity) {
			return newestStart(&countable[i]).After(newestStart(&countable[j]))
		}
		return countable[i].Activity.After(countable[j].Activity)
	})
	selected := map[string]Unit{}
	for _, u := range age {
		selected[unitKey(&u)] = u
	}
	if policy.MaxRunsPerProject > 0 {
		for i := policy.MaxRunsPerProject; i < len(countable); i++ {
			u := countable[i]
			if _, exists := selected[unitKey(&u)]; !exists {
				u.Reason = "count"
				selected[unitKey(&u)] = u
			}
		}
	}
	result := make([]Unit, 0, len(selected))
	for _, u := range selected {
		sort.Slice(u.Members, func(i, j int) bool { return u.Members[i].ID < u.Members[j].ID })
		result = append(result, u)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Activity.Before(result[j].Activity) })
	return result
}

func unitKey(u *Unit) string {
	ids := make([]string, len(u.Members))
	for i := range u.Members {
		ids[i] = u.Members[i].ID
	}
	sort.Strings(ids)
	return ids[0]
}
func newestStart(u *Unit) time.Time {
	var t time.Time
	for i := range u.Members {
		if u.Members[i].StartTime.After(t) {
			t = u.Members[i].StartTime
		}
	}
	return t
}

// expired reports whether activity is older than limit; a zero limit never expires.
func expired(activity time.Time, limit time.Duration, now time.Time) bool {
	return limit != 0 && activity.Before(now.Add(-limit))
}
