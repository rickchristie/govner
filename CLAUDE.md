# Govner
Govner is a collection of Go development tools.

# Critical Behavior
- **ALWAYS write all outputs of test, bash commands to /tmp file** when running test, scripts that you need the result for.
  You can then run head/tail or search on the result file. This avoids re-running the script again whenever there are issues.
- **ALWAYS fully finish your tasks** when executing anything. Never stop to ask "would you like to continue?" or anything similar.
  You are given tasks, fully complete them, don't waste our time.

# TUI Standards
- 