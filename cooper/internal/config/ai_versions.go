package config

import "fmt"

// MirrorAITools removes old AI version choices. Keep the built version so
// users can see that an existing image needs a rebuild after this change.
func (c *Config) MirrorAITools() []string {
	var notices []string
	for i := range c.AITools {
		tool := &c.AITools[i]
		if tool.Mode == ModeLatest || tool.Mode == ModePin {
			notices = append(notices, fmt.Sprintf("%s now uses the host version; the %s setting was removed.", tool.Name, tool.Mode))
		}
		tool.Mode = ModeMirror
		tool.PinnedVersion = ""
	}
	return notices
}
