package profiler

import (
	"encoding/json"
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
		{"simple call", "main()==>foo", strPtr("main()"), "foo"},
		{"namespaced", "App\\Service::run==>App\\Repo::find", strPtr("App\\Service::run"), "App\\Repo::find"},
		{"closure", "App\\{closure}==>strlen", strPtr("App\\{closure}"), "strlen"},
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

func TestPct(t *testing.T) {
	tests := []struct {
		name  string
		value int64
		peak  int64
		want  float64
	}{
		{"50%", 500, 1000, 50.0},
		{"100%", 1000, 1000, 100.0},
		{"zero value", 0, 1000, 0},
		{"zero peak", 500, 0, 0},
		{"both zero", 0, 0, 0},
		{"small fraction", 1, 1000, 0.1},
		{"negative peak", 500, -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pct(tt.value, tt.peak)
			if got != tt.want {
				t.Errorf("pct(%d, %d) = %f, want %f", tt.value, tt.peak, got, tt.want)
			}
		})
	}
}

func TestMax64(t *testing.T) {
	if max64(5, 3) != 5 {
		t.Error("max64(5, 3) should be 5")
	}
	if max64(3, 5) != 5 {
		t.Error("max64(3, 5) should be 5")
	}
	if max64(-1, 0) != 0 {
		t.Error("max64(-1, 0) should be 0")
	}
}

// --- Process tests ---

func TestProcess_BasicFields(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()": {WallTime: 1000, CPU: 500, Memory: 2000, PeakMem: 3000, Calls: 1},
		},
		AppName:  "my-app",
		Hostname: "server-1",
		Date:     1700000000,
		Tags:     map[string]any{"php": "8.3"},
	})

	if event.Event != "PROFILE_RECEIVED" {
		t.Errorf("Event = %q, want PROFILE_RECEIVED", event.Event)
	}
	if event.UUID == "" {
		t.Error("UUID should not be empty")
	}
	if event.AppName != "my-app" {
		t.Errorf("AppName = %q", event.AppName)
	}
	if event.Hostname != "server-1" {
		t.Errorf("Hostname = %q", event.Hostname)
	}
	if event.Date != 1700000000 {
		t.Errorf("Date = %d", event.Date)
	}
	if event.Tags["php"] != "8.3" {
		t.Errorf("Tags[php] = %v", event.Tags["php"])
	}
	if event.ReceivedAt.IsZero() {
		t.Error("ReceivedAt should be set")
	}
}

func TestProcess_Peaks(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":       {WallTime: 1000, CPU: 500, Memory: 2000, PeakMem: 3000, Calls: 1},
			"main()==>foo": {WallTime: 800, CPU: 400, Memory: 1500, PeakMem: 2000, Calls: 3},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	if event.Peaks.WallTime != 1000 {
		t.Errorf("Peaks.WallTime = %d, want 1000", event.Peaks.WallTime)
	}
	if event.Peaks.CPU != 500 {
		t.Errorf("Peaks.CPU = %d, want 500", event.Peaks.CPU)
	}
	if event.Peaks.Memory != 2000 {
		t.Errorf("Peaks.Memory = %d, want 2000", event.Peaks.Memory)
	}
	if event.Peaks.PeakMem != 3000 {
		t.Errorf("Peaks.PeakMem = %d, want 3000", event.Peaks.PeakMem)
	}
	if event.Peaks.Calls != 1 {
		t.Errorf("Peaks.Calls = %d, want 1", event.Peaks.Calls)
	}
}

func TestProcess_PeaksWithoutMain(t *testing.T) {
	// Some profilers use "value" instead of "main()" — peaks should be zero
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"value":       {WallTime: 1000, CPU: 500, Memory: 2000, PeakMem: 3000, Calls: 1},
			"value==>foo": {WallTime: 800, CPU: 400, Memory: 1500, PeakMem: 2000, Calls: 3},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	if event.Peaks.WallTime != 0 {
		t.Errorf("Peaks.WallTime = %d, want 0 (no main())", event.Peaks.WallTime)
	}
}

