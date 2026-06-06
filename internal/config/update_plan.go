package config

import "sort"

type SourceSetPlan struct {
	SourceSetID string
	BuildIDs    []string
	OpenWrt     SourceRepository
	Feeds       []SourceRepository
	Plugins     []PluginRepository
}

type SourceRepository struct {
	Name   string
	Repo   string
	Branch string
	Path   string
}

type PluginRepository struct {
	Name   string
	Type   string
	Repo   string
	Branch string
	Path   string
	Risk   string
}

func UpdateSourceSetPlans(cfg *UserConfig, buildID string) ([]SourceSetPlan, error) {
	if cfg == nil {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "$", "config 不能为空", "先读取 user config")
	}

	builds := cfg.Builds
	if buildID != "" {
		build, ok := BuildByID(cfg, buildID)
		if !ok {
			return nil, newConfigError("CONFIG_SCHEMA_ERROR", "build_id", "build 不存在", "选择配置中已声明的 build id")
		}
		builds = []BuildConfig{build}
	}

	feedByName := map[string]FeedConfig{}
	for _, feed := range cfg.Feeds {
		feedByName[feed.Name] = feed
	}
	pluginByName := map[string]PluginConfig{}
	for _, plugin := range cfg.Plugins {
		pluginByName[plugin.Name] = plugin
	}

	plansByID := map[string]*SourceSetPlan{}
	for _, build := range builds {
		sourceSetID, err := SourceSetID(cfg, build)
		if err != nil {
			return nil, err
		}
		plan := plansByID[sourceSetID]
		if plan == nil {
			created := SourceSetPlan{
				SourceSetID: sourceSetID,
				BuildIDs:    []string{},
				OpenWrt: SourceRepository{
					Name:   "openwrt",
					Repo:   cfg.OpenWrt.Repo,
					Branch: cfg.OpenWrt.Branch,
				},
				Feeds:   []SourceRepository{},
				Plugins: []PluginRepository{},
			}
			for _, name := range build.Feeds {
				feed := feedByName[name]
				created.Feeds = append(created.Feeds, SourceRepository{
					Name:   feed.Name,
					Repo:   feed.Repo,
					Branch: feed.Branch,
					Path:   feed.Path,
				})
			}
			for _, name := range build.Plugins {
				plugin := pluginByName[name]
				created.Plugins = append(created.Plugins, PluginRepository{
					Name:   plugin.Name,
					Type:   stringDefault(plugin.Type, "unknown"),
					Repo:   plugin.Repo,
					Branch: plugin.Branch,
					Path:   plugin.Path,
					Risk:   plugin.Risk,
				})
			}
			sort.Slice(created.Feeds, func(i, j int) bool { return created.Feeds[i].Name < created.Feeds[j].Name })
			sort.Slice(created.Plugins, func(i, j int) bool { return created.Plugins[i].Name < created.Plugins[j].Name })
			plansByID[sourceSetID] = &created
			plan = &created
		}
		plan.BuildIDs = append(plan.BuildIDs, build.ID)
	}

	plans := make([]SourceSetPlan, 0, len(plansByID))
	for _, plan := range plansByID {
		sort.Strings(plan.BuildIDs)
		plans = append(plans, *plan)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].SourceSetID < plans[j].SourceSetID })
	return plans, nil
}
