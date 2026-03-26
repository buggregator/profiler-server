package profiler

import (
	"testing"
)

func TestSplitEdgeName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantParent *string
		wantCallee string
	}{
		{"root entry", "main()", nil, "main()"},
		{"caller==>callee", "main()==>foo", strPtr("main()"), "foo"},
		{"deep call", "App\\Service::run==>App\\Repo::find", strPtr("App\\Service::run"), "App\\Repo::find"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, callee := splitEdgeName(tt.input)
			if callee != tt.wantCallee {
				t.Errorf("callee = %q, want %q", callee, tt.wantCallee)
			}
			if tt.wantParent == nil && parent != nil {
				t.Errorf("parent = %q, want nil", *parent)
			}
			if tt.wantParent != nil && (parent == nil || *parent != *tt.wantParent) {
				t.Errorf("parent = %v, want %q", parent, *tt.wantParent)
			}
		})
	}
}

func TestProcess(t *testing.T) {
	incoming := &IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()": {WallTime: 1000, CPU: 500, Memory: 2000, PeakMem: 3000, Calls: 1},
			"main()==>A": {WallTime: 800, CPU: 400, Memory: 1500, PeakMem: 2000, Calls: 3},
			"main()==>B": {WallTime: 100, CPU: 50, Memory: 200, PeakMem: 500, Calls: 1},
			"A==>C":      {WallTime: 300, CPU: 150, Memory: 100, PeakMem: 200, Calls: 2},
		},
		AppName:  "test-app",
		Hostname: "localhost",
		Date:     1700000000,
	}

	event := Process(incoming)

	// Basic fields
	if event.AppName != "test-app" {
		t.Errorf("AppName = %q, want %q", event.AppName, "test-app")
	}
	if event.Event != "PROFILE_RECEIVED" {
		t.Errorf("Event = %q, want %q", event.Event, "PROFILE_RECEIVED")
	}
	if event.UUID == "" {
		t.Error("UUID should not be empty")
	}

	// Peaks from main()
	if event.Peaks.WallTime != 1000 {
		t.Errorf("Peaks.WallTime = %d, want %d", event.Peaks.WallTime, 1000)
	}
	if event.Peaks.CPU != 500 {
		t.Errorf("Peaks.CPU = %d, want %d", event.Peaks.CPU, 500)
	}

	// Edge count
	if event.TotalEdges != 4 {
		t.Errorf("TotalEdges = %d, want %d", event.TotalEdges, 4)
	}

	// Verify edges exist and have correct callees
	callees := make(map[string]bool)
	for _, edge := range event.Edges {
		callees[edge.Callee] = true
	}
	for _, expected := range []string{"main()", "A", "B", "C"} {
		if !callees[expected] {
			t.Errorf("missing edge for callee %q", expected)
		}
	}

	// Verify parent resolution: A's parent should be main()'s edge
	var mainEdgeID, aEdge *Edge
	for _, edge := range event.Edges {
		e := edge
		if edge.Callee == "main()" {
			mainEdgeID = &e
		}
		if edge.Callee == "A" {
			aEdge = &e
		}
	}
	if mainEdgeID == nil {
		t.Fatal("main() edge not found")
	}
	if aEdge == nil {
		t.Fatal("A edge not found")
	}
	if aEdge.Parent == nil || *aEdge.Parent != mainEdgeID.ID {
		t.Errorf("A's parent = %v, want %q", aEdge.Parent, mainEdgeID.ID)
	}
}

func TestProcessWithoutCPU(t *testing.T) {
	// Simulate XHProf without XHPROF_FLAGS_CPU — cpu will be 0
	incoming := &IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":      {WallTime: 1000, Memory: 2000, PeakMem: 3000, Calls: 1},
			"main()==>foo": {WallTime: 500, Memory: 1000, PeakMem: 1500, Calls: 2},
		},
		AppName:  "test",
		Hostname: "localhost",
		Date:     1700000000,
	}

	event := Process(incoming)

	if event.TotalEdges != 2 {
		t.Errorf("TotalEdges = %d, want 2", event.TotalEdges)
	}
	if event.Peaks.CPU != 0 {
		t.Errorf("Peaks.CPU = %d, want 0", event.Peaks.CPU)
	}
}

func TestPct(t *testing.T) {
	if got := pct(500, 1000); got != 50.0 {
		t.Errorf("pct(500, 1000) = %f, want 50.0", got)
	}
	if got := pct(0, 1000); got != 0 {
		t.Errorf("pct(0, 1000) = %f, want 0", got)
	}
	if got := pct(500, 0); got != 0 {
		t.Errorf("pct(500, 0) = %f, want 0", got)
	}
}

func strPtr(s string) *string {
	return &s
}