func TestProcess_DiffsCalculation(t *testing.T) {
	// main() calls A and B. A calls C.
	// main() exclusive = main() inclusive - (A inclusive + B inclusive)
	// A exclusive = A inclusive - C inclusive
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":      {WallTime: 1000, CPU: 500, Calls: 1},
			"main()==>A":  {WallTime: 700, CPU: 350, Calls: 2},
			"main()==>B":  {WallTime: 200, CPU: 100, Calls: 1},
			"A==>C":       {WallTime: 300, CPU: 150, Calls: 1},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	edges := edgesByCallee(event)

	// main() exclusive wt = 1000 - (700 + 200) = 100
	mainEdge := edges["main()"]
	if mainEdge.Diff.WallTime != 100 {
		t.Errorf("main() diff.wt = %d, want 100", mainEdge.Diff.WallTime)
	}
	if mainEdge.Diff.CPU != 50 {
		t.Errorf("main() diff.cpu = %d, want 50", mainEdge.Diff.CPU)
	}

	// A exclusive wt = 700 - 300 = 400
	aEdge := edges["A"]
	if aEdge.Diff.WallTime != 400 {
		t.Errorf("A diff.wt = %d, want 400", aEdge.Diff.WallTime)
	}

	// B has no children → exclusive = inclusive
	bEdge := edges["B"]
	if bEdge.Diff.WallTime != 200 {
		t.Errorf("B diff.wt = %d, want 200", bEdge.Diff.WallTime)
	}

	// C has no children → exclusive = inclusive
	cEdge := edges["C"]
	if cEdge.Diff.WallTime != 300 {
		t.Errorf("C diff.wt = %d, want 300", cEdge.Diff.WallTime)
	}
}

func TestProcess_DiffsClampToZero(t *testing.T) {
	// Edge case: children sum > parent (measurement imprecision)
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":     {WallTime: 100},
			"main()==>A": {WallTime: 60},
			"main()==>B": {WallTime: 60}, // 60+60 > 100
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	mainEdge := edgesByCallee(event)["main()"]
	if mainEdge.Diff.WallTime != 0 {
		t.Errorf("diff should clamp to 0 when children > parent, got %d", mainEdge.Diff.WallTime)
	}
}

func TestProcess_ParentResolution(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":     {WallTime: 1000, Calls: 1},
			"main()==>A": {WallTime: 800, Calls: 1},
			"main()==>B": {WallTime: 100, Calls: 1},
			"A==>C":      {WallTime: 300, Calls: 1},
			"B==>D":      {WallTime: 50, Calls: 1},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	edges := edgesByCallee(event)
	edgesByID := make(map[string]Edge)
	for _, e := range event.Edges {
		edgesByID[e.ID] = e
	}

	// main() is root — no parent
	if edges["main()"].Parent != nil {
		t.Error("main() should have no parent")
	}

	// A's parent should be main()'s edge
	aParent := edges["A"].Parent
	if aParent == nil {
		t.Fatal("A should have a parent")
	}
	if edgesByID[*aParent].Callee != "main()" {
		t.Errorf("A's parent callee = %q, want main()", edgesByID[*aParent].Callee)
	}

	// C's parent should be A's edge
	cParent := edges["C"].Parent
	if cParent == nil {
		t.Fatal("C should have a parent")
	}
	if edgesByID[*cParent].Callee != "A" {
		t.Errorf("C's parent callee = %q, want A", edgesByID[*cParent].Callee)
	}

	// D's parent should be B's edge
	dParent := edges["D"].Parent
	if dParent == nil {
		t.Fatal("D should have a parent")
	}
	if edgesByID[*dParent].Callee != "B" {
		t.Errorf("D's parent callee = %q, want B", edgesByID[*dParent].Callee)
	}
}

func TestProcess_DiamondGraph(t *testing.T) {
	// Diamond: main→A, main→B, A→C, B→C
	// Function C is called from both A and B.
	// XHProf has separate entries: "A==>C" and "B==>C"
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":     {WallTime: 1000, Calls: 1},
			"main()==>A": {WallTime: 600, Calls: 1},
			"main()==>B": {WallTime: 300, Calls: 1},
			"A==>C":      {WallTime: 200, Calls: 2},
			"B==>C":      {WallTime: 100, Calls: 1},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	// Should have 5 edges (each XHProf entry = one edge)
	if event.TotalEdges != 5 {
		t.Errorf("TotalEdges = %d, want 5", event.TotalEdges)
	}

	// Both "A==>C" and "B==>C" should be separate edges with different parents
	var cEdges []Edge
	for _, e := range event.Edges {
		if e.Callee == "C" {
			cEdges = append(cEdges, e)
		}
	}
	if len(cEdges) != 2 {
		t.Fatalf("expected 2 C edges (from A and B), got %d", len(cEdges))
	}

	// Verify they have different parents
	parents := map[string]bool{}
	for _, e := range cEdges {
		if e.Parent != nil {
			parents[*e.Parent] = true
		}
	}
	if len(parents) < 2 {
		t.Error("C edges from A and B should have different parents")
	}
}

