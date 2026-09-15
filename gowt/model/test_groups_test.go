package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlashGroupsKeepChildResultsWhenPackageEnds(t *testing.T) {
	for _, result := range []string{"pass", "skip", "fail"} {
		t.Run(result, func(t *testing.T) {
			tree := NewTestTree()
			processEvents(t, tree,
				TestEvent{Action: "run", Package: "pkg", Test: "TestCases/read/write"},
				TestEvent{Action: "pass", Package: "pkg", Test: "TestCases/read/write"},
				TestEvent{Action: "run", Package: "pkg", Test: "TestCases/empty/value"},
				TestEvent{Action: "skip", Package: "pkg", Test: "TestCases/empty/value"},
				TestEvent{Action: result, Package: "pkg"},
			)

			passed := tree.GetNode("pkg/TestCases/read")
			skipped := tree.GetNode("pkg/TestCases/empty")
			assert.Equal(t, StatusPassed, passed.Status)
			assert.Equal(t, StatusSkipped, skipped.Status)
			assert.Equal(t, 1, passed.PassedCount)
			assert.Equal(t, 1, skipped.SkippedCount)
			assert.Equal(t, 2, tree.TotalCount)
			assert.Equal(t, 1, tree.PassedCount)
			assert.Equal(t, 1, tree.SkippedCount)
			assert.Zero(t, tree.RunningCount)
			if result == "fail" {
				assert.Equal(t, 1, tree.FailedCount, "the package failure is separate from the passing tests")
				assert.Equal(t, FailureKindPackage, tree.GetNode("pkg").FailureKind)
				return
			}
			assert.Zero(t, tree.FailedCount)
		})
	}
}

func TestGroupCountsItsFirstDirectEventOnce(t *testing.T) {
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "run", Package: "pkg", Test: "TestParent/child"},
		TestEvent{Action: "pass", Package: "pkg", Test: "TestParent/child"},
	)
	assert.Equal(t, 1, tree.TotalCount)

	// A later direct event proves that the parent is also a test. Output-only
	// workloads need to count too, even when Go omits a run or terminal event.
	event := TestEvent{Action: "output", Package: "pkg", Test: "TestParent", Output: "parent log\n"}
	assert.True(t, tree.ProcessEvent(event), "the new test count must refresh the tree")
	assert.False(t, tree.ProcessEvent(event))
	assert.Equal(t, 2, tree.TotalCount)
	tree.ProcessEvent(TestEvent{Action: "fail", Package: "pkg"})
	assert.Equal(t, 1, tree.PassedCount)
	assert.Equal(t, 1, tree.FailedCount)
	assert.Equal(t, StatusFailed, tree.GetNode("pkg/TestParent").Status)
	assert.Equal(t, StatusPassed, tree.GetNode("pkg/TestParent/child").Status)
}

func TestPausedParentStillFailsWhenPackageStops(t *testing.T) {
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "run", Package: "pkg", Test: "TestParent"},
		TestEvent{Action: "pause", Package: "pkg", Test: "TestParent"},
		TestEvent{Action: "run", Package: "pkg", Test: "TestParent/read/write"},
		TestEvent{Action: "pass", Package: "pkg", Test: "TestParent/read/write"},
		TestEvent{Action: "fail", Package: "pkg"},
	)

	parent := tree.GetNode("pkg/TestParent")
	require.NotNil(t, parent)
	assert.Equal(t, StatusFailed, parent.Status)
	assert.Equal(t, StatusPassed, tree.GetNode("pkg/TestParent/read").Status)
	assert.Equal(t, []int{1, 1, 0, 0, 0}, statsSlice(tree))
	assert.Equal(t, 2, tree.TotalCount)
}
