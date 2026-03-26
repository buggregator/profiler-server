# RoadRunner Profiler Plugin

A RoadRunner plugin that receives XHProf profiling data via HTTP, processes it entirely in Go (peaks, diffs, edges tree, percentages), and forwards the fully processed result to the Jobs pipeline for storage.

## Configuration

```yaml
profiler:
  addr: "127.0.0.1:9914"
  max_request_size: 52428800  # 50MB
  read_timeout: 60s
  write_timeout: 30s
  jobs:
    pipeline: "profiler"
    priority: 10
    auto_ack: true

jobs:
  pipelines:
    profiler:
      driver: memory
      config:
        priority: 10
  consume:
    - profiler
```

## How it works

1. PHP XHProf client sends HTTP POST with JSON payload:
   ```json
   {
     "profile": { "main()": {"wt": 1000, "cpu": 500, ...}, "main()==>foo": {...} },
     "app_name": "my-app",
     "hostname": "server-1",
     "date": 1700000000,
     "tags": {}
   }
   ```

2. The plugin processes the profile in Go:
   - Extracts peaks from `main()` entry
   - Calculates exclusive metrics (diffs) per function
   - Builds edge tree with two-pass parent resolution
   - Computes percentages relative to peaks
   - Orders edges via BFS (parents before children)

3. Pushes a `profiler.profile` job with the fully processed event to the Jobs pipeline.

## Job format

Job name: `profiler.profile`

Payload contains the complete `ProfileEvent` JSON with pre-computed edges, peaks, diffs, and percentages — ready for direct database storage without further computation on the PHP side.
