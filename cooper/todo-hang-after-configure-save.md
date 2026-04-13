Currently after saving configuration, the cooper configure TUI hangs for a bit until it returns.
If there are actions after saving/save build that can take a while, there should be a loading indicator for it.
Since we already have loading indicator screen for cooper up start and shutdown, we can reuse that for this case.
Good UI provides proper feedback to the user.