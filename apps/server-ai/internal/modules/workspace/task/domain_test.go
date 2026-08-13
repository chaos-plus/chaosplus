package task

import "testing"

func TestValidateAllowsPlanningTaskWithoutExecutionWorkspace(t *testing.T) {
	value := &Task{Title: "Plan release", Status: StatusOpen}
	if err := validate(value); err != nil {
		t.Fatalf("validate planning task: %v", err)
	}
}
