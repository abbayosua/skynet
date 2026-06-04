package scheduler

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

type Job struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Prompt      string    `json:"prompt"`
	Description string    `json:"description,omitempty"`
	Interval    string    `json:"interval"`
	TimeoutSec  int       `json:"timeout_seconds,omitempty"`
	WorkDir     string    `json:"work_dir"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastRunAt   time.Time `json:"last_run_at,omitempty"`
	LastResult  string    `json:"last_result,omitempty"`
	RunCount    int       `json:"run_count,omitempty"`
}

type RunRecord struct {
	JobID     string    `json:"job_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Success   bool      `json:"success"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
}

func ParseInterval(s string) (time.Duration, error) {
	switch s {
	case "minutely", "every minute", "1m":
		return time.Minute, nil
	case "hourly", "every hour", "1h":
		return time.Hour, nil
	case "daily", "every day", "24h":
		return 24 * time.Hour, nil
	case "weekly", "every week":
		return 7 * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("expected duration like \"30m\", \"1h\", \"6h\", \"24h\" or named: hourly, daily, weekly")
	}
	if d < time.Minute {
		return 0, fmt.Errorf("minimum interval is 1 minute")
	}
	return d, nil
}

func (j *Job) Validate() error {
	if j.Name == "" {
		return fmt.Errorf("name is required")
	}
	if j.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if j.Interval == "" {
		return fmt.Errorf("interval is required")
	}
	if _, err := ParseInterval(j.Interval); err != nil {
		return fmt.Errorf("invalid interval %q: %w", j.Interval, err)
	}
	return nil
}

func jobID(name string) string {
	var b []byte
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b = append(b, byte(r))
		} else if r == ' ' {
			b = append(b, '-')
		} else if len(b) > 0 && b[len(b)-1] == '-' {
			continue
		}
	}
	id := string(b)
	if id == "" {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return id
}
