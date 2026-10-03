package flp

import "strconv"

// MatchConfidence is "exact" | "name" | "unmatched".
type MatchConfidence string

const (
	ConfExact     MatchConfidence = "exact"
	ConfName      MatchConfidence = "name"
	ConfUnmatched MatchConfidence = "unmatched"
)

// Match is one pairing decision. Old==nil → added; New==nil → removed.
type Match[T any] struct {
	Old        *T
	New        *T
	Confidence MatchConfidence
}

func (m Match[T]) IsMatched() bool { return m.Old != nil && m.New != nil }
func (m Match[T]) IsAdded() bool   { return m.Old == nil && m.New != nil }
func (m Match[T]) IsRemoved() bool { return m.Old != nil && m.New == nil }

// pairByKey is the two-pass pairing workhorse. secondary may be nil, and may
// return ok=false to opt an entity out of name matching.
func pairByKey[T any](a, b []T, primary func(*T) string, secondary func(*T) (string, bool)) []Match[T] {
	matches := []Match[T]{}
	bPrimaryIdx := map[string]int{}
	for i := range b {
		k := primary(&b[i])
		if _, ok := bPrimaryIdx[k]; !ok {
			bPrimaryIdx[k] = i
		}
	}
	consumedB := map[int]bool{}

	unmatchedA := []int{}
	for i := range a {
		k := primary(&a[i])
		if idx, ok := bPrimaryIdx[k]; ok && !consumedB[idx] {
			matches = append(matches, Match[T]{Old: &a[i], New: &b[idx], Confidence: ConfExact})
			consumedB[idx] = true
		} else {
			unmatchedA = append(unmatchedA, i)
		}
	}

	if secondary != nil {
		still := []int{}
		for _, ai := range unmatchedA {
			sk, ok := secondary(&a[ai])
			if !ok {
				still = append(still, ai)
				continue
			}
			found := -1
			for i := range b {
				if consumedB[i] {
					continue
				}
				if bk, bok := secondary(&b[i]); bok && bk == sk {
					found = i
					break
				}
			}
			if found >= 0 {
				matches = append(matches, Match[T]{Old: &a[ai], New: &b[found], Confidence: ConfName})
				consumedB[found] = true
			} else {
				still = append(still, ai)
			}
		}
		unmatchedA = still
	}

	for _, ai := range unmatchedA {
		matches = append(matches, Match[T]{Old: &a[ai], New: nil, Confidence: ConfUnmatched})
	}
	for i := range b {
		if !consumedB[i] {
			matches = append(matches, Match[T]{Old: nil, New: &b[i], Confidence: ConfUnmatched})
		}
	}
	return matches
}

func optName(name *string) (string, bool) {
	if name != nil && *name != "" {
		return *name, true
	}
	return "", false
}

// MatchChannels pairs channels by iid, falling back to (kind, name).
func MatchChannels(oldC, newC []Channel) []Match[Channel] {
	return pairByKey(oldC, newC,
		func(c *Channel) string { return strconv.Itoa(c.Iid) },
		func(c *Channel) (string, bool) {
			if c.Name != nil && *c.Name != "" {
				return string(c.Kind) + "\x00" + *c.Name, true
			}
			return "", false
		})
}

// MatchPatterns pairs patterns by id, falling back to name.
func MatchPatterns(oldP, newP []Pattern) []Match[Pattern] {
	return pairByKey(oldP, newP,
		func(p *Pattern) string { return strconv.Itoa(p.ID) },
		func(p *Pattern) (string, bool) { return optName(p.Name) })
}

// MatchMixerInserts pairs inserts by index, falling back to name.
func MatchMixerInserts(oldI, newI []MixerInsert) []Match[MixerInsert] {
	return pairByKey(oldI, newI,
		func(i *MixerInsert) string { return strconv.Itoa(i.Index) },
		func(i *MixerInsert) (string, bool) { return optName(i.Name) })
}

// MatchTracks pairs tracks by index, falling back to name.
func MatchTracks(oldT, newT []Track) []Match[Track] {
	return pairByKey(oldT, newT,
		func(t *Track) string { return strconv.Itoa(t.Index) },
		func(t *Track) (string, bool) { return optName(t.Name) })
}

// MatchArrangements pairs arrangements by id, falling back to name.
func MatchArrangements(oldA, newA []Arrangement) []Match[Arrangement] {
	return pairByKey(oldA, newA,
		func(a *Arrangement) string { return strconv.Itoa(a.ID) },
		func(a *Arrangement) (string, bool) { return optName(a.Name) })
}

// ProjectMatch holds every per-entity match list.
type ProjectMatch struct {
	Channels     []Match[Channel]
	Patterns     []Match[Pattern]
	MixerInserts []Match[MixerInsert]
	Arrangements []Match[Arrangement]
}

// MatchProjects runs every per-entity matcher.
func MatchProjects(o, n *FLPProject) ProjectMatch {
	return ProjectMatch{
		Channels:     MatchChannels(o.Channels, n.Channels),
		Patterns:     MatchPatterns(o.Patterns, n.Patterns),
		MixerInserts: MatchMixerInserts(o.Inserts, n.Inserts),
		Arrangements: MatchArrangements(o.Arrangements, n.Arrangements),
	}
}
