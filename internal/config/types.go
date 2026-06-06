package config

type UserConfig struct {
	Version   int             `yaml:"version"`
	Workspace WorkspaceConfig `yaml:"workspace"`
	OpenWrt   OpenWrtConfig   `yaml:"openwrt"`
	Docker    DockerConfig    `yaml:"docker"`
	Builds    []BuildConfig   `yaml:"builds"`
	Feeds     []FeedConfig    `yaml:"feeds"`
	Plugins   []PluginConfig  `yaml:"plugins"`
	Health    HealthConfig    `yaml:"health"`
	AIRepair  AIRepairConfig  `yaml:"ai_repair"`
	Artifacts ArtifactsConfig `yaml:"artifacts"`
}

type WorkspaceConfig struct {
	ID                string `yaml:"id"`
	Name              string `yaml:"name"`
	WorktreeStorage   string `yaml:"worktree_storage"`
	LinuxWorktreeRoot string `yaml:"linux_worktree_root"`
}

type OpenWrtConfig struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	Update string `yaml:"update"`
}

type DockerConfig struct {
	Image    string `yaml:"image"`
	Platform string `yaml:"platform"`
}

type BuildConfig struct {
	ID      string             `yaml:"id"`
	OpenWrt BuildOpenWrtConfig `yaml:"openwrt"`
	Feeds   []string           `yaml:"feeds"`
	Plugins []string           `yaml:"plugins"`
	Config  BuildOptions       `yaml:"config"`
}

type BuildOpenWrtConfig struct {
	Target    string `yaml:"target"`
	Subtarget string `yaml:"subtarget"`
	Profile   string `yaml:"profile"`
}

type BuildOptions struct {
	Fragments []string `yaml:"fragments"`
	Packages  []string `yaml:"packages"`
	Jobs      any      `yaml:"jobs"`
}

type FeedConfig struct {
	Name    string `yaml:"name"`
	Repo    string `yaml:"repo"`
	Branch  string `yaml:"branch"`
	Path    string `yaml:"path"`
	Enabled *bool  `yaml:"enabled"`
}

type PluginConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Repo    string `yaml:"repo"`
	Branch  string `yaml:"branch"`
	Path    string `yaml:"path"`
	Enabled *bool  `yaml:"enabled"`
	Risk    string `yaml:"risk"`
}

type HealthConfig struct {
	MinDiskGB *int `yaml:"min_disk_gb"`
}

type AIRepairConfig struct {
	Enabled    *bool    `yaml:"enabled"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args"`
	Timeout    string   `yaml:"timeout"`
	MaxRetries *int     `yaml:"max_retries"`
	Adoption   string   `yaml:"adoption"`
}

type ArtifactsConfig struct {
	Retention string `yaml:"retention"`
}

type ResolvedConfig struct {
	SchemaVersion   int               `yaml:"schema_version"`
	RunID           string            `yaml:"run_id"`
	WorkspaceID     string            `yaml:"workspace_id"`
	SourceSetID     string            `yaml:"source_set_id"`
	BuildID         string            `yaml:"build_id"`
	ProjectRoot     string            `yaml:"project_root"`
	Workspace       ResolvedWorkspace `yaml:"workspace"`
	OpenWrt         ResolvedOpenWrt   `yaml:"openwrt"`
	Docker          ResolvedDocker    `yaml:"docker"`
	Build           ResolvedBuild     `yaml:"build"`
	Feeds           []ResolvedFeed    `yaml:"feeds"`
	Plugins         []ResolvedPlugin  `yaml:"plugins"`
	Health          ResolvedHealth    `yaml:"health"`
	AIRepair        ResolvedAIRepair  `yaml:"ai_repair"`
	Artifacts       ResolvedArtifacts `yaml:"artifacts"`
	AdoptedPatchIDs []string          `yaml:"adopted_patch_ids"`
}

type ResolvedWorkspace struct {
	ID                string `yaml:"id"`
	Name              string `yaml:"name"`
	WorktreeStorage   string `yaml:"worktree_storage"`
	LinuxWorktreeRoot string `yaml:"linux_worktree_root"`
	LogicalWorktreeID string `yaml:"logical_worktree_id"`
}

type ResolvedOpenWrt struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	Update string `yaml:"update"`
}

type ResolvedDocker struct {
	Image                 string `yaml:"image"`
	Platform              string `yaml:"platform"`
	ContainerWorktreePath string `yaml:"container_worktree_path"`
}

type ResolvedBuild struct {
	ID        string   `yaml:"id"`
	Target    string   `yaml:"target"`
	Subtarget string   `yaml:"subtarget"`
	Profile   string   `yaml:"profile"`
	Feeds     []string `yaml:"feeds"`
	Plugins   []string `yaml:"plugins"`
	Fragments []string `yaml:"fragments"`
	Packages  []string `yaml:"packages"`
	Jobs      int      `yaml:"jobs"`
}

type ResolvedFeed struct {
	Name    string `yaml:"name"`
	Repo    string `yaml:"repo"`
	Branch  string `yaml:"branch"`
	Path    string `yaml:"path"`
	Enabled bool   `yaml:"enabled"`
}

type ResolvedPlugin struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Repo    string `yaml:"repo"`
	Branch  string `yaml:"branch"`
	Path    string `yaml:"path"`
	Enabled bool   `yaml:"enabled"`
	Risk    string `yaml:"risk"`
}

type ResolvedHealth struct {
	MinDiskGB int `yaml:"min_disk_gb"`
}

type ResolvedAIRepair struct {
	Enabled    bool     `yaml:"enabled"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args"`
	Timeout    string   `yaml:"timeout"`
	MaxRetries int      `yaml:"max_retries"`
	Adoption   string   `yaml:"adoption"`
}

type ResolvedArtifacts struct {
	Retention string `yaml:"retention"`
}
