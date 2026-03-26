package profiler

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Process takes raw XHProf data and produces a fully processed ProfileEvent
func Process(incoming *IncomingProfile) *ProfileEvent {
	profile := incoming.Profile

	// Step 1: Normalize — fill missing metrics with 0 (cpu absent without XHPROF_FLAGS_CPU)
	for name, m := range profile {
		_ = m // already zero-valued in Go for missing JSON fields
		profile[name] = m
	}

	// Step 2: Extract peaks from main() entry
	peaks := Metrics{}
	if main, ok := profile["main()"]; ok {
		peaks = Metrics{
			WallTime: main.WallTime,
			CPU:      main.CPU,
			Memory:   main.Memory,
			PeakMem:  main.PeakMem,
			Calls:    main.Calls,
		}
	}

	// Step 3: Calculate diffs (exclusive metrics per function)
	// Aggregate children's inclusive metrics per parent function
	childrenSum := make(map[string]Metrics)
	for name, values := range profile {
		parent, _ := splitEdgeName(name)
		if parent != nil {
			sum := childrenSum[*parent]
			sum.CPU += values.CPU
			sum.WallTime += values.WallTime
			sum.Memory += values.Memory
			sum.PeakMem += values.PeakMem
			sum.Calls += values.Calls
			childrenSum[*parent] = sum
		}
	}

	diffs := make(map[string]Diffs)
	for name, values := range profile {
		_, callee := splitEdgeName(name)
		children := childrenSum[callee]
		diffs[name] = Diffs{
			WallTime: max64(0, values.WallTime-children.WallTime),
			CPU:      max64(0, values.CPU-children.CPU),
			Memory:   max64(0, values.Memory-children.Memory),
			PeakMem:  max64(0, values.PeakMem-children.PeakMem),
			Calls:    max64(0, values.Calls-children.Calls),
		}
	}

	// Step 4: Build edges with two-pass parent resolution + BFS ordering
	type tempEdge struct {
		id     string
		caller *string
		callee string
		cost   Metrics
		diff   Diffs
		parent *string
	}

	edgesTemp := make(map[string]*tempEdge)
	calleeToEdgeID := make(map[string]string)

	id := 1
	for name, values := range profile {
		parent, callee := splitEdgeName(name)

		edgeID := edgeIDStr(id)

		edgesTemp[edgeID] = &tempEdge{
			id:     edgeID,
			caller: parent,
			callee: callee,
			cost: Metrics{
				WallTime: values.WallTime,
				CPU:      values.CPU,
				Memory:   values.Memory,
				PeakMem:  values.PeakMem,
				Calls:    values.Calls,
			},
			diff: diffs[name],
		}

		if _, exists := calleeToEdgeID[callee]; !exists {
			calleeToEdgeID[callee] = edgeID
		}

		id++
	}

	// Second pass: resolve parent references
	for _, edge := range edgesTemp {
		if edge.caller != nil {
			if parentEdgeID, ok := calleeToEdgeID[*edge.caller]; ok {
				p := parentEdgeID
				edge.parent = &p
			}
		}
	}

	// BFS ordering: parents before children
	childrenMap := make(map[string][]string)
	var roots []string
	for edgeID, edge := range edgesTemp {
		if edge.parent == nil {
			roots = append(roots, edgeID)
		} else {
			childrenMap[*edge.parent] = append(childrenMap[*edge.parent], edgeID)
		}
	}

	orderedEdges := make(map[string]Edge)
	queue := make([]string, len(roots))
	copy(queue, roots)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		te := edgesTemp[current]

		// Calculate percentages
		pcts := Percentages{
			WallTime: pct(te.cost.WallTime, peaks.WallTime),
			CPU:      pct(te.cost.CPU, peaks.CPU),
			Memory:   pct(te.cost.Memory, peaks.Memory),
			PeakMem:  pct(te.cost.PeakMem, peaks.PeakMem),
			Calls:    pct(te.cost.Calls, peaks.Calls),
		}

		orderedEdges[current] = Edge{
			ID:       te.id,
			Caller:   te.caller,
			Callee:   te.callee,
			Cost:     te.cost,
			Diff:     te.diff,
			Percents: pcts,
			Parent:   te.parent,
		}

		for _, childID := range childrenMap[current] {
			queue = append(queue, childID)
		}
	}

	// Add orphaned edges
	for edgeID, te := range edgesTemp {
		if _, exists := orderedEdges[edgeID]; !exists {
			orderedEdges[edgeID] = Edge{
				ID:       te.id,
				Caller:   te.caller,
				Callee:   te.callee,
				Cost:     te.cost,
				Diff:     te.diff,
				Percents: Percentages{},
				Parent:   te.parent,
			}
		}
	}

	return &ProfileEvent{
		Event:      "PROFILE_RECEIVED",
		UUID:       uuid.NewString(),
		AppName:    incoming.AppName,
		Hostname:   incoming.Hostname,
		Date:       incoming.Date,
		Tags:       incoming.Tags,
		Peaks:      peaks,
		Edges:      orderedEdges,
		TotalEdges: len(orderedEdges),
		ReceivedAt: time.Now(),
	}
}

// splitEdgeName splits "caller==>callee" into (caller, callee).
// For root entries like "main()", returns (nil, "main()").
func splitEdgeName(name string) (*string, string) {
	parts := strings.SplitN(name, "==>", 2)
	if len(parts) == 2 {
		return &parts[0], parts[1]
	}
	return nil, parts[0]
}

func edgeIDStr(id int) string {
	return "e" + strconv.Itoa(id)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func pct(value, peak int64) float64 {
	if peak <= 0 || value <= 0 {
		return 0
	}
	return math.Round(float64(value)/float64(peak)*100*1000) / 1000
}
