package opencode

const (
	pluginSource           = "plugins/opencode/mimir.ts"
	setupSkillSourcePrefix = "skills/mimir-setup/"
	useSkillSourcePrefix   = "skills/mimir-use/"
)

func ArtifactSourcePrefixes() []string {
	return []string{pluginSource, setupSkillSourcePrefix, useSkillSourcePrefix}
}
