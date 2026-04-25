package logging

import "fmt"

// CmdLogger wraps a Logger to provide structured step-based logging for
// CLI commands like "cooper configure" and "cooper up". Each entry is a
// single line with machine-parseable key=value pairs.
type CmdLogger struct {
	l       *Logger
	command string
}

// NewCmdLogger creates a CmdLogger that writes to {command}.log in dir.
// Uses the same rotation settings as the ACL/bridge loggers (10 MB, 10 files).
func NewCmdLogger(dir, command string) *CmdLogger {
	return &CmdLogger{
		l:       NewLogger(dir, command, 10*1024*1024, 10),
		command: command,
	}
}

// LogStart writes a log entry indicating the command has started.
func (c *CmdLogger) LogStart() {
	c.l.Log(fmt.Sprintf("command=%s status=started", c.command))
}

// LogStep writes a log entry for a completed or failed step. If err is nil,
// the step is logged as ok; otherwise it is logged as error with the message.
func (c *CmdLogger) LogStep(step int, name string, err error) {
	if err != nil {
		c.l.Log(fmt.Sprintf("command=%s step=%d name=%q status=error err=%v", c.command, step, name, err))
	} else {
		c.l.Log(fmt.Sprintf("command=%s step=%d name=%q status=ok", c.command, step, name))
	}
}

// LogEvent writes a non-step lifecycle event. This is used for long-running
// commands such as "cooper up", where startup can complete successfully but
// the command may later fail because the TUI receives an external signal.
func (c *CmdLogger) LogEvent(name string, err error) {
	if err != nil {
		c.l.Log(fmt.Sprintf("command=%s event=%q status=error err=%v", c.command, name, err))
	} else {
		c.l.Log(fmt.Sprintf("command=%s event=%q status=ok", c.command, name))
	}
}

// LogDone writes a final log entry. If err is nil, the command completed
// successfully; otherwise the error is recorded.
func (c *CmdLogger) LogDone(err error) {
	if err != nil {
		c.l.Log(fmt.Sprintf("command=%s status=failed err=%v", c.command, err))
	} else {
		c.l.Log(fmt.Sprintf("command=%s status=done", c.command))
	}
}

// Close closes the underlying log file.
func (c *CmdLogger) Close() error {
	return c.l.Close()
}
