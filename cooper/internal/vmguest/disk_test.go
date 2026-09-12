package vmguest

import (
	"errors"
	"strings"
	"testing"
)

func TestDockerWrapperForcesBothBuildFormsThroughFixedProxy(t *testing.T) {
	t.Parallel()
	script := dockerWrapperScript("http://172.30.0.1:3128", "172.30.0.1")
	for _, want := range []string{
		`if [ "${1-}" = build ]`,
		`if [ "${1-}" = image ] && [ "${2-}" = build ]`,
		"--build-arg HTTP_PROXY=http://172.30.0.1:3128",
		"--build-arg HTTPS_PROXY=http://172.30.0.1:3128",
		"--build-arg NO_PROXY=localhost,127.0.0.1,172.30.0.1",
		"--build-arg http_proxy=http://172.30.0.1:3128",
		"--build-arg https_proxy=http://172.30.0.1:3128",
		"--build-arg no_proxy=localhost,127.0.0.1,172.30.0.1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Docker wrapper does not contain %q:\n%s", want, script)
		}
	}
}

func TestValidateGrowpartResult(t *testing.T) {
	t.Parallel()
	commandErr := errors.New("exit status 1")
	for _, test := range []struct {
		name    string
		output  string
		err     error
		wantErr bool
	}{
		{name: "grown"},
		{name: "already full", output: "NOCHANGE: partition 1 is size 123", err: commandErr},
		{name: "failed", output: "FAILED: cannot read partition table", err: commandErr, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateGrowpartResult([]byte(test.output), test.err)
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "grow VM root partition")) {
				t.Fatalf("error = %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLastOutputLine(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		output string
		want   string
	}{
		{name: "empty", want: "no Docker error output"},
		{name: "one line", output: "no space left on device\n", want: "no space left on device"},
		{name: "multiple lines", output: "loading layer\nwrite cache: no space left on device\n", want: "write cache: no space left on device"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := lastOutputLine([]byte(test.output)); got != test.want {
				t.Fatalf("lastOutputLine() = %q, want %q", got, test.want)
			}
		})
	}
}
