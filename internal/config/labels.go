package config

func (c Config) EnvironmentLabel() string {
	switch c.Environment {
	case "development":
		return "开发"
	case "production":
		return "生产"
	case "test", "testing":
		return "测试"
	case "staging":
		return "预发布"
	default:
		return c.Environment
	}
}

func (c Config) VersionLabel() string {
	switch c.Version {
	case "development":
		return "开发版"
	case "initial":
		return "初始版本"
	default:
		return c.Version
	}
}

func (c Config) RoleLabel() string {
	switch c.Role {
	case "api":
		return "接口服务"
	case "frontend":
		return "前端服务"
	case "worker":
		return "后台任务"
	case "cronjob":
		return "定时任务"
	default:
		return c.Role
	}
}

func (c Config) AppLabel() string {
	if c.AppName == "transform" {
		return "翻译服务"
	}
	return c.AppName
}
