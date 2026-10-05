package pyflp

import (
	"fmt"
	"sort"
)

// IndexedEvent pairs an event with the index (R) at which it occurred in the
// root tree. Trees share IndexedEvent pointers, so changing R or E is
// visible in every tree which holds the same entry.
type IndexedEvent struct {
	R int
	E *Event
}

// Select is the result of a selection callback.
type Select int

const (
	// Skip ignores the event (Python: None).
	Skip Select = iota
	// Include keeps the event (Python: True).
	Include
	// Cut ends the current group (Python: False). Subtrees yields what has
	// been collected so far; Subtree treats it like Skip.
	Cut
)

// EventTree provides mutable "views" which propagate changes back to their
// parents. The tree is analogous to the hierarchy used by models.
type EventTree struct {
	parent   *EventTree
	root     *EventTree
	children []*EventTree
	lst      []*IndexedEvent // always sorted by R (stable)
}

// NewEventTree creates a tree with an optional parent. init entries are
// shared with the caller.
func NewEventTree(parent *EventTree, init []*IndexedEvent) *EventTree {
	t := &EventTree{parent: parent}
	t.lst = append(t.lst, init...)
	sort.SliceStable(t.lst, func(i, j int) bool { return t.lst[i].R < t.lst[j].R })
	if parent != nil {
		parent.children = append(parent.children, t)
	}
	p := parent
	for p != nil && p.parent != nil {
		p = p.parent
	}
	if p != nil {
		t.root = p
	} else {
		t.root = t
	}
	return t
}

// NewRootTree builds the root tree of a list of events in file order.
func NewRootTree(events []*Event) *EventTree {
	init := make([]*IndexedEvent, len(events))
	for i, e := range events {
		init[i] = &IndexedEvent{R: i, E: e}
	}
	return NewEventTree(nil, init)
}

// Parent returns the immediate ancestor or nil for the root.
func (t *EventTree) Parent() *EventTree { return t.parent }

// Root returns the parent of all parent trees (itself for the root).
func (t *EventTree) Root() *EventTree { return t.root }

// Children returns the views derived from this tree.
func (t *EventTree) Children() []*EventTree { return t.children }

// Len returns the number of events.
func (t *EventTree) Len() int { return len(t.lst) }

// Entries returns the indexed events (shared, not copied).
func (t *EventTree) Entries() []*IndexedEvent { return t.lst }

// Events returns the events in order.
func (t *EventTree) Events() []*Event {
	out := make([]*Event, len(t.lst))
	for i, ie := range t.lst {
		out[i] = ie.E
	}
	return out
}

// Contains reports whether an event with the ID exists.
func (t *EventTree) Contains(id EventID) bool {
	for _, ie := range t.lst {
		if ie.E.id == id {
			return true
		}
	}
	return false
}

// ContainsAny reports whether any of the IDs exists.
func (t *EventTree) ContainsAny(ids ...EventID) bool {
	for _, id := range ids {
		if t.Contains(id) {
			return true
		}
	}
	return false
}

