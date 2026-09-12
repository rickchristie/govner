package docker

import "testing"

func TestValidateRuntimeNamespace(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"cooper", "cooper-vm-e2e", "a.b_c", "name-"} {
		if err := ValidateRuntimeNamespace(valid); err != nil {
			t.Errorf("ValidateRuntimeNamespace(%q) error = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "-cooper", "Cooper", "cooper/runtime", "cooper vm", "cooper@vm"} {
		if err := ValidateRuntimeNamespace(invalid); err == nil {
			t.Errorf("ValidateRuntimeNamespace(%q) succeeded", invalid)
		}
	}
}