func TestProcess_Percentages(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":      {WallTime: 1000, CPU: 500, Memory: 2000, PeakMem: 4000, Calls: 1},
			"main()==>foo": {WallTime: 500, CPU: 250, Memory: 1000, PeakMem: 2000, Calls: 3},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	fooEdge := edgesByCallee(event)["foo"]

	// foo.wt=500, peaks.wt=1000 → 50%
	if fooEdge.Percents.WallTime != 50.0 {
		t.Errorf("foo p_wt = %f, want 50.0", fooEdge.Percents.WallTime)
	}
	if fooEdge.Percents.CPU != 50.0 {
		t.Errorf("foo p_cpu = %f, want 50.0", fooEdge.Percents.CPU)
	}
	if fooEdge.Percents.Memory != 50.0 {
		t.Errorf("foo p_mu = %f, want 50.0", fooEdge.Percents.Memory)
	}
	if fooEdge.Percents.PeakMem != 50.0 {
		t.Errorf("foo p_pmu = %f, want 50.0", fooEdge.Percents.PeakMem)
	}

	// main() should be 100%
	mainEdge := edgesByCallee(event)["main()"]
	if mainEdge.Percents.WallTime != 100.0 {
		t.Errorf("main p_wt = %f, want 100.0", mainEdge.Percents.WallTime)
	}
}

func TestProcess_WithoutCPU(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":      {WallTime: 1000, Memory: 2000, PeakMem: 3000, Calls: 1},
			"main()==>foo": {WallTime: 500, Memory: 1000, PeakMem: 1500, Calls: 2},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	if event.Peaks.CPU != 0 {
		t.Errorf("Peaks.CPU = %d, want 0 (no XHPROF_FLAGS_CPU)", event.Peaks.CPU)
	}

	for _, edge := range event.Edges {
		if edge.Percents.CPU != 0 {
			t.Errorf("edge %q p_cpu = %f, want 0", edge.Callee, edge.Percents.CPU)
		}
	}
}

func TestProcess_BFSOrdering(t *testing.T) {
	// Verify parents appear before children in the ordered edges map
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":     {WallTime: 1000, Calls: 1},
			"main()==>A": {WallTime: 800, Calls: 1},
			"A==>B":      {WallTime: 400, Calls: 1},
			"B==>C":      {WallTime: 200, Calls: 1},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	// All 4 edges should be present
	if event.TotalEdges != 4 {
		t.Errorf("TotalEdges = %d, want 4", event.TotalEdges)
	}

	// Every non-root edge should have a parent that exists in the edges map
	for _, edge := range event.Edges {
		if edge.Parent != nil {
			if _, ok := event.Edges[*edge.Parent]; !ok {
				t.Errorf("edge %q references parent %q which doesn't exist", edge.Callee, *edge.Parent)
			}
		}
	}
}

func TestProcess_CallerField(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":     {WallTime: 1000, Calls: 1},
			"main()==>A": {WallTime: 800, Calls: 1},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	edges := edgesByCallee(event)

	// main() has no caller
	if edges["main()"].Caller != nil {
		t.Error("main() should have no caller")
	}

	// A's caller should be "main()"
	if edges["A"].Caller == nil || *edges["A"].Caller != "main()" {
		t.Errorf("A caller = %v, want main()", edges["A"].Caller)
	}
}

func TestProcess_EmptyProfile(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile:  map[string]RawMetrics{},
		AppName:  "test",
		Hostname: "h",
		Date:     1,
	})

	if event.TotalEdges != 0 {
		t.Errorf("TotalEdges = %d, want 0", event.TotalEdges)
	}
	if event.Peaks.WallTime != 0 {
		t.Errorf("Peaks.WallTime = %d, want 0", event.Peaks.WallTime)
	}
}