func idIn(id EventID, ids []EventID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func (t *EventTree) getIE(ids ...EventID) []*IndexedEvent {
	var out []*IndexedEvent
	for _, ie := range t.lst {
		if idIn(ie.E.id, ids) {
			out = append(out, ie)
		}
	}
	return out
}

// Ids returns the set of IDs present, in ascending order.
func (t *EventTree) Ids() []EventID {
	seen := map[EventID]bool{}
	var out []EventID
	for _, ie := range t.lst {
		if !seen[ie.E.id] {
			seen[ie.E.id] = true
			out = append(out, ie.E.id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Indexes returns the root indexes of all events, ascending.
func (t *EventTree) Indexes() []int {
	out := make([]int, len(t.lst))
	for i, ie := range t.lst {
		out[i] = ie.R
	}
	sort.Ints(out)
	return out
}

// Count returns the number of events with the ID.
func (t *EventTree) Count(id EventID) int { return len(t.getIE(id)) }

// Get returns the events whose ID is one of ids, in order.
func (t *EventTree) Get(ids ...EventID) []*Event {
	var out []*Event
	for _, ie := range t.lst {
		if idIn(ie.E.id, ids) {
			out = append(out, ie.E)
		}
	}
	return out
}

// First returns the first event with the ID, or an error wrapping
// ErrEventNotFound.
func (t *EventTree) First(id EventID) (*Event, error) {
	for _, ie := range t.lst {
		if ie.E.id == id {
			return ie.E, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrEventNotFound, IDName(id))
}

// FirstOf returns the first event with the given ID or nil.
func (t *EventTree) FirstOf(id EventID) *Event {
	e, _ := t.First(id)
	return e
}

// String summarises the tree.
func (t *EventTree) String() string {
	return fmt.Sprintf("EventTree(%d IDs, %d events)", len(t.Ids()), t.Len())
}

// Equal compares the contained events (like PyFLP, by index and event).
func (t *EventTree) Equal(o *EventTree) bool {
	if o == nil || len(t.lst) != len(o.lst) {
		return false
	}
	for i := range t.lst {
		if t.lst[i].R != o.lst[i].R || !t.lst[i].E.Equal(o.lst[i].E) {
			return false
		}
	}
	return true
}

func (t *EventTree) addEntry(ie *IndexedEvent) {
	i := sort.Search(len(t.lst), func(i int) bool { return t.lst[i].R > ie.R })
	t.lst = append(t.lst, nil)
	copy(t.lst[i+1:], t.lst[i:])
	t.lst[i] = ie
}

func (t *EventTree) removeEntry(ie *IndexedEvent) {
	for i, x := range t.lst {
		if x == ie {
			t.lst = append(t.lst[:i], t.lst[i+1:]...)
			return
		}
	}
}

func (t *EventTree) recursive(action func(*EventTree)) {
	action(t)
	for a := t.parent; a != nil; a = a.parent {
		action(a)
	}
}

// Append inserts an event at the end of this tree (and all parents).
func (t *EventTree) Append(e *Event) { t.Insert(t.Len(), e) }

// Insert inserts an event at position pos in this and all parent trees.
func (t *EventTree) Insert(pos int, e *Event) {
	rootIdx := 0
	if t.Len() > 0 {
		idx := t.Indexes()
		if pos < 0 {
			pos = 0
		}
		if pos >= len(idx) {
			rootIdx = idx[len(idx)-1] + 1 // append after the last event
		} else {
			rootIdx = idx[pos]
		}
	} else {
		rootIdx = len(t.root.lst)
	}
	// Shift all root indexes at or after rootIdx by +1 to prevent collisions.
	for _, ie := range t.root.lst {
		if ie.R >= rootIdx {
			ie.R++
		}
	}
	entry := &IndexedEvent{R: rootIdx, E: e}
	t.recursive(func(et *EventTree) { et.addEntry(entry) })
}

// Pop removes and returns the pos-th event with the ID in this tree and all
// parents.
func (t *EventTree) Pop(id EventID, pos int) (*Event, error) {
	matches := t.getIE(id)
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrEventNotFound, IDName(id))
	}
	if pos < 0 || pos >= len(matches) {
		return nil, fmt.Errorf("%w: %s at position %d", ErrEventNotFound, IDName(id), pos)
	}
	ie := matches[pos]
	t.recursive(func(et *EventTree) { et.removeEntry(ie) })
	for _, r := range t.root.lst {
		if r.R >= ie.R {
			r.R--
		}
	}
	return ie.E, nil
}

// Remove removes the pos-th event with the ID.
func (t *EventTree) Remove(id EventID, pos int) error {
	_, err := t.Pop(id, pos)
	return err
}

// Subtree returns a mutable view of the events for which sel returned
// Include.
func (t *EventTree) Subtree(sel func(*Event) Select) *EventTree {
	var el []*IndexedEvent
	for _, ie := range t.lst {
		if sel(ie.E) == Include {
			el = append(el, ie)
		}
	}
	return NewEventTree(t, el)
}

// SubtreeIDs returns a view of the events whose ID is one of ids.
func (t *EventTree) SubtreeIDs(ids ...EventID) *EventTree {
	return t.Subtree(func(e *Event) Select {
		if idIn(e.id, ids) {
			return Include
		}
		return Skip
	})
}

// Subtrees returns views until sel and repeat are satisfied. When sel
// returns Cut the events collected so far are emitted as a tree and the
// current event starts the next one. Events collected after the last Cut are
// not emitted (same as PyFLP). Use repeat = -1 for no limit.
func (t *EventTree) Subtrees(sel func(*Event) Select, repeat int) []*EventTree {
	var out []*EventTree
	var el []*IndexedEvent
	for _, ie := range t.lst {
		if repeat == 0 {
			return out
		}
		switch sel(ie.E) {
		case Cut:
			out = append(out, NewEventTree(t, el))
			el = []*IndexedEvent{ie}
			repeat--
		case Include:
			el = append(el, ie)
		}
	}
	return out
}

// Divide returns subtrees holding the events whose ID is in ids, separated
// at every occurrence of separator. The last subtree is always returned.
func (t *EventTree) Divide(separator EventID, ids ...EventID) []*EventTree {
	var out []*EventTree
	var el []*IndexedEvent
	first := true
	for _, ie := range t.lst {
		if ie.E.id == separator {
			if !first {
				out = append(out, NewEventTree(t, el))
				el = nil
			} else {
				first = false
			}
		}
		if idIn(ie.E.id, ids) {
			el = append(el, ie)
		}
	}
	out = append(out, NewEventTree(t, el))
	return out
}

// Separate returns one tree per event with the ID.
func (t *EventTree) Separate(id EventID) []*EventTree {
	var out []*EventTree
	for _, ie := range t.getIE(id) {
		out = append(out, NewEventTree(t, []*IndexedEvent{ie}))
	}
	return out
}

// Group zips the events of each ID together (like itertools.zip_longest):
// the n-th tree holds the n-th event of every ID which has one.
func (t *EventTree) Group(ids ...EventID) []*EventTree {
	cols := make([][]*IndexedEvent, len(ids))
	longest := 0
	for i, id := range ids {
		cols[i] = t.getIE(id)
		if len(cols[i]) > longest {
			longest = len(cols[i])
		}
	}
	var out []*EventTree
	for n := 0; n < longest; n++ {
		var el []*IndexedEvent
		for _, col := range cols {
			if n < len(col) {
				el = append(el, col[n])
			}
		}
		out = append(out, NewEventTree(t, el))
	}
	return out
}
