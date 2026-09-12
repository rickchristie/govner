package workload

import (
	"encoding/csv"
	"strings"
)

// DockerBindMount uses Docker's CSV grammar so a comma or quote in a host
// path cannot change the mount options. Both execution back ends use it.
func DockerBindMount(source, target string, readOnly bool) string {
	fields := []string{"type=bind", "src=" + source, "dst=" + target}
	if readOnly {
		fields = append(fields, "readonly")
	}
	var output strings.Builder
	writer := csv.NewWriter(&output)
	_ = writer.Write(fields)
	writer.Flush()
	return strings.TrimSuffix(output.String(), "\n")
}
