package profiler

import "time"

// Metrics holds XHProf metric values for a single caller→callee edge
type Metrics struct {
	WallTime int64 `json:"wt"`
	CPU      int64 `json:"cpu"`
	Memory   int64 `json:"mu"`
	PeakMem  int64 `json:"pmu"`
	Calls    int64 `json:"ct"`
}

// Percentages of peak values
type Percentages struct {
	WallTime float64 `json:"p_wt"`
	CPU      float64 `json:"p_cpu"`
	Memory   float64 `json:"p_mu"`
	PeakMem  float64 `json:"p_pmu"`
	Calls    float64 `json:"p_ct"`
}

// Diffs (exclusive metrics: inclusive minus children)
type Diffs struct {
	WallTime int64 `json:"d_wt"`
	CPU      int64 `json:"d_cpu"`
	Memory   int64 `json:"d_mu"`
	PeakMem  int64 `json:"d_pmu"`
	Calls    int64 `json:"d_ct"`
}

// Edge represents a processed caller→callee relationship
type Edge struct {
	ID       string      `json:"id"`
	Caller   *string     `json:"caller"`
	Callee   string      `json:"callee"`
	Cost     Metrics     `json:"cost"`
	Diff     Diffs       `json:"diff"`
	Percents Percentages `json:"percents"`
	Parent   *string     `json:"parent"`
}

// ProfileEvent is the fully processed event pushed to Jobs
type ProfileEvent struct {
	Event     string            `json:"event"`
	UUID      string            `json:"uuid"`
	AppName   string            `json:"app_name"`
	Hostname  string            `json:"hostname"`
	Date      int64             `json:"date"`
	Tags      map[string]any    `json:"tags,omitempty"`
	Peaks     Metrics           `json:"peaks"`
	Edges     map[string]Edge   `json:"edges"`
	TotalEdges int              `json:"total_edges"`
	ReceivedAt time.Time        `json:"received_at"`
}

// IncomingProfile is the raw XHProf payload from the PHP client
type IncomingProfile struct {
	Profile  map[string]RawMetrics `json:"profile"`
	AppName  string                `json:"app_name"`
	Hostname string                `json:"hostname"`
	Date     int64                 `json:"date"`
	Tags     map[string]any        `json:"tags,omitempty"`
}

// RawMetrics from XHProf — cpu may be absent when XHPROF_FLAGS_CPU is not set
type RawMetrics struct {
	WallTime int64 `json:"wt"`
	CPU      int64 `json:"cpu"`
	Memory   int64 `json:"mu"`
	PeakMem  int64 `json:"pmu"`
	Calls    int64 `json:"ct"`
}
