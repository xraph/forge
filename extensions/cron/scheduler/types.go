package scheduler

import cronext "github.com/xraph/forge/extensions/cron"

// Helper functions for type assertions

// ToJob converts interface{} to *cronext.Job.
func ToJob(v any) *cronext.Job {
	if v == nil {
		return nil
	}

	if job, ok := v.(*cronext.Job); ok {
		return job
	}

	return nil
}

// ToJobs converts []interface{} to []*cronext.Job.
func ToJobs(v []any) []*cronext.Job {
	if v == nil {
		return nil
	}

	jobs := make([]*cronext.Job, 0, len(v))
	for _, item := range v {
		if job := ToJob(item); job != nil {
			jobs = append(jobs, job)
		}
	}

	return jobs
}

// FromJobs converts []*cronext.Job to []interface{}.
func FromJobs(jobs []*cronext.Job) []any {
	if jobs == nil {
		return nil
	}

	result := make([]any, len(jobs))
	for i, job := range jobs {
		result[i] = job
	}

	return result
}
