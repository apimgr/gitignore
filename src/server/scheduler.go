package server

import (
	"net/http"

	"github.com/apimgr/gitignore/src/db"
)

// handleAPISchedulerStatus returns the persisted state of every scheduled task
// (AI.md PART 18 "Scheduler Status"). The database is the source of truth, so
// this read-only endpoint queries it directly rather than holding a reference
// to the running scheduler.
func (s *Server) handleAPISchedulerStatus(w http.ResponseWriter, r *http.Request) {
	states, err := db.LoadSchedulerStates()
	if err != nil {
		sendAPIResponseError(w, "scheduler_error", "failed to load scheduler state")
		return
	}

	tasks := make([]map[string]interface{}, 0, len(states))
	for _, st := range states {
		task := map[string]interface{}{
			"task_id":     st.TaskID,
			"task_name":   st.TaskName,
			"schedule":    st.Schedule,
			"last_status": st.LastStatus,
			"last_error":  st.LastError,
			"run_count":   st.RunCount,
			"fail_count":  st.FailCount,
			"enabled":     st.Enabled,
		}
		if !st.LastRun.IsZero() {
			task["last_run"] = st.LastRun.UTC()
		}
		if !st.NextRun.IsZero() {
			task["next_run"] = st.NextRun.UTC()
		}
		tasks = append(tasks, task)
	}

	sendAPIResponseOK(w, map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
}
