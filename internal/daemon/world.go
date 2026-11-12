package daemon

// What an ending must reach beyond the agent's own process group.
//
// Claude starts a background shell in a group of its own (BUG-73), so retire's
// sweep of the agent's group never reached it, and a park sweeps nothing at all,
// after which the daemon exits holding no handle to anything: the park book
// holds no pid. So the daemon notes, while an agent runs, the group it leads and
// the groups its members lead, and sweeps them when the fleet ends.
//
// Nothing is signalled on a guess. A noted group is swept only while a process
// noted in it still runs under the same start time: a pid cannot be reused while
// its group has a member, so a surviving noted member proves the number is still
// that group's. A listing that cannot be read signals nothing - a leak, never a
// kill. Only the groups led by a *direct* child of the agent's group are noted:
// a daemon that a command in the agent started is that command's grandchild,
// and ending it would end a fleet this one does not own.

import (
	"slices"
	"strconv"
	"strings"
	"sync"
)

// procRow is one line of the machine's process listing.
type procRow struct {
	pid, ppid, pgid int
	start           string
}

// procRef is a process as noted: its start time tells it from the next process
// to be given its pid.
type procRef struct {
	pid   int
	start string
}

// procGroup is a process group and the processes noted in it.
type procGroup struct {
	pgid    int
	members []procRef
}

// world is what one agent started: the group it leads, and the groups led by
// its group's direct children.
type world struct {
	own     procGroup
	foreign []procGroup
}

func (w world) groups() []procGroup {
	if len(w.own.members) == 0 {
		return slices.Clone(w.foreign)
	}
	return append([]procGroup{w.own}, w.foreign...)
}

// parseProcRows reads `pid ppid pgid lstart` rows, skipping any it cannot read.
func parseProcRows(out []byte) []procRow {
	var rows []procRow
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		pgid, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil || pid <= 0 {
			continue
		}
		rows = append(rows, procRow{pid, ppid, pgid, strings.Join(f[3:], " ")})
	}
	return rows
}

func groupOf(rows []procRow, pgid int) procGroup {
	g := procGroup{pgid: pgid}
	for _, r := range rows {
		if r.pgid == pgid {
			g.members = append(g.members, procRef{r.pid, r.start})
		}
	}
	return g
}

// worldOf is the world of the agent leading group pgid, as the listing shows it.
// A group nothing leads is nobody's to name: the agent is gone and the number
// may have been given to another.
func worldOf(rows []procRow, pgid int) world {
	if pgid <= 1 || !slices.ContainsFunc(rows, func(r procRow) bool { return r.pid == pgid && r.pgid == pgid }) {
		return world{}
	}
	w := world{own: groupOf(rows, pgid)}
	for _, r := range rows {
		inOwn := slices.ContainsFunc(w.own.members, func(m procRef) bool { return m.pid == r.ppid })
		if inOwn && r.pgid == r.pid && r.pid != pgid {
			w.foreign = append(w.foreign, groupOf(rows, r.pid))
		}
	}
	return w
}

func indexOf(rows []procRow) map[int]procRow {
	idx := make(map[int]procRow, len(rows))
	for _, r := range rows {
		idx[r.pid] = r
	}
	return idx
}

// survivors are the members still running as the processes that were noted,
// still in the group.
func (g procGroup) survivors(idx map[int]procRow) []procRef {
	return slices.DeleteFunc(slices.Clone(g.members), func(m procRef) bool {
		r, ok := idx[m.pid]
		return !ok || r.start != m.start || r.pgid != g.pgid
	})
}

// merged is the union of two views of one group.
func (g procGroup) merged(o procGroup) procGroup {
	if g.pgid == 0 {
		g.pgid = o.pgid
	}
	for _, m := range o.members {
		if !slices.Contains(g.members, m) {
			g.members = append(g.members, m)
		}
	}
	return g
}

// refreshed is the world after another look: a group stays while one noted
// member runs, loses the members that ended, and gains what is new. A shell that
// has since exited leaves its child under a group nothing can reach from the
// agent, and this is how that group is still held.
func (w world) refreshed(rows []procRow, pgid int) world {
	idx := indexOf(rows)
	fresh := worldOf(rows, pgid)
	next := world{own: procGroup{pgid: w.own.pgid, members: w.own.survivors(idx)}.merged(fresh.own)}
	for _, g := range append(slices.Clone(w.foreign), fresh.foreign...) {
		g = procGroup{pgid: g.pgid, members: g.survivors(idx)}
		if len(g.members) == 0 {
			continue
		}
		if i := slices.IndexFunc(next.foreign, func(h procGroup) bool { return h.pgid == g.pgid }); i >= 0 {
			next.foreign[i] = next.foreign[i].merged(g)
		} else {
			next.foreign = append(next.foreign, g)
		}
	}
	return next
}

// sweepable names, once each and in order, the groups the listing shows still
// running a process that was noted in them.
func sweepable(rows []procRow, groups []procGroup) []int {
	idx := indexOf(rows)
	var out []int
	for _, g := range groups {
		if len(g.survivors(idx)) > 0 && !slices.Contains(out, g.pgid) {
			out = append(out, g.pgid)
		}
	}
	slices.Sort(out)
	return out
}

// worldLedger holds what parked agents started. A wake makes a new agent, so
// the groups the old one led are held here until the woken session ends or the
// daemon does.
type worldLedger struct {
	mu   sync.Mutex
	held []heldWorld
}

// heldWorld is one park's groups, under the ids the session was known by.
type heldWorld struct {
	ids    []string
	groups []procGroup
}

// hold keeps one entry per session however often it parks: a wake does not end
// what an earlier life left, and the woken agent does not know it.
func (l *worldLedger) hold(ids []string, groups []procGroup) {
	if len(groups) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	at := slices.IndexFunc(l.held, func(h heldWorld) bool {
		return slices.ContainsFunc(ids, func(id string) bool { return id != "" && slices.Contains(h.ids, id) })
	})
	if at < 0 {
		l.held = append(l.held, heldWorld{ids: ids, groups: groups})
		return
	}
	h := l.held[at]
	for _, id := range ids {
		if id != "" && !slices.Contains(h.ids, id) {
			h.ids = append(h.ids, id)
		}
	}
	for _, g := range groups {
		if i := slices.IndexFunc(h.groups, func(o procGroup) bool { return o.pgid == g.pgid }); i >= 0 {
			h.groups[i] = h.groups[i].merged(g)
		} else {
			h.groups = append(h.groups, g)
		}
	}
	l.held[at] = h
}

// take removes and returns what is held under any of these ids.
func (l *worldLedger) take(ids ...string) []procGroup {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []procGroup
	l.held = slices.DeleteFunc(l.held, func(h heldWorld) bool {
		if !slices.ContainsFunc(h.ids, func(id string) bool { return id != "" && slices.Contains(ids, id) }) {
			return false
		}
		out = append(out, h.groups...)
		return true
	})
	return out
}

func (l *worldLedger) takeAll() []procGroup {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []procGroup
	for _, h := range l.held {
		out = append(out, h.groups...)
	}
	l.held = nil
	return out
}