func TestProcess_SingleMainEntry(t *testing.T) {
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()": {WallTime: 500, CPU: 200, Memory: 1000, PeakMem: 2000, Calls: 1},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	if event.TotalEdges != 1 {
		t.Errorf("TotalEdges = %d, want 1", event.TotalEdges)
	}

	mainEdge := edgesByCallee(event)["main()"]
	// No children → diff == cost
	if mainEdge.Diff.WallTime != 500 {
		t.Errorf("main diff.wt = %d, want 500 (no children)", mainEdge.Diff.WallTime)
	}
}

func TestProcess_RealisticXHProfPayload(t *testing.T) {
	// Real payload from a Spiral Framework request (from PHP test fixture)
	jsonPayload := `{
		"profile": {
			"Nyholm\\Psr7\\Response::getHeaderLine==>Nyholm\\Psr7\\Response::getHeader": {"ct":2,"wt":4,"cpu":4,"mu":648,"pmu":0},
			"App\\Middleware\\LocaleSelector::process==>Nyholm\\Psr7\\Response::getHeaderLine": {"ct":1,"wt":6,"cpu":6,"mu":1216,"pmu":0},
			"App\\Middleware\\LocaleSelector::process==>Nyholm\\Psr7\\Response::getBody": {"ct":1,"wt":1,"cpu":1,"mu":568,"pmu":0},
			"Nyholm\\Psr7\\Stream::getUri==>Nyholm\\Psr7\\Stream::getMetadata": {"ct":1,"wt":4,"cpu":5,"mu":1728,"pmu":0},
			"Nyholm\\Psr7\\Stream::getSize==>Nyholm\\Psr7\\Stream::getUri": {"ct":1,"wt":8,"cpu":8,"mu":1240,"pmu":0},
			"App\\Middleware\\LocaleSelector::process==>Nyholm\\Psr7\\Stream::getSize": {"ct":1,"wt":15,"cpu":16,"mu":3624,"pmu":0},
			"Spiral\\Telemetry\\Span::setStatus==>Spiral\\Telemetry\\Span\\Status::__construct": {"ct":2,"wt":3,"cpu":3,"mu":584,"pmu":0},
			"App\\Middleware\\LocaleSelector::process==>Spiral\\Telemetry\\Span::setStatus": {"ct":1,"wt":5,"cpu":6,"mu":1312,"pmu":0},
			"main()==>App\\Middleware\\LocaleSelector::process": {"ct":1,"wt":211752,"cpu":82707,"mu":2600352,"pmu":1837832},
			"main()==>Nyholm\\Psr7\\Response::getStatusCode": {"ct":2,"wt":1,"cpu":1,"mu":552,"pmu":0},
			"main()==>Spiral\\Telemetry\\Span::setAttribute": {"ct":2,"wt":2,"cpu":3,"mu":928,"pmu":0},
			"main()==>Nyholm\\Psr7\\Response::getHeaderLine": {"ct":1,"wt":3,"cpu":4,"mu":552,"pmu":0},
			"main()==>Nyholm\\Psr7\\Response::getBody": {"ct":1,"wt":1,"cpu":1,"mu":536,"pmu":0},
			"main()==>Nyholm\\Psr7\\Stream::getSize": {"ct":1,"wt":0,"cpu":1,"mu":536,"pmu":0},
			"main()==>Spiral\\Telemetry\\Span::setStatus": {"ct":1,"wt":2,"cpu":3,"mu":632,"pmu":0},
			"main()==>Spiral\\Bootloader\\DebugBootloader::state": {"ct":1,"wt":96,"cpu":97,"mu":10200,"pmu":0},
			"main()==>Spiral\\Debug\\State::getTags": {"ct":1,"wt":0,"cpu":1,"mu":536,"pmu":0},
			"main()==>Nyholm\\Psr7\\ServerRequest::getAttribute": {"ct":1,"wt":2,"cpu":2,"mu":552,"pmu":0},
			"main()": {"ct":1,"wt":211999,"cpu":82952,"mu":2614696,"pmu":1837832}
		},
		"tags": {"php":"8.2.5","method":"GET"},
		"app_name": "My super app",
		"hostname": "Localhost",
		"date": 1714289301
	}`

	var incoming IncomingProfile
	if err := json.Unmarshal([]byte(jsonPayload), &incoming); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	event := Process(&incoming)

	// 19 entries in profile → 19 edges
	if event.TotalEdges != 19 {
		t.Errorf("TotalEdges = %d, want 19", event.TotalEdges)
	}

	// Peaks from main()
	if event.Peaks.WallTime != 211999 {
		t.Errorf("Peaks.WallTime = %d, want 211999", event.Peaks.WallTime)
	}
	if event.Peaks.CPU != 82952 {
		t.Errorf("Peaks.CPU = %d, want 82952", event.Peaks.CPU)
	}
	if event.Peaks.PeakMem != 1837832 {
		t.Errorf("Peaks.PeakMem = %d, want 1837832", event.Peaks.PeakMem)
	}

	// All edges should have valid parent references
	for _, edge := range event.Edges {
		if edge.Parent != nil {
			if _, ok := event.Edges[*edge.Parent]; !ok {
				t.Errorf("edge %q has parent %q which doesn't exist in edges", edge.Callee, *edge.Parent)
			}
		}
	}

	// main() edge should have no parent and 100% percentages
	mainEdge := edgesByCallee(event)["main()"]
	if mainEdge.Parent != nil {
		t.Error("main() should have no parent")
	}
	if mainEdge.Percents.WallTime != 100.0 {
		t.Errorf("main() p_wt = %f, want 100.0", mainEdge.Percents.WallTime)
	}

	// The biggest child: LocaleSelector::process has 211752 wt out of 211999
	localeEdge := edgesByCallee(event)["App\\Middleware\\LocaleSelector::process"]
	expectedPct := pct(211752, 211999)
	if localeEdge.Percents.WallTime != expectedPct {
		t.Errorf("LocaleSelector p_wt = %f, want %f", localeEdge.Percents.WallTime, expectedPct)
	}

	// Diffs: main() exclusive = 211999 - sum of all main()==>X
	// main()==>LocaleSelector: 211752, main()==>getStatusCode: 1, main()==>setAttribute: 2,
	// main()==>getHeaderLine: 3, main()==>getBody: 1, main()==>getSize: 0,
	// main()==>setStatus: 2, main()==>DebugBootloader::state: 96, main()==>getTags: 0,
	// main()==>getAttribute: 2
	// sum = 211752+1+2+3+1+0+2+96+0+2 = 211859
	// exclusive = 211999 - 211859 = 140
	if mainEdge.Diff.WallTime != 140 {
		t.Errorf("main() diff.wt = %d, want 140", mainEdge.Diff.WallTime)
	}

	// Verify JSON serialization works
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}
	if len(data) == 0 {
		t.Error("serialized event is empty")
	}
}

