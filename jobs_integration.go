package profiler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/roadrunner-server/api/v4/plugins/v4/jobs"
)

// Jobs is the interface provided by Jobs plugin for pushing jobs
type Jobs interface {
	Push(ctx context.Context, msg jobs.Message) error
}

// Job represents a job message to be pushed to Jobs plugin
// Implements jobs.Message interface
type Job struct {
	Job     string              `json:"job"`
	Ident   string              `json:"id"`
	Pld     []byte              `json:"payload"`
	Hdr     map[string][]string `json:"headers"`
	Options *JobOptions         `json:"options,omitempty"`
}

// JobOptions carry information about how to handle given job
type JobOptions struct {
	Priority int64  `json:"priority"`
	Pipeline string `json:"pipeline,omitempty"`
	Delay    int64  `json:"delay,omitempty"`
	AutoAck  bool   `json:"auto_ack"`
}

// Implement jobs.Message interface

func (j *Job) ID() string      { return j.Ident }
func (j *Job) GroupID() string  { return j.Options.Pipeline }
func (j *Job) Name() string     { return j.Job }
func (j *Job) Payload() []byte  { return j.Pld }
func (j *Job) Headers() map[string][]string { return j.Hdr }

func (j *Job) Priority() int64 {
	if j.Options == nil {
		return 10
	}
	return j.Options.Priority
}

func (j *Job) Delay() int64 {
	if j.Options == nil {
		return 0
	}
	return j.Options.Delay
}

func (j *Job) AutoAck() bool {
	if j.Options == nil {
		return false
	}
	return j.Options.AutoAck
}

// Kafka-specific methods (required by jobs.Message interface)
func (j *Job) Offset() int64        { return 0 }
func (j *Job) Partition() int32     { return 0 }
func (j *Job) Topic() string        { return "" }
func (j *Job) Metadata() string     { return "" }
func (j *Job) UpdatePriority(p int64) {
	if j.Options == nil {
		j.Options = &JobOptions{}
	}
	j.Options.Priority = p
}

// profileToJobMessage converts ProfileEvent to a jobs.Message
func profileToJobMessage(event *ProfileEvent, cfg *JobsConfig) (jobs.Message, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile event: %w", err)
	}

	return &Job{
		Job:   "profiler.profile",
		Ident: uuid.NewString(),
		Pld:   payload,
		Hdr: map[string][]string{
			"uuid":          {event.UUID},
			"app_name":      {event.AppName},
			"payload_class": {"profiler:handler"},
		},
		Options: &JobOptions{
			Pipeline: cfg.Pipeline,
			Priority: cfg.Priority,
			Delay:    cfg.Delay,
			AutoAck:  cfg.AutoAck,
		},
	}, nil
}