func TestProcess_CostPreservation(t *testing.T) {
	// Ensure raw metrics are preserved in edge.Cost
	event := Process(&IncomingProfile{
		Profile: map[string]RawMetrics{
			"main()":      {WallTime: 1000, CPU: 500, Memory: 2000, PeakMem: 3000, Calls: 1},
			"main()==>foo": {WallTime: 700, CPU: 350, Memory: 1500, PeakMem: 2500, Calls: 5},
		},
		AppName: "test", Hostname: "h", Date: 1,
	})

	foo := edgesByCallee(event)["foo"]
	if foo.Cost.WallTime != 700 {
		t.Errorf("foo cost.wt = %d, want 700", foo.Cost.WallTime)
	}
	if foo.Cost.CPU != 350 {
		t.Errorf("foo cost.cpu = %d, want 350", foo.Cost.CPU)
	}
	if foo.Cost.Memory != 1500 {
		t.Errorf("foo cost.mu = %d, want 1500", foo.Cost.Memory)
	}
	if foo.Cost.PeakMem != 2500 {
		t.Errorf("foo cost.pmu = %d, want 2500", foo.Cost.PeakMem)
	}
	if foo.Cost.Calls != 5 {
		t.Errorf("foo cost.ct = %d, want 5", foo.Cost.Calls)
	}
}

// --- helpers ---

func edgesByCallee(event *ProfileEvent) map[string]Edge {
	m := make(map[string]Edge)
	for _, e := range event.Edges {
		m[e.Callee] = e
	}
	return m
}

func strPtr(s string) *string {
	return &s
}
